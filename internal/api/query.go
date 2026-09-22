package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// Query results are returned as one JSON document, so an actually unlimited
// query still needs a memory guard. Results below this budget are unlimited;
// larger results fail clearly instead of allowing the server and browser to
// exhaust memory while building/decoding the response.
const maxQueryResultBytes = 64 << 20
const maxQuerySQLBytes = 8 << 20
const maxQueryStatements = 100

type queryResultTooLargeError struct {
	maxBytes int
}

func (e *queryResultTooLargeError) Error() string {
	return fmt.Sprintf("query result is too large for the UI (over %d MiB); add a LIMIT/filter or choose a smaller result size", e.maxBytes/(1<<20))
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
	inSingle, inDouble, inEscapeString := false, false, false
	inLineComment := false
	blockCommentDepth := 0
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
		if blockCommentDepth > 0 {
			cur.WriteByte(c)
			if c == '/' && nxt == '*' {
				cur.WriteByte(nxt)
				i += 2
				blockCommentDepth++
				continue
			}
			if c == '*' && nxt == '/' {
				cur.WriteByte(nxt)
				i += 2
				blockCommentDepth--
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
			if inEscapeString && c == '\\' && i+1 < n {
				cur.WriteByte(sql[i+1])
				i += 2
				continue
			}
			if c == '\'' {
				if nxt == '\'' {
					cur.WriteByte(nxt)
					i += 2
					continue
				}
				inSingle, inEscapeString = false, false
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
			blockCommentDepth = 1
			cur.WriteByte(c)
			cur.WriteByte(nxt)
			i += 2
			continue
		}
		if (c == 'e' || c == 'E') && nxt == '\'' && (i == 0 || !isIdentChar(sql[i-1])) {
			inSingle, inEscapeString = true, true
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
			depth := 1
			for *i < n && depth > 0 {
				if sql[*i] == '/' && *i+1 < n && sql[*i+1] == '*' {
					depth++
					*i += 2
					*start += 2
					continue
				}
				if sql[*i] == '*' && *i+1 < n && sql[*i+1] == '/' {
					depth--
					*i += 2
					*start += 2
					continue
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
	if err := decodeBody(r, &req); err != nil {
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
	if len(raw) > maxQuerySQLBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "sql script too large (max 8 MiB)"})
		return
	}
	stmts := splitStatements(raw)
	if len(stmts) > maxQueryStatements {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("too many statements (max %d)", maxQueryStatements)})
		return
	}
	// One deadline covers the whole script. Each statement also keeps the
	// normal query timeout in runSingle/execQuery, but must not restart that
	// budget for every statement in a multi-statement request.
	requestCtx, requestCancel := context.WithTimeout(r.Context(), queryTimeout)
	defer requestCancel()
	r = r.WithContext(requestCtx)
	// Schema may have changed: drop the cached complete snapshot after a
	// successful DDL run so the editor refetches once (not per keystroke).
	wantInvalidate := ddlRe.MatchString(raw)
	if len(stmts) > 1 {
		start := time.Now()
		results := []map[string]any{}
		var totalResultBytes int64
		for idx, st := range stmts {
			// The SELECT wrapper hides the real text (and shifts Position),
			// so line-map against the text Postgres actually parsed while
			// reporting the original text in results.
			execText := wrapSelect(st.text, req.Limit)
			res, qerr := h.runSingle(r, qq, st.text, req.Limit, &totalResultBytes)
			if qerr != nil {
				body := map[string]any{"error": qerr.Error(), "statement": st.text, "results": results, "statements": len(stmts), "duration_ms": time.Since(start).Milliseconds(), "in_txn": h.Mgr.InTxn(id)}
				for k, v := range errLocation(raw, idx, st.offset, st.text, execText, qerr) {
					body[k] = v
				}
				writeJSON(w, queryErrorStatus(qerr, http.StatusBadRequest), body)
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
	h.execQuery(w, r, qq, id, sql, loc, -1, req.Limit)
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
	parts := splitStatements(sql)
	if len(parts) != 1 {
		return false
	}
	s := strings.TrimSpace(parts[0].text)
	keyword := strings.ToUpper(firstSQLKeyword(s))
	if keyword == "SELECT" || keyword == "VALUES" || keyword == "TABLE" {
		return true
	}
	if keyword != "WITH" {
		return false
	}
	return topLevelResultKeyword(s)
}

func firstSQLKeyword(sql string) string {
	for i := 0; i < len(sql); {
		if sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r' || sql[i] == '\f' || sql[i] == '\v' {
			i++
			continue
		}
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			i += 2
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			i += 2
			depth := 1
			for i < len(sql) && depth > 0 {
				if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/' {
					depth--
					i += 2
					continue
				}
				i++
			}
			continue
		}
		start := i
		for i < len(sql) && isIdentChar(sql[i]) {
			i++
		}
		return sql[start:i]
	}
	return ""
}

// topLevelResultKeyword identifies the command after a WITH clause without
// attempting to parse PostgreSQL expressions. CTE bodies are parenthesized,
// so top-level SELECT/VALUES/TABLE is sufficient for the result cap.
func topLevelResultKeyword(sql string) bool {
	depth := 0
	inSingle, inDouble, escapeString := false, false, false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		var nxt byte
		if i+1 < len(sql) {
			nxt = sql[i+1]
		}
		if inSingle {
			if escapeString && c == '\\' && i+1 < len(sql) {
				i++
				continue
			}
			if c == '\'' {
				if nxt == '\'' {
					i++
					continue
				}
				inSingle, escapeString = false, false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				if nxt == '"' {
					i++
					continue
				}
				inDouble = false
			}
			continue
		}
		if c == '-' && nxt == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && nxt == '*' {
			depthComment := 1
			i += 2
			for i < len(sql) && depthComment > 0 {
				if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
					depthComment++
					i += 2
					continue
				}
				if i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/' {
					depthComment--
					i += 2
					continue
				}
				i++
			}
			i--
			continue
		}
		if (c == 'e' || c == 'E') && nxt == '\'' && (i == 0 || !isIdentChar(sql[i-1])) {
			inSingle, escapeString = true, true
			i++
			continue
		}
		if c == '\'' {
			inSingle = true
			continue
		}
		if c == '"' {
			inDouble = true
			continue
		}
		if c == '(' {
			depth++
			continue
		}
		if c == ')' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 && isIdentChar(c) {
			start := i
			for i+1 < len(sql) && isIdentChar(sql[i+1]) {
				i++
			}
			word := strings.ToUpper(sql[start : i+1])
			switch word {
			case "SELECT", "VALUES", "TABLE":
				return true
			case "INSERT", "UPDATE", "DELETE", "MERGE":
				return false
			}
		}
	}
	return false
}

// wrapSelect applies the selected result limit to one ad-hoc SELECT.
// Non-SELECTs and no-limit (0) pass through untouched.
func wrapSelect(sql string, limit int) string {
	if limit > 0 && isSingleSelect(sql) {
		return fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", strings.TrimSuffix(sql, ";"), limit)
	}
	return sql
}

func (h *Handler) runSingle(r *http.Request, qq db.Querier, sql string, limit int, budget *int64) (map[string]any, error) {
	sql = wrapSelect(sql, limit)
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rows, err := qq.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collectLimit := 0
	if limit > 0 {
		collectLimit = limit + 1
	}
	cols, types, data, cmd, err := collectQueryRowsBudget(rows, collectLimit, budget)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"columns": cols, "types": types, "rows": data,
		"rows_affected": cmd.RowsAffected(),
		"duration_ms":   time.Since(start).Milliseconds(),
		"statement":     sql,
	}
	if limit > 0 && len(data) > limit {
		out["has_more"] = true
		out["rows"] = data[:limit]
	}
	return out, nil
}

func (h *Handler) Explain(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string `json:"session_id"`
		SQL     string `json:"sql"`
	}
	if err := decodeBody(r, &req); err != nil {
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
	if len(req.SQL) > maxQuerySQLBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "sql statement too large (max 8 MiB)"})
		return
	}
	prefix := "EXPLAIN (FORMAT JSON)"
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
