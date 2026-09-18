package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"
)

type queryReq struct {
	Session string `json:"session_id"`
	SQL     string `json:"sql"`
	Limit   int    `json:"limit"`
}

// splitStmt is one trimmed statement plus the byte offset of its first byte
// in the source script (for mapping Postgres error positions to lines).
type splitStmt struct {
	text   string
	offset int
}

// splitStatements splits SQL on semicolons outside strings/comments,
// including PostgreSQL dollar-quoted bodies. Whitespace between statements
// is skipped before each scan, so offset points at the statement's first
// meaningful byte in the source script.
func splitStatements(sql string) []splitStmt {
	var out []splitStmt
	var cur strings.Builder
	n := len(sql)
	i := 0
	start := 0
	skipTrivia(sql, &i, &start)
	var dollarTag string
	inDollar := false
	inSingle, inDouble := false, false
	inLineComment, inBlockComment := false, false
	for i < n {
		c := sql[i]
		var nxt byte
		if i+1 < n {
			nxt = sql[i+1]
		}
		if inLineComment {
			cur.WriteByte(c)
			if c == '\n' {
				inLineComment = false
			}
			i++
			continue
		}
		if inBlockComment {
			cur.WriteByte(c)
			if c == '*' && nxt == '/' {
				cur.WriteByte(nxt)
				i += 2
				inBlockComment = false
				continue
			}
			i++
			continue
		}
		if inDollar {
			cur.WriteByte(c)
			if c == '$' {
				// check for closing tag
				if strings.HasPrefix(sql[i:], dollarTag) {
					cur.WriteString(dollarTag[1:])
					i += len(dollarTag)
					inDollar = false
					continue
				}
			}
			i++
			continue
		}
		if inSingle {
			cur.WriteByte(c)
			if c == '\'' {
				if nxt == '\'' {
					cur.WriteByte(nxt)
					i += 2
					continue
				}
				inSingle = false
			}
			i++
			continue
		}
		if inDouble {
			cur.WriteByte(c)
			if c == '"' {
				if nxt == '"' {
					cur.WriteByte(nxt)
					i += 2
					continue
				}
				inDouble = false
			}
			i++
			continue
		}
		// not in anything
		if c == '-' && nxt == '-' {
			inLineComment = true
			cur.WriteByte(c)
			cur.WriteByte(nxt)
			i += 2
			continue
		}
		if c == '/' && nxt == '*' {
			inBlockComment = true
			cur.WriteByte(c)
			cur.WriteByte(nxt)
			i += 2
			continue
		}
		if c == '\'' {
			inSingle = true
			cur.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			inDouble = true
			cur.WriteByte(c)
			i++
			continue
		}
		if c == '$' {
			// try to parse $tag$
			j := i + 1
			for j < n && (isIdentChar(sql[j]) || sql[j] == '$') {
				if sql[j] == '$' {
					break
				}
				j++
			}
			if j < n && sql[j] == '$' {
				dollarTag = sql[i : j+1]
				inDollar = true
				cur.WriteString(dollarTag)
				i += len(dollarTag)
				continue
			}
			cur.WriteByte(c)
			i++
			continue
		}
		if c == ';' {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				// `start` is already past leading trivia; only trailing
				// trivia (before the `;`) needs trimming to find the first
				// meaningful byte.
				out = append(out, splitStmt{text: s, offset: start + (len(cur.String()) - len(strings.TrimLeft(cur.String(), " \t\n\r\f\v")))})
			}
			cur.Reset()
			i++
			start = i
			skipTrivia(sql, &i, &start)
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, splitStmt{text: s, offset: start + (len(cur.String()) - len(strings.TrimLeft(cur.String(), " \t\n\r\f\v")))})
	}
	return out
}

// skipTrivia fast-forwards over whitespace, line comments and block comments
// so the next statement starts at its first meaningful byte. It mirrors the
// comment rules in splitStatements but does not feed `cur`.
func skipTrivia(sql string, i, start *int) {
	n := len(sql)
	for *i < n {
		c := sql[*i]
		var nxt byte
		if *i+1 < n {
			nxt = sql[*i+1]
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
			(*i)++
			(*start)++
			continue
		}
		if c == '-' && nxt == '-' {
			*i += 2
			*start += 2
			for *i < n && sql[*i] != '\n' {
				(*i)++
				(*start)++
			}
			continue
		}
		if c == '/' && nxt == '*' {
			*i += 2
			*start += 2
			for *i < n {
				if sql[*i] == '*' && *i+1 < n && sql[*i+1] == '/' {
					*i += 2
					*start += 2
					break
				}
				(*i)++
				(*start)++
			}
			continue
		}
		break
	}
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req queryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
	raw := req.SQL
	if strings.TrimSpace(raw) == "" {
		writeJSON(w, 400, map[string]string{"error": "empty sql"})
		return
	}
	stmts := splitStatements(raw)
	// Schema may have changed: drop the cached complete snapshot after a
	// successful DDL run so the editor refetches once (not per keystroke).
	wantInvalidate := ddlRe.MatchString(raw)
	if len(stmts) > 1 {
		start := time.Now()
		results := []map[string]any{}
		for idx, st := range stmts {
			// The SELECT wrapper hides the real text (and shifts Position),
			// so line-map against the text Postgres actually parsed while
			// reporting the original text in results.
			execText := wrapSelect(st.text, req.Limit)
			res, qerr := h.runSingle(r, qq, execText, 0)
			if qerr != nil {
				body := map[string]any{"error": qerr.Error(), "statement": st.text, "results": results, "statements": len(stmts), "duration_ms": time.Since(start).Milliseconds(), "in_txn": h.Mgr.InTxn(id)}
				for k, v := range errLocation(raw, idx, st.offset, st.text, execText, qerr) {
					body[k] = v
				}
				writeJSON(w, 400, body)
				return
			}
			if res != nil {
				res["statement"] = st.text
			}
			results = append(results, res)
		}
		if wantInvalidate {
			globalComplete.Invalidate(id)
		}
		writeJSON(w, 200, map[string]any{"results": results, "duration_ms": time.Since(start).Milliseconds(), "in_txn": h.Mgr.InTxn(id)})
		return
	}
	sql := strings.TrimSpace(raw)
	off, text := 0, sql
	if len(stmts) == 1 {
		off, text = stmts[0].offset, stmts[0].text
	}
	sql = wrapSelect(text, req.Limit)
	loc := &stmtLoc{script: raw, index: -1, offset: off, text: text}
	if wantInvalidate {
		defer globalComplete.Invalidate(id)
	}
	h.execQuery(w, r, qq, id, sql, loc, -1)
}

// stmtLoc maps an executed statement back to the user's script.
type stmtLoc struct {
	script string // full untrimmed script as sent
	index  int    // statement index in the script, -1 for a single statement
	offset int    // byte offset of the statement's first byte in script
	text   string // trimmed statement text before the SELECT wrapper
}

// isSingleSelect reports whether sql is one unwrapped SELECT (no trailing
// statements). The console wraps those to enforce the row cap.
func isSingleSelect(sql string) bool {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	return strings.HasPrefix(strings.ToUpper(s), "SELECT") && strings.Count(s, ";") == 0
}

// wrapSelect caps ad-hoc SELECTs so the console never floods the UI.
// Non-SELECTs and out-of-range limits pass through untouched.
func wrapSelect(sql string, limit int) string {
	if limit > 0 && limit < 5000 && isSingleSelect(sql) {
		return fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", strings.TrimSuffix(sql, ";"), limit)
	}
	return sql
}

func (h *Handler) runSingle(r *http.Request, qq db.Querier, sql string, limit int) (map[string]any, error) {
	if limit > 0 && limit < 5000 && isSingleSelect(sql) {
		sql = fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", strings.TrimSuffix(sql, ";"), limit)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rows, err := qq.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	types := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Name
		types[i] = strconv.Itoa(int(f.DataTypeOID))
	}
	data := [][]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		data = append(data, jsonSafeCells(vals, fields))
		if len(data) >= 1000 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cmd := rows.CommandTag()
	return map[string]any{
		"columns": cols, "types": types, "rows": data,
		"rows_affected": cmd.RowsAffected(),
		"duration_ms":   time.Since(start).Milliseconds(),
		"statement":     sql,
	}, nil
}

func (h *Handler) Explain(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string `json:"session_id"`
		SQL     string `json:"sql"`
		Analyze bool   `json:"analyze"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
	if strings.TrimSpace(req.SQL) == "" {
		writeJSON(w, 400, map[string]string{"error": "empty sql"})
		return
	}
	prefix := "EXPLAIN (FORMAT JSON)"
	if req.Analyze {
		prefix = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var raw json.RawMessage
	err := qq.QueryRow(ctx, prefix+" "+req.SQL).Scan(&raw)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
