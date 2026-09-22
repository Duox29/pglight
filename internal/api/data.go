package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) TableData(w http.ResponseWriter, r *http.Request) {
	qq, sid, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	if table == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	if schema == "" {
		schema = "public"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	order := strings.TrimSpace(r.URL.Query().Get("order"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	qt := pgx.Identifier{schema, table}.Sanitize()
	where := ""
	var filterArgs []any
	if filter != "" {
		filterSQL, args, err := safeTableFilter(filter)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		where = " WHERE " + filterSQL
		filterArgs = args
	}
	if order != "" {
		parts := strings.Split(order, ",")
		safe := []string{}
		for _, o := range parts {
			o = strings.TrimSpace(o)
			if o == "" {
				continue
			}
			bits := strings.Fields(o)
			col := pgx.Identifier{bits[0]}.Sanitize()
			dir := "ASC"
			if len(bits) > 1 && strings.ToUpper(bits[1]) == "DESC" {
				dir = "DESC"
			}
			safe = append(safe, col+" "+dir)
		}
		if len(safe) > 0 {
			where += " ORDER BY " + strings.Join(safe, ", ")
		}
	}
	// Fetch one extra row instead of running a potentially expensive COUNT(*).
	// The extra row gives the UI an exact has_more signal while keeping this
	// endpoint to one database round-trip.
	fetchLimit := limit + 1
	sql := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", qt, where, fetchLimit, offset)
	h.execQueryArgs(w, r, qq, sid, sql, filterArgs, nil, -1, limit)
}

func safeTableFilter(input string) (string, []any, error) {
	parts, err := splitFilterAnd(input)
	if err != nil {
		return "", nil, err
	}
	clauses := make([]string, 0, len(parts))
	args := make([]any, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		upper := strings.ToUpper(part)
		matched := false
		for _, op := range []string{" IS NOT NULL", " IS NULL", " ILIKE ", " LIKE ", " >= ", " <= ", " <> ", " != ", " = ", " > ", " < "} {
			if idx := strings.Index(upper, op); idx >= 0 {
				left := strings.TrimSpace(part[:idx])
				right := strings.TrimSpace(part[idx+len(op):])
				if left == "" || !validFilterIdentifier(left) {
					return "", nil, fmt.Errorf("invalid filter column")
				}
				column := pgx.Identifier{left}.Sanitize()
				if strings.TrimSpace(op) == "IS NULL" || strings.TrimSpace(op) == "IS NOT NULL" {
					if right != "" {
						return "", nil, fmt.Errorf("IS NULL filters do not take a value")
					}
					clauses = append(clauses, column+strings.TrimSpace(op))
					matched = true
					break
				}
				value, valueErr := parseFilterValue(right)
				if valueErr != nil {
					return "", nil, valueErr
				}
				args = append(args, value)
				clauses = append(clauses, fmt.Sprintf("%s %s $%d", column, strings.TrimSpace(op), len(args)))
				matched = true
				break
			}
		}
		if !matched {
			return "", nil, fmt.Errorf("unsupported filter; use column operator value joined with AND")
		}
	}
	if len(clauses) == 0 {
		return "", nil, fmt.Errorf("filter is empty")
	}
	return strings.Join(clauses, " AND "), args, nil
}

func splitFilterAnd(input string) ([]string, error) {
	var out []string
	start := 0
	inQuote := false
	for i := 0; i < len(input); i++ {
		if input[i] == '\'' {
			if inQuote && i+1 < len(input) && input[i+1] == '\'' {
				i++
				continue
			}
			inQuote = !inQuote
			continue
		}
		if !inQuote && i+3 <= len(input) && strings.EqualFold(input[i:i+3], "and") && (i == 0 || input[i-1] == ' ' || input[i-1] == '\t') && (i+3 == len(input) || input[i+3] == ' ' || input[i+3] == '\t') {
			out = append(out, input[start:i])
			i += 2
			start = i + 1
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated filter string")
	}
	out = append(out, input[start:])
	return out, nil
}

func validFilterIdentifier(s string) bool {
	if s == "" || !(s[0] == '_' || s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for _, c := range s[1:] {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '$') {
			return false
		}
	}
	return true
}

func parseFilterValue(s string) (any, error) {
	if s == "" {
		return nil, fmt.Errorf("filter value is required")
	}
	if strings.EqualFold(s, "true") {
		return true, nil
	}
	if strings.EqualFold(s, "false") {
		return false, nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	var n pgtype.Numeric
	if err := n.Scan(s); err == nil {
		return n, nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		value := s[1 : len(s)-1]
		if strings.Contains(value, "\\") {
			return nil, fmt.Errorf("backslash escapes are not supported in table filters")
		}
		return strings.ReplaceAll(value, "''", "'"), nil
	}
	return nil, fmt.Errorf("unsupported filter value")
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	term := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(term) < 2 {
		writeJSON(w, 200, []map[string]any{})
		return
	}
	like := "%" + term + "%"
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `
		(SELECT 'table' AS kind, table_schema AS schema, table_name AS name, table_schema||'.'||table_name AS detail FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema') AND table_schema NOT LIKE 'pg\_%' AND (table_name ILIKE $1 OR table_schema ILIKE $1) LIMIT 30)
		UNION ALL
		(SELECT 'column', table_schema, table_name||'.'||column_name, data_type FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') AND table_schema NOT LIKE 'pg\_%' AND column_name ILIKE $1 LIMIT 30)
		UNION ALL
		(SELECT 'function', nspname, proname||'('||pg_get_function_arguments(p.oid)||')', lanname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND p.proname ILIKE $1 LIMIT 20)
		LIMIT 80`, like)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"kind", "schema", "name", "detail"}, data))
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session    string   `json:"session_id"`
		Schema     string   `json:"schema"`
		Table      string   `json:"table"`
		Columns    []string `json:"columns"`
		Rows       [][]any  `json:"rows"`
		OnConflict bool     `json:"on_conflict_do_nothing"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	if id == "" {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	if req.Table == "" || len(req.Columns) == 0 || len(req.Rows) == 0 {
		writeJSON(w, 400, map[string]string{"error": "table, columns and rows required"})
		return
	}
	if len(req.Rows) > 20000 {
		writeJSON(w, 400, map[string]string{"error": "too many rows (max 20000 per import)"})
		return
	}
	schema := req.Schema
	if schema == "" {
		schema = "public"
	}
	// One operation lease: when an explicit txn is open the raw pgx.Tx under
	// e.mu serves metadata reads AND all INSERT batches, and Commit blocks
	// until release — so a concurrent Commit can't commit batch 1 and fail
	// batch 2 (partial commit with an error to the user).
	qqRaw, release, inTxn, pool, ok := h.Mgr.AcquireLease(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	suffix := ""
	if req.OnConflict {
		suffix = " ON CONFLICT DO NOTHING"
	}
	// Preserve JSON numeric fidelity (json.Number, not float64) and coerce
	// digit-strings against the real column types so exact int8/numeric
	// values — including ones the UI round-tripped as strings — keep their
	// PostgreSQL types.
	qqMeta := qqRaw
	udts, err := colUDTs(ctx, logging.Wrap(qqMeta, h.Log, id), schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	coerced := make([][]any, len(req.Rows))
	for i, row := range req.Rows {
		if len(row) != len(req.Columns) {
			writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("row width %d != columns %d", len(row), len(req.Columns))})
			return
		}
		coerced[i] = coerceRow(numRow(row), req.Columns, udts)
	}

	if inTxn {
		qq := logging.Wrap(qqRaw, h.Log, id)
		total, err := insertRowsBatched(ctx, qq, schema, req.Table, req.Columns, coerced, suffix)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"rows_affected": total, "in_txn": true})
		return
	}
	if pool == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	txq := logging.Wrap(tx, h.Log, id)
	total, err := insertRowsBatched(ctx, txq, schema, req.Table, req.Columns, coerced, suffix)
	if err != nil {
		_ = tx.Rollback(ctx)
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"rows_affected": total})
}

func (h *Handler) RowOp(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string         `json:"session_id"`
		Schema  string         `json:"schema"`
		Table   string         `json:"table"`
		Op      string         `json:"op"`
		Values  map[string]any `json:"values"`
		Where   map[string]any `json:"where"`
		// Single marks a one-row UI action (edit cell / delete row). The
		// backend then guarantees exactly one row is affected: outside an
		// explicit txn it runs the statement in its own transaction and
		// rolls back on 0 or 2+ matches, so a stale/non-unique predicate
		// can never silently delete the wrong rows.
		Single bool `json:"single"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	qqRaw, release, inTxn, pool, ok := h.Mgr.AcquireLease(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	qq := logging.Wrap(qqRaw, h.Log, id)
	if req.Schema == "" {
		req.Schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	udts, err := colUDTs(ctx, qq, req.Schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	qt := pgx.Identifier{req.Schema, req.Table}.Sanitize()
	var sql string
	var args []any
	switch req.Op {
	case "insert":
		sql, args = buildInsert(qt, numMap(req.Values), udts)
	case "update":
		var whereErr error
		sql, args, whereErr = buildUpdate(qt, numMap(req.Values), numMap(req.Where), udts)
		if whereErr != nil {
			writeJSON(w, 400, map[string]string{"error": whereErr.Error()})
			return
		}
	case "delete":
		var whereErr error
		sql, args, whereErr = buildDelete(qt, numMap(req.Where), udts)
		if whereErr != nil {
			writeJSON(w, 400, map[string]string{"error": whereErr.Error()})
			return
		}
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown op"})
		return
	}
	if req.Single && req.Op != "insert" && inTxn {
		affected, execErr := execSingleRowInTxn(ctx, qq, sql, args)
		if execErr != nil {
			code := 400
			if _, conflict := execErr.(*rowCountError); conflict {
				code = 409
			}
			writeJSON(w, code, map[string]string{"error": execErr.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"rows_affected": affected, "in_txn": true})
		return
	}
	if !inTxn && pool == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	affected, execErr := h.execSingleRow(ctx, id, qq, sql, args, req.Single && req.Op != "insert")
	if execErr != nil {
		code := 400
		if _, conflict := execErr.(*rowCountError); conflict {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": execErr.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"rows_affected": affected, "in_txn": h.Mgr.InTxn(id)})
}

// BatchDelete deletes many rows atomically: one PostgreSQL transaction for
// the whole list, so bulk deletes can no longer partially complete. Each
// entry matches exactly one row; the first mismatch rolls everything back.
func (h *Handler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string           `json:"session_id"`
		Schema  string           `json:"schema"`
		Table   string           `json:"table"`
		Where   []map[string]any `json:"where"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	if id == "" {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	if req.Table == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	if len(req.Where) == 0 || len(req.Where) > 5000 {
		writeJSON(w, 400, map[string]string{"error": "where list must hold 1..5000 entries"})
		return
	}
	if req.Schema == "" {
		req.Schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	qqRaw, release, inTxn, pool, ok := h.Mgr.AcquireLease(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	qq := logging.Wrap(qqRaw, h.Log, id)
	udts, err := colUDTs(ctx, qq, req.Schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	qt := pgx.Identifier{req.Schema, req.Table}.Sanitize()
	stmts := make([]string, len(req.Where))
	argSets := make([][]any, len(req.Where))
	for i, wmap := range req.Where {
		s, a, werr := buildDelete(qt, numMap(wmap), udts)
		if werr != nil {
			writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("row %d: %s", i, werr.Error())})
			return
		}
		stmts[i], argSets[i] = s, a
	}
	var deleted int64
	if inTxn {
		if _, err := qq.Exec(ctx, "SAVEPOINT pglight_rowop"); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		for i := range stmts {
			n, derr := execCount(ctx, qq, stmts[i], argSets[i])
			if derr != nil {
				rollbackRowOpSavepoint(ctx, qq)
				writeJSON(w, 400, map[string]string{"error": derr.Error()})
				return
			}
			if n != 1 {
				rollbackRowOpSavepoint(ctx, qq)
				writeJSON(w, 409, map[string]string{"error": fmt.Sprintf("row %d matched %d rows, expected exactly 1 — rolling back this operation", i, n)})
				return
			}
			deleted += n
		}
		if _, err := qq.Exec(ctx, "RELEASE SAVEPOINT pglight_rowop"); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": deleted, "in_txn": true})
		return
	}
	if pool == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	txq := logging.Wrap(tx, h.Log, id)
	for i := range stmts {
		n, derr := execCount(ctx, txq, stmts[i], argSets[i])
		if derr != nil {
			_ = tx.Rollback(ctx)
			writeJSON(w, 400, map[string]string{"error": derr.Error()})
			return
		}
		if n != 1 {
			_ = tx.Rollback(ctx)
			writeJSON(w, 409, map[string]string{"error": fmt.Sprintf("row %d matched %d rows, expected exactly 1 — nothing was deleted", i, n)})
			return
		}
		deleted += n
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted})
}

// rowCountError reports a single-row action that matched 0 or 2+ rows.
type rowCountError struct {
	n int64
}

func (e *rowCountError) Error() string {
	if e.n == 0 {
		return "row no longer exists (stale data — refresh and retry)"
	}
	return fmt.Sprintf("predicate matched %d rows, expected exactly 1 — nothing was changed", e.n)
}

func execCount(ctx context.Context, q db.Querier, sql string, args []any) (int64, error) {
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// execSingleRow runs a write; when single is set it guarantees exactly one
// affected row, using a private transaction when the session has none open.
func (h *Handler) execSingleRow(ctx context.Context, id string, qq db.Querier, sql string, args []any, single bool) (int64, error) {
	if !single {
		return execCount(ctx, qq, sql, args)
	}
	if h.Mgr.InTxn(id) {
		n, err := execCount(ctx, qq, sql, args)
		if err != nil {
			return 0, err
		}
		if n != 1 {
			return n, &rowCountError{n: n}
		}
		return n, nil
	}
	pool, ok := h.Mgr.Get(id)
	if !ok {
		return 0, fmt.Errorf("not connected")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	txq := logging.Wrap(tx, h.Log, id)
	n, err := execCount(ctx, txq, sql, args)
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if n != 1 {
		_ = tx.Rollback(ctx)
		return n, &rowCountError{n: n}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

// execSingleRowInTxn makes a single-row operation atomic without aborting the
// user's surrounding transaction. A failed PostgreSQL statement can put the
// transaction into the aborted state, so rollback-to-savepoint is required on
// SQL errors as well as row-count mismatches.
func execSingleRowInTxn(ctx context.Context, q db.Querier, sql string, args []any) (int64, error) {
	if _, err := q.Exec(ctx, "SAVEPOINT pglight_rowop"); err != nil {
		return 0, err
	}
	n, err := execCount(ctx, q, sql, args)
	if err != nil {
		rollbackRowOpSavepoint(ctx, q)
		return 0, err
	}
	if n != 1 {
		rollbackRowOpSavepoint(ctx, q)
		return n, &rowCountError{n: n}
	}
	if _, err := q.Exec(ctx, "RELEASE SAVEPOINT pglight_rowop"); err != nil {
		rollbackRowOpSavepoint(ctx, q)
		return 0, err
	}
	return n, nil
}

func rollbackRowOpSavepoint(ctx context.Context, q db.Querier) {
	_, _ = q.Exec(ctx, "ROLLBACK TO SAVEPOINT pglight_rowop")
	_, _ = q.Exec(ctx, "RELEASE SAVEPOINT pglight_rowop")
}

func buildInsert(qt string, values map[string]any, udts map[string]string) (string, []any) {
	cols := []string{}
	vals := []any{}
	ph := []string{}
	i := 1
	for k, v := range values {
		cols = append(cols, pgx.Identifier{k}.Sanitize())
		vals = append(vals, coerceVal(v, udts[k]))
		ph = append(ph, fmt.Sprintf("$%d", i))
		i++
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qt, strings.Join(cols, ","), strings.Join(ph, ",")), vals
}

func buildSet(values map[string]any, udts map[string]string, start int) ([]string, []any) {
	set := []string{}
	args := []any{}
	i := start
	for k, v := range values {
		set = append(set, fmt.Sprintf("%s=$%d", pgx.Identifier{k}.Sanitize(), i))
		args = append(args, coerceVal(v, udts[k]))
		i++
	}
	return set, args
}

// buildWhere renders an equality predicate; NULL compares with IS NULL.
// An empty map is an error — bare UPDATE/DELETE is refused (pgAdmin safety).
func buildWhere(where map[string]any, udts map[string]string, start int) ([]string, []any, error) {
	clauses := []string{}
	args := []any{}
	i := start
	for k, v := range where {
		if v == nil {
			clauses = append(clauses, fmt.Sprintf("%s IS NULL", pgx.Identifier{k}.Sanitize()))
		} else {
			clauses = append(clauses, fmt.Sprintf("%s=$%d", pgx.Identifier{k}.Sanitize(), i))
			args = append(args, coerceVal(v, udts[k]))
			i++
		}
	}
	if len(clauses) == 0 {
		return nil, nil, fmt.Errorf("refusing write without WHERE (pgAdmin safety)")
	}
	return clauses, args, nil
}

func buildUpdate(qt string, values, where map[string]any, udts map[string]string) (string, []any, error) {
	set, args := buildSet(values, udts, 1)
	clauses, wargs, err := buildWhere(where, udts, len(args)+1)
	if err != nil {
		return "", nil, err
	}
	args = append(args, wargs...)
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s", qt, strings.Join(set, ","), strings.Join(clauses, " AND ")), args, nil
}

func buildDelete(qt string, where map[string]any, udts map[string]string) (string, []any, error) {
	clauses, args, err := buildWhere(where, udts, 1)
	if err != nil {
		return "", nil, fmt.Errorf("refusing delete without WHERE")
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s", qt, strings.Join(clauses, " AND ")), args, nil
}

// colUDTs returns column_name → udt_name for a table, used to give
// stringified exact numerics (and JSON numbers) their real PostgreSQL types
// in write paths.
func colUDTs(ctx context.Context, q db.Querier, schema, table string) (map[string]string, error) {
	_, data, err := queryJSON(q, ctx, `SELECT column_name, udt_name FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2`, schema, table)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(data))
	for _, row := range data {
		if len(row) >= 2 {
			out[fmt.Sprint(row[0])] = fmt.Sprint(row[1])
		}
	}
	return out, nil
}

var intRe = regexp.MustCompile(`^[+-]?\d+$`)

// coerceVal converts a JSON-decoded value to the Go type pgx encodes into
// the column's PostgreSQL type: digit-strings (which is how exact int8/
// numeric values arrive after the string wire format) become int64/Numeric.
func coerceVal(v any, udt string) any {
	s, ok := v.(string)
	if n, number := v.(json.Number); number {
		s, ok = n.String(), true
	}
	if !ok {
		return v
	}
	switch udt {
	case "int2", "int4", "int8":
		if intRe.MatchString(strings.TrimSpace(s)) {
			if i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				return i
			}
		}
		return s
	case "numeric", "decimal":
		t := strings.TrimSpace(s)
		var n pgtype.Numeric
		if err := n.Scan(t); err == nil {
			return n
		}
		return s
	case "float4", "float8":
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f
		}
		return s
	default:
		return s
	}
}

func coerceMap(m map[string]any, udts map[string]string) map[string]any {
	for k, v := range m {
		m[k] = coerceVal(v, udts[k])
	}
	return m
}

func coerceRow(row []any, columns []string, udts map[string]string) []any {
	for i, v := range row {
		if i < len(columns) {
			row[i] = coerceVal(v, udts[columns[i]])
		}
	}
	return row
}
