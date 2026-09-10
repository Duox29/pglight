package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dbclient/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	Mgr *db.Manager
}

type connectReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DbName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
	Session  string `json:"session_id"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) pool(r *http.Request) (*pgxpool.Pool, bool) {
	id := r.URL.Query().Get("session_id")
	if id == "" {
		id = r.Header.Get("X-Session-Id")
	}
	if id == "" {
		return nil, false
	}
	p, ok := h.Mgr.Get(id)
	return p, ok
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req connectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := req.Session
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	cs := db.ConnString(req.Host, req.Port, req.User, req.Password, req.DbName, req.SSLMode)
	if err := h.Mgr.Add(id, cs); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"session_id": id})
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	h.Mgr.Close(r.URL.Query().Get("session_id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func queryJSON(p *pgxpool.Pool, ctx context.Context, sql string, args ...any) ([]string, [][]any, error) {
	rows, err := p.Query(ctx, sql, args...)
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

func (h *Handler) Databases(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(p, ctx, `SELECT datname, pg_size_pretty(pg_database_size(datname)), datallowconn FROM pg_database WHERE datistemplate=false ORDER BY datname`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "size", "allow_conn"}, data))
}

func (h *Handler) Schemas(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(p, ctx, `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog','information_schema') AND schema_name NOT LIKE 'pg_%' ORDER BY schema_name`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var out []string
	for _, d := range data {
		out = append(out, fmt.Sprint(d[0]))
	}
	if out == nil {
		out = []string{}
	}
	writeJSON(w, 200, out)
}

func (h *Handler) Tables(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	sql := `SELECT t.table_schema, t.table_name, t.table_type,
		(SELECT count(*) FROM information_schema.columns c WHERE c.table_schema=t.table_schema AND c.table_name=t.table_name),
		COALESCE((SELECT reltuples::bigint FROM pg_class cl JOIN pg_namespace n ON n.oid=cl.relnamespace WHERE n.nspname=t.table_schema AND cl.relname=t.table_name),0)
		FROM information_schema.tables t WHERE t.table_schema NOT IN ('pg_catalog','information_schema')`
	var args []any
	if schema != "" {
		sql += ` AND t.table_schema=$1`
		args = append(args, schema)
	}
	sql += ` ORDER BY 1,2`
	_, data, err := queryJSON(p, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"schema", "name", "type", "columns", "est_rows"}, data))
}

func (h *Handler) Objects(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	kind := r.URL.Query().Get("kind")
	schema := r.URL.Query().Get("schema")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var sql string
	switch kind {
	case "views":
		sql = `SELECT table_schema, table_name FROM information_schema.views WHERE table_schema NOT IN ('pg_catalog','information_schema')`
	case "functions":
		sql = `SELECT n.nspname, p.proname||'('||pg_get_function_arguments(p.oid)||')' FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') ORDER BY 1,2 LIMIT 500`
	case "sequences":
		sql = `SELECT sequence_schema, sequence_name FROM information_schema.sequences ORDER BY 1,2`
	case "indexes":
		sql = `SELECT schemaname, indexname FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog','information_schema') ORDER BY 1,2 LIMIT 500`
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown kind"})
		return
	}
	if schema != "" && kind != "functions" {
		sql += ` AND table_schema='` + strings.ReplaceAll(schema, "'", "") + `'`
	}
	_, data, err := queryJSON(p, ctx, sql)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"schema", "name"}, data))
}

func (h *Handler) Columns(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(p, ctx, `
		SELECT c.column_name, c.data_type, c.is_nullable, c.column_default,
		CASE WHEN kcu.column_name IS NOT NULL THEN true ELSE false END AS is_pk
		FROM information_schema.columns c
		LEFT JOIN information_schema.table_constraints tc ON tc.table_schema=c.table_schema AND tc.table_name=c.table_name AND tc.constraint_type='PRIMARY KEY'
		LEFT JOIN information_schema.key_column_usage kcu ON kcu.constraint_name=tc.constraint_name AND kcu.column_name=c.column_name
		WHERE c.table_name=$1 AND ($2='' OR c.table_schema=$2)
		ORDER BY c.ordinal_position`, table, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "type", "nullable", "default", "pk"}, data))
}

func (h *Handler) DDL(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var ddl string
	err := p.QueryRow(ctx, `SELECT 'CREATE TABLE ' || quote_ident($1) || '.' || quote_ident($2) || E' (\n' ||
		string_agg('  ' || quote_ident(column_name) || ' ' || data_type ||
		CASE WHEN is_nullable='NO' THEN ' NOT NULL' ELSE '' END ||
		COALESCE(' DEFAULT '||column_default,''), E',\n' ORDER BY ordinal_position) || E'\n);'
		FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2`, schema, table).Scan(&ddl)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, idx, _ := queryJSON(p, ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname=$1 AND tablename=$2`, schema, table)
	_, fk, _ := queryJSON(p, ctx, `SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='f'`, schema+"."+table)
	writeJSON(w, 200, map[string]any{"ddl": ddl, "indexes": rowsToMaps([]string{"name", "def"}, idx), "foreign_keys": rowsToMaps([]string{"name", "def"}, fk)})
}

func (h *Handler) TableData(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var total int64
	_ = p.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s%s", qt, whereClauseOnly(where))).Scan(&total)
	sql := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", qt, where, limit, offset)
	h.execQuery(w, r, p, sql, total)
}

func whereClauseOnly(where string) string {
	if i := strings.Index(strings.ToUpper(where), " ORDER BY "); i >= 0 {
		return where[:i]
	}
	return where
}

type queryReq struct {
	Session string `json:"session_id"`
	SQL     string `json:"sql"`
	Limit   int    `json:"limit"`
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
	p, ok := h.Mgr.Get(req.Session)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		writeJSON(w, 400, map[string]string{"error": "empty sql"})
		return
	}
	if req.Limit > 0 && req.Limit < 5000 && isSingleSelect(sql) {
		sql = fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", strings.TrimSuffix(sql, ";"), req.Limit)
	}
	h.execQuery(w, r, p, sql, -1)
}

func isSingleSelect(sql string) bool {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	return strings.HasPrefix(strings.ToUpper(s), "SELECT") && strings.Count(s, ";") == 0
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
	p, ok := h.Mgr.Get(req.Session)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	prefix := "EXPLAIN (FORMAT JSON)"
	if req.Analyze {
		prefix = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var raw json.RawMessage
	err := p.QueryRow(ctx, prefix+" "+req.SQL).Scan(&raw)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(p, ctx, `SELECT pid, usename, application_name, state, wait_event_type||':'||coalesce(wait_event,''), left(query,200), now()-query_start AS duration FROM pg_stat_activity WHERE datname=current_database() ORDER BY query_start`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"pid", "user", "app", "state", "wait", "query", "duration"}, data))
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	pid, _ := strconv.Atoi(r.URL.Query().Get("pid"))
	kill := r.URL.Query().Get("kill") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	fn := "pg_cancel_backend"
	if kill {
		fn = "pg_terminate_backend"
	}
	var res bool
	if err := p.QueryRow(ctx, fmt.Sprintf("SELECT %s($1)", fn), pid).Scan(&res); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": res})
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
	p, ok := h.Mgr.Get(req.Session)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
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
	tag, err := p.Exec(ctx, sql, args...)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"rows_affected": tag.RowsAffected()})
}

func strVal(v any) any {
	if s, ok := v.(string); ok && s == "__NULL__" {
		return nil
	}
	return v
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pool(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, t, _ := queryJSON(p, ctx, `SELECT table_schema||'.'||table_name FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema') LIMIT 1000`)
	_, c, _ := queryJSON(p, ctx, `SELECT column_name FROM information_schema.columns GROUP BY 1 ORDER BY 1 LIMIT 1000`)
	tables, cols := []string{}, []string{}
	for _, r := range t {
		tables = append(tables, fmt.Sprint(r[0]))
	}
	for _, r := range c {
		cols = append(cols, fmt.Sprint(r[0]))
	}
	kw := []string{"SELECT", "FROM", "WHERE", "JOIN", "LEFT JOIN", "ORDER BY", "GROUP BY", "LIMIT", "INSERT INTO", "UPDATE", "DELETE FROM", "EXPLAIN", "CREATE TABLE", "ALTER TABLE"}
	writeJSON(w, 200, map[string]any{"tables": tables, "columns": cols, "keywords": kw})
}

func (h *Handler) execQuery(w http.ResponseWriter, r *http.Request, p *pgxpool.Pool, sql string, total int64) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := p.Query(ctx, sql)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
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
		if len(data) >= 1000 {
			break
		}
	}
	cmd := rows.CommandTag()
	out := map[string]any{
		"columns": cols, "types": types, "rows": data,
		"rows_affected": cmd.RowsAffected(),
		"duration_ms":   time.Since(start).Milliseconds(),
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
