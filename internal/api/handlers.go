package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	Mgr *db.Manager
	Log *logging.Logger
}

// queryTimeout caps user query execution (console, table ops). Long enough
// for analytical statements; the console Cancel button ends them sooner.
const queryTimeout = 120 * time.Second

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// pool returns the raw pool (non-txn). Prefer q() for query paths.
func (h *Handler) pool(r *http.Request) (*pgxpool.Pool, bool) {
	id := sessionID(r)
	if id == "" {
		return nil, false
	}
	p, ok := h.Mgr.Get(id)
	return p, ok
}

// q returns the txn-aware query target plus the session id.
func (h *Handler) q(r *http.Request) (db.Querier, string, bool) {
	id := sessionID(r)
	if id == "" {
		return nil, "", false
	}
	t, ok := h.Mgr.Q(id)
	return logging.Wrap(t, h.Log, id), id, ok
}

func sessionID(r *http.Request) string {
	id := r.URL.Query().Get("session_id")
	if id == "" {
		id = r.Header.Get("X-Session-Id")
	}
	return id
}

func sessionFromBody(id string, r *http.Request) string {
	if id != "" {
		return id
	}
	return sessionID(r)
}

func queryJSON(q db.Querier, ctx context.Context, sql string, args ...any) ([]string, [][]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Name
	}
	var data [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		data = append(data, vals)
	}
	return cols, data, rows.Err()
}

// errLocation maps a Postgres error to 1-based line/column in the user's
// script. stmtText/execText are the sent/parsed statement (they differ when
// the SELECT wrapper applied). Always additive: code + statement_index when
// known, line/column fall back to the statement start when Position is
// missing (duplicate objects, permission errors) or fails to map.
// Non-PgErrors yield nil.
func errLocation(script string, stmtIdx, stmtOff int, stmtText, execText string, qerr error) map[string]any {
	var pgErr *pgconn.PgError
	if !errors.As(qerr, &pgErr) {
		return nil
	}
	out := map[string]any{}
	if pgErr.Code != "" {
		out["code"] = pgErr.Code
	}
	if stmtIdx >= 0 {
		out["statement_index"] = stmtIdx
	}
	// fallback reports the statement start so callers always have a line
	// to jump to when the precise error offset is unavailable.
	fallback := func() {
		if stmtOff < 0 || stmtOff > len(script) {
			return
		}
		if stmtOff+len(stmtText) > len(script) || script[stmtOff:stmtOff+len(stmtText)] != stmtText {
			return
		}
		line, col := lineCol(script, stmtOff)
		out["line"] = line
		out["column"] = col
	}
	pos := int(pgErr.Position) // 1-based chars in execText; 0 when N/A
	if pos < 1 {
		fallback()
		return out
	}
	execRunes := []rune(execText)
	if pos > len(execRunes) {
		fallback()
		return out
	}
	// Exec-relative 0-based char offset → statement-relative.
	stmtChar := pos - 1
	if execText != stmtText {
		const prefix = "SELECT * FROM ("
		pre := len([]rune(prefix))
		if !strings.HasPrefix(execText, prefix) || stmtChar < pre || stmtChar >= pre+len([]rune(stmtText)) {
			fallback()
			return out
		}
		stmtChar -= pre
	}
	stmtRunes := []rune(stmtText)
	if stmtChar > len(stmtRunes) {
		fallback()
		return out
	}
	// Statement-relative chars → absolute byte offset in the script.
	abs := -1
	if stmtOff >= 0 && stmtOff+len(stmtText) <= len(script) && script[stmtOff:stmtOff+len(stmtText)] == stmtText {
		abs = stmtOff + len(string(stmtRunes[:stmtChar]))
	}
	if abs < 0 {
		fallback()
		return out
	}
	line, col := lineCol(script, abs)
	out["line"] = line
	out["column"] = col
	return out
}

// lineCol converts a byte offset into 1-based line/column (columns in runes).
func lineCol(s string, off int) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > len(s) {
		off = len(s)
	}
	line = 1 + strings.Count(s[:off], "\n")
	last := strings.LastIndex(s[:off], "\n")
	col = len([]rune(s[last+1:off])) + 1
	return line, col
}

// queryErrBody builds the additive /api/query + /api/table-data error shape:
// {error} plus code/statement_index and line/column (exact offset, else
// statement start) when the statement maps back to the script.
func queryErrBody(h *Handler, sid string, loc *stmtLoc, execText string, qerr error) map[string]any {
	body := map[string]any{"error": qerr.Error(), "in_txn": h.Mgr.InTxn(sid)}
	script, idx, off, text := execText, -1, 0, execText
	if loc != nil {
		script, idx, off, text = loc.script, loc.index, loc.offset, loc.text
	}
	for k, v := range errLocation(script, idx, off, text, execText, qerr) {
		body[k] = v
	}
	return body
}

func (h *Handler) execQuery(w http.ResponseWriter, r *http.Request, qq db.Querier, sid string, sql string, loc *stmtLoc, total int64, visibleLimit ...int) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rows, err := qq.Query(ctx, sql)
	if err != nil {
		writeJSON(w, 400, queryErrBody(h, sid, loc, sql, err))
		return
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
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		data = append(data, vals)
		if len(data) >= 1001 {
			break
		}
	}
	cmd := rows.CommandTag()
	hasMore := false
	if len(visibleLimit) > 0 && visibleLimit[0] > 0 && len(data) > visibleLimit[0] {
		hasMore = true
		data = data[:visibleLimit[0]]
	}
	out := map[string]any{
		"columns": cols, "types": types, "rows": data,
		"rows_affected": cmd.RowsAffected(),
		"duration_ms":   time.Since(start).Milliseconds(),
		"in_txn":        h.Mgr.InTxn(sid),
	}
	if len(visibleLimit) > 0 {
		out["has_more"] = hasMore
	}
	if out["rows"] == nil {
		out["rows"] = [][]any{}
	}
	if total >= 0 {
		out["total"] = total
	}
	writeJSON(w, 200, out)
}

func rowsToMaps(cols []string, data [][]any) []map[string]any {
	out := []map[string]any{}
	for _, d := range data {
		m := map[string]any{}
		for i, c := range cols {
			if i < len(d) {
				m[c] = d[i]
			}
		}
		out = append(out, m)
	}
	if out == nil {
		return []map[string]any{}
	}
	return out
}
