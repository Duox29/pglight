package api

import (
	"context"
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
	if filter != "" {
		where = " WHERE " + filter
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
	h.execQuery(w, r, qq, sid, sql, nil, -1, limit)
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
	pool, hasPool, inTxn := h.Mgr.Snapshot(id)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	cols := make([]string, len(req.Columns))
	for i, c := range req.Columns {
		if strings.TrimSpace(c) == "" {
			writeJSON(w, 400, map[string]string{"error": "empty column name"})
			return
		}
		cols[i] = pgx.Identifier{c}.Sanitize()
	}
	qt := pgx.Identifier{schema, req.Table}.Sanitize()
	suffix := ""
	if req.OnConflict {
		suffix = " ON CONFLICT DO NOTHING"
	}
	// Preserve JSON numeric fidelity (json.Number, not float64) and coerce
	// digit-strings against the real column types so exact int8/numeric
	// values — including ones the UI round-tripped as strings — keep their
	// PostgreSQL types.
	qqMeta, ok := h.Mgr.Q(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	udts, err := colUDTs(ctx, logging.Wrap(qqMeta, h.Log, id), schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	coerced := make([][]any, len(req.Rows))
	for i, row := range req.Rows {
		if len(row) != len(cols) {
			writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("row width %d != columns %d", len(row), len(cols))})
			return
		}
		coerced[i] = coerceRow(numRow(row), req.Columns, udts)
	}
	insertBatch := func(q db.Querier, batch [][]any) (int64, error) {
		ph := []string{}
		args := []any{}
		n := 1
		for _, row := range batch {
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
		return tag.RowsAffected(), nil
	}

	const batchSize = 500
	if inTxn {
		qqRaw, _ := h.Mgr.Q(id)
		qq := logging.Wrap(qqRaw, h.Log, id)
		var total int64
		for s := 0; s < len(coerced); s += batchSize {
			e := s + batchSize
			if e > len(coerced) {
				e = len(coerced)
			}
			n, err := insertBatch(qq, coerced[s:e])
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			total += n
		}
		writeJSON(w, 200, map[string]any{"rows_affected": total, "in_txn": true})
		return
	}
	if !hasPool {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	txq := logging.Wrap(tx, h.Log, id)
	var total int64
	failed := false
	var failErr error
	for s := 0; s < len(coerced); s += batchSize {
		e := s + batchSize
		if e > len(coerced) {
			e = len(coerced)
		}
		n, err := insertBatch(txq, coerced[s:e])
		if err != nil {
			failed = true
			failErr = err
			break
		}
		total += n
	}
	if failed {
		_ = tx.Rollback(ctx)
		writeJSON(w, 400, map[string]string{"error": failErr.Error()})
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
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
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
	qq, ok := h.Mgr.Q(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
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
	if h.Mgr.InTxn(id) {
		for i := range stmts {
			n, derr := execCount(ctx, qq, stmts[i], argSets[i])
			if derr != nil {
				writeJSON(w, 400, map[string]string{"error": derr.Error()})
				return
			}
			if n != 1 {
				writeJSON(w, 409, map[string]string{"error": fmt.Sprintf("row %d matched %d rows, expected exactly 1 — rolling back your transaction is recommended", i, n)})
				return
			}
			deleted += n
		}
		writeJSON(w, 200, map[string]any{"deleted": deleted, "in_txn": true})
		return
	}
	pool, hasPool := h.Mgr.Get(id)
	if !hasPool {
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
