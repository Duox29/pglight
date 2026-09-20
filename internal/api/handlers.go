package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	Mgr    *db.Manager
	Log    *logging.Logger
	Store  *store.Store
	UserID string
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

// maxBodyBytes caps JSON request bodies (all endpoints share decodeBody):
// 64MB comfortably fits a 20k-row import while bounding memory/CPU from
// hostile payloads. Larger bodies get a clean 400, not an OOM.
const maxBodyBytes = 64 << 20

// decodeBody decodes a JSON request body preserving numeric fidelity:
// json.Number keeps arbitrary precision instead of collapsing to float64.
// Callers convert numbers via numVal before passing them to PostgreSQL.
func decodeBody(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("request body too large (max %d bytes)", maxBodyBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(v)
}

// numVal converts a decoded JSON number to the most exact Go value: integral
// numbers become int64 (exact through pgx), decimals stay float64 unless the
// caller coerces them against a known column type. Non-numbers pass through.
func numVal(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := n.Int64(); err == nil && json.Number(strconv.FormatInt(i, 10)) == n {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return string(n)
}

// numVals maps numVal over a slice (import rows) or a map (row ops).
func numRow(row []any) []any {
	for i, v := range row {
		row[i] = numVal(v)
	}
	return row
}

func numMap(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = numVal(v)
	}
	return m
}

// exactOIDs are PostgreSQL types whose values lose precision as JSON/JS
// numbers (IEEE-754 doubles only cover ±2^53 exactly). int8, numeric and
// money cross the wire as strings; the type OID alongside tells the frontend
// how to render and send them back. Array variants are element-wise strings.
var exactOIDs = map[uint32]bool{
	20:   true, // int8
	790:  true, // money (pgx decodes as text already; kept for arrays)
	1700: true, // numeric
	1016: true, // _int8
	791:  true, // _money
	1231: true, // _numeric
}

// jsonSafeCells converts exact-numeric pgx values to strings before JSON
// encoding so JavaScript cannot silently corrupt them (e.g. int8 PKs above
// 9007199254740991). All other values pass through untouched.
func jsonSafeCells(vals []any, fds []pgconn.FieldDescription) []any {
	for i, v := range vals {
		if v == nil {
			continue
		}
		var oid uint32
		if i < len(fds) {
			oid = fds[i].DataTypeOID
		}
		if exactOIDs[oid] {
			vals[i] = exactString(v)
		}
	}
	return vals
}

func exactString(v any) any {
	switch n := v.(type) {
	case int64:
		return strconv.FormatInt(n, 10)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case int:
		return strconv.Itoa(n)
	case uint64:
		return strconv.FormatUint(n, 10)
	case pgtype.Numeric:
		if s, err := n.Value(); err == nil {
			if str, ok := s.(string); ok {
				return str
			}
		}
		if b, err := n.MarshalJSON(); err == nil {
			return string(b)
		}
		return fmt.Sprint(v)
	case string:
		return n
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			out := make([]any, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = exactString(rv.Index(i).Interface())
			}
			return out
		}
		return v
	}
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
		data = append(data, jsonSafeCells(vals, fields))
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

// queryErrorStatus preserves the existing client error contract while making
// oversized unlimited results actionable instead of returning a generic 500.
func queryErrorStatus(err error, fallback int) int {
	var tooLarge *queryResultTooLargeError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return fallback
}

// collectQueryRows materializes a bounded JSON result. maxRows is zero for
// no row-count limit; the byte budget still applies to every result so a
// large cell/result cannot exhaust server or browser memory.
func collectQueryRows(rows pgx.Rows, maxRows int) ([]string, []string, [][]any, pgconn.CommandTag, error) {
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	types := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Name
		types[i] = strconv.Itoa(int(f.DataTypeOID))
	}
	data := [][]any{}
	resultBytes := 0
	for rows.Next() {
		if maxRows > 0 && len(data) >= maxRows {
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, nil, pgconn.CommandTag{}, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		row := jsonSafeCells(vals, fields)
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, nil, nil, pgconn.CommandTag{}, err
		}
		resultBytes += len(encoded) + 1
		if resultBytes > maxQueryResultBytes {
			return nil, nil, nil, pgconn.CommandTag{}, &queryResultTooLargeError{maxBytes: maxQueryResultBytes}
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, pgconn.CommandTag{}, err
	}
	return cols, types, data, rows.CommandTag(), nil
}

func (h *Handler) execQuery(w http.ResponseWriter, r *http.Request, qq db.Querier, sid string, sql string, loc *stmtLoc, total int64, visibleLimit ...int) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rows, err := qq.Query(ctx, sql)
	if err != nil {
		writeJSON(w, queryErrorStatus(err, http.StatusBadRequest), queryErrBody(h, sid, loc, sql, err))
		return
	}
	defer rows.Close()
	collectLimit := 0
	if len(visibleLimit) > 0 && visibleLimit[0] > 0 {
		// Table-data needs one extra row for has_more. Query results use the
		// same path, while wrapSelect prevents a single SELECT from exceeding
		// the chosen limit.
		collectLimit = visibleLimit[0] + 1
	}
	cols, types, data, cmd, err := collectQueryRows(rows, collectLimit)
	if err != nil {
		writeJSON(w, queryErrorStatus(err, http.StatusInternalServerError), queryErrBody(h, sid, loc, sql, err))
		return
	}
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

// insertRowsBatched is the shared bulk-INSERT path for CSV import
// (/api/import) and mock-data generation (/api/mock-data/generate):
// pgx-sanitized identifiers, atomic only when the caller wraps it in a
// transaction (both callers do — either the session's explicit txn or a
// private one they commit/rollback themselves). Batches are sized so one
// statement never exceeds PostgreSQL's 65535 bind-parameter limit
// (500 rows × N columns); a zero-column call inserts DEFAULT VALUES rows
// (default-only tables).
func insertRowsBatched(ctx context.Context, q db.Querier, schema, table string, columns []string, rows [][]any, suffix string) (int64, error) {
	cols := make([]string, len(columns))
	for i, c := range columns {
		if strings.TrimSpace(c) == "" {
			return 0, fmt.Errorf("empty column name")
		}
		cols[i] = pgx.Identifier{c}.Sanitize()
	}
	qt := pgx.Identifier{schema, table}.Sanitize()
	if len(cols) == 0 {
		// Default-only tables have no column list. One INSERT per row would
		// need 20k round-trips for a full request, so batch many
		// single-row DEFAULT VALUES statements into one Exec per batch
		// (same transaction/lease as the caller holds — still atomic).
		const defBatch = 500
		var total int64
		for s := 0; s < len(rows); s += defBatch {
			e := s + defBatch
			if e > len(rows) {
				e = len(rows)
			}
			for _, row := range rows[s:e] {
				if len(row) != 0 {
					return 0, fmt.Errorf("row width %d != columns 0", len(row))
				}
			}
			n := e - s
			var sb strings.Builder
			for i := 0; i < n; i++ {
				sb.WriteString(fmt.Sprintf("INSERT INTO %s DEFAULT VALUES%s; ", qt, suffix))
			}
			tag, err := q.Exec(ctx, sb.String())
			if err != nil {
				return 0, err
			}
			// Multi-statement Exec reports only the last tag; each
			// statement inserts exactly one row, so count batches exactly.
			if tag.RowsAffected() == int64(n) {
				total += tag.RowsAffected()
			} else {
				total += int64(n)
			}
		}
		return total, nil
	}
	// maxPGParams is PostgreSQL's extended-protocol bind-parameter ceiling.
	const maxPGParams = 65535
	batchSize := 500
	if n := maxPGParams / len(cols); n < batchSize {
		batchSize = n
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("too many columns (%d): one row exceeds 65535 parameters", len(cols))
	}
	var total int64
	for s := 0; s < len(rows); s += batchSize {
		e := s + batchSize
		if e > len(rows) {
			e = len(rows)
		}
		ph := []string{}
		args := []any{}
		n := 1
		for _, row := range rows[s:e] {
			if len(row) != len(cols) {
				return 0, fmt.Errorf("row width %d != columns %d", len(row), len(cols))
			}
			rowPh := make([]string, len(cols))
			for i, v := range row {
				rowPh[i] = fmt.Sprintf("$%d", n)
				args = append(args, v)
				n++
			}
			ph = append(ph, "("+strings.Join(rowPh, ",")+")")
		}
		tag, err := q.Exec(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES %s%s", qt, strings.Join(cols, ","), strings.Join(ph, ","), suffix), args...)
		if err != nil {
			return 0, err
		}
		total += tag.RowsAffected()
	}
	return total, nil
}
