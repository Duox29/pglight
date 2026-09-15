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

	"github.com/jackc/pgx/v5"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	pool, hasPool := h.Mgr.Get(id)
	inTxn := h.Mgr.InTxn(id)
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
				args = append(args, strVal(v))
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
		for s := 0; s < len(req.Rows); s += batchSize {
			e := s + batchSize
			if e > len(req.Rows) {
				e = len(req.Rows)
			}
			n, err := insertBatch(qq, req.Rows[s:e])
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
	for s := 0; s < len(req.Rows); s += batchSize {
		e := s + batchSize
		if e > len(req.Rows) {
			e = len(req.Rows)
		}
		n, err := insertBatch(txq, req.Rows[s:e])
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
	if req.Schema == "" {
		req.Schema = "public"
	}
	qt := pgx.Identifier{req.Schema, req.Table}.Sanitize()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var sql string
	var args []any
	switch req.Op {
	case "insert":
		cols := []string{}
		vals := []any{}
		ph := []string{}
		i := 1
		for k, v := range req.Values {
			cols = append(cols, pgx.Identifier{k}.Sanitize())
			vals = append(vals, strVal(v))
			ph = append(ph, fmt.Sprintf("$%d", i))
			i++
		}
		args = vals
		sql = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qt, strings.Join(cols, ","), strings.Join(ph, ","))
	case "update":
		set := []string{}
		i := 1
		for k, v := range req.Values {
			set = append(set, fmt.Sprintf("%s=$%d", pgx.Identifier{k}.Sanitize(), i))
			args = append(args, strVal(v))
			i++
		}
		where := []string{}
		for k, v := range req.Where {
			if v == nil {
				where = append(where, fmt.Sprintf("%s IS NULL", pgx.Identifier{k}.Sanitize()))
			} else {
				where = append(where, fmt.Sprintf("%s=$%d", pgx.Identifier{k}.Sanitize(), i))
				args = append(args, strVal(v))
				i++
			}
		}
		if len(where) == 0 {
			writeJSON(w, 400, map[string]string{"error": "refusing update without WHERE (pgAdmin safety)"})
			return
		}
		sql = fmt.Sprintf("UPDATE %s SET %s WHERE %s", qt, strings.Join(set, ","), strings.Join(where, " AND "))
	case "delete":
		where := []string{}
		i := 1
		for k, v := range req.Where {
			if v == nil {
				where = append(where, fmt.Sprintf("%s IS NULL", pgx.Identifier{k}.Sanitize()))
			} else {
				where = append(where, fmt.Sprintf("%s=$%d", pgx.Identifier{k}.Sanitize(), i))
				args = append(args, strVal(v))
				i++
			}
		}
		if len(where) == 0 {
			writeJSON(w, 400, map[string]string{"error": "refusing delete without WHERE"})
			return
		}
		sql = fmt.Sprintf("DELETE FROM %s WHERE %s", qt, strings.Join(where, " AND "))
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown op"})
		return
	}
	tag, err := qq.Exec(ctx, sql, args...)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"rows_affected": tag.RowsAffected(), "in_txn": h.Mgr.InTxn(id)})
}

func strVal(v any) any {
	if s, ok := v.(string); ok && s == "__NULL__" {
		return nil
	}
	return v
}
