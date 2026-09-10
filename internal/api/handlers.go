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
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
  Mgr *db.Manager
  Log *logging.Logger
}

// queryTimeout caps user query execution (console, table ops). Long enough
// for analytical statements; the console Cancel button ends them sooner.
const queryTimeout = 1000 * time.Second

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
	if id != "" && h.Mgr.Alive(id) {
		// Reload/resume path: same browser session, healthy pool — reuse it
		// instead of leaking a new pool per refresh.
		writeJSON(w, 200, map[string]string{"session_id": id})
		return
	}
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

// --- transactions (DataGrip-style explicit txn) ---

func (h *Handler) Txn(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string `json:"session_id"`
		Action  string `json:"action"`
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	action := strings.ToLower(strings.TrimSpace(req.Action))
	var err error
	switch action {
	case "begin":
		err = h.Mgr.Begin(ctx, id)
	case "commit":
		err = h.Mgr.Commit(ctx, id)
	case "rollback":
		err = h.Mgr.Rollback(ctx, id)
	case "status":
		// no-op, just report
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action (begin|commit|rollback|status)"})
		return
	}
	if h.Log != nil && action != "status" {
		h.Log.LogTxn(action, err)
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "in_txn": h.Mgr.InTxn(id)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "in_txn": h.Mgr.InTxn(id)})
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

func (h *Handler) Databases(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `SELECT datname, pg_size_pretty(pg_database_size(datname)), datallowconn FROM pg_database WHERE datistemplate=false ORDER BY datname`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "size", "allow_conn"}, data))
}

func (h *Handler) Schemas(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog','information_schema') AND schema_name NOT LIKE 'pg_%' ORDER BY schema_name`)
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
	q, _, ok := h.q(r)
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
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"schema", "name", "type", "columns", "est_rows"}, data))
}

func (h *Handler) Objects(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	kind := r.URL.Query().Get("kind")
	schema := r.URL.Query().Get("schema")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var sql string
	var args []any
	switch kind {
	case "views":
		sql = `SELECT table_schema, table_name FROM information_schema.views WHERE table_schema NOT IN ('pg_catalog','information_schema')`
		if schema != "" {
			sql += ` AND table_schema=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2`
	case "matviews":
		sql = `SELECT schemaname, matviewname FROM pg_matviews`
		if schema != "" {
			sql += ` WHERE schemaname=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2`
	case "foreign":
		sql = `SELECT foreign_table_schema, foreign_table_name FROM information_schema.foreign_tables WHERE foreign_table_schema NOT IN ('pg_catalog','information_schema')`
		if schema != "" {
			sql += ` AND table_schema=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2`
	case "functions":
		sql = `SELECT n.nspname, p.proname||'('||pg_get_function_arguments(p.oid)||')', l.lanname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang WHERE n.nspname NOT IN ('pg_catalog','information_schema')`
		if schema != "" {
			sql += ` AND n.nspname=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2 LIMIT 500`
	case "sequences":
		sql = `SELECT sequence_schema, sequence_name FROM information_schema.sequences`
		if schema != "" {
			sql += ` WHERE sequence_schema=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2`
	case "indexes":
		sql = `SELECT schemaname, indexname FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog','information_schema')`
		if schema != "" {
			sql += ` AND schemaname=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2 LIMIT 500`
	case "types":
		sql = `SELECT n.nspname, t.typname FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND t.typtype IN ('e','d','c') AND t.oid NOT IN (SELECT reltype FROM pg_class WHERE relkind IN ('r','v','m','f','p'))`
		if schema != "" {
			sql += ` AND n.nspname=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2 LIMIT 500`
	case "triggers":
		sql = `SELECT event_object_schema, trigger_name FROM information_schema.triggers WHERE trigger_schema NOT IN ('pg_catalog','information_schema')`
		if schema != "" {
			sql += ` AND event_object_schema=$1`
			args = append(args, schema)
		}
		sql += ` ORDER BY 1,2 LIMIT 500`
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown kind (views|matviews|foreign|functions|sequences|indexes|types|triggers)"})
		return
	}
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	cols := []string{"schema", "name"}
	if kind == "functions" {
		cols = []string{"schema", "name", "lang"}
	}
	writeJSON(w, 200, rowsToMaps(cols, data))
}

func (h *Handler) Columns(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `
		SELECT c.column_name, c.data_type, c.is_nullable, c.column_default,
		CASE WHEN kcu.column_name IS NOT NULL THEN true ELSE false END AS is_pk,
		COALESCE(col_description((c.table_schema||'.'||c.table_name)::regclass, c.ordinal_position), '') AS comment
		FROM information_schema.columns c
		LEFT JOIN information_schema.table_constraints tc ON tc.table_schema=c.table_schema AND tc.table_name=c.table_name AND tc.constraint_type='PRIMARY KEY'
		LEFT JOIN information_schema.key_column_usage kcu ON kcu.constraint_name=tc.constraint_name AND kcu.column_name=c.column_name AND kcu.table_schema=c.table_schema
		WHERE c.table_name=$1 AND ($2='' OR c.table_schema=$2)
		ORDER BY c.ordinal_position`, table, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "type", "nullable", "default", "pk", "comment"}, data))
}

func (h *Handler) DDL(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
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
	err := q.QueryRow(ctx, `SELECT 'CREATE TABLE ' || quote_ident($1) || '.' || quote_ident($2) || E' (\n' ||
		string_agg('  ' || quote_ident(column_name) || ' ' || data_type ||
		CASE WHEN is_nullable='NO' THEN ' NOT NULL' ELSE '' END ||
		COALESCE(' DEFAULT '||column_default,''), E',\n' ORDER BY ordinal_position) || E'\n);'
		FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2`, schema, table).Scan(&ddl)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, idx, _ := queryJSON(q, ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname=$1 AND tablename=$2`, schema, table)
	_, fk, _ := queryJSON(q, ctx, `SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=($1||'.'||$2)::regclass`, schema, table)
	_, cons, _ := queryJSON(q, ctx, `SELECT conname, CASE contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE' ELSE contype::text END, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=($1||'.'||$2)::regclass ORDER BY 2,1`, schema, table)
	_, trg, _ := queryJSON(q, ctx, `SELECT trigger_name, event_manipulation||' '||action_timing||' '||action_statement FROM information_schema.triggers WHERE event_object_schema=$1 AND event_object_table=$2`, schema, table)
	var owner, comment string
	_ = q.QueryRow(ctx, `SELECT COALESCE((SELECT relowner::regrole::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2),''), COALESCE(obj_description(($1||'.'||$2)::regclass,'pg_class'),'')`, schema, table).Scan(&owner, &comment)
	writeJSON(w, 200, map[string]any{
		"ddl": ddl, "owner": owner, "comment": comment,
		"indexes":      rowsToMaps([]string{"name", "def"}, idx),
		"foreign_keys": rowsToMaps([]string{"name", "def"}, fk),
		"constraints":  rowsToMaps([]string{"name", "type", "def"}, cons),
		"triggers":     rowsToMaps([]string{"name", "def"}, trg),
	})
}

func (h *Handler) ViewDef(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, name := r.URL.Query().Get("schema"), r.URL.Query().Get("name")
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	if schema == "" {
		schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var def string
	if err := q.QueryRow(ctx, `SELECT pg_get_viewdef(($1||'.'||$2)::regclass, true)`, schema, name).Scan(&def); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"definition": def})
}

func (h *Handler) FuncDef(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, name := r.URL.Query().Get("schema"), r.URL.Query().Get("name")
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	// name may be "proname(args)" from explorer; strip signature for lookup
	base := name
	if i := strings.Index(base, "("); i >= 0 {
		base = base[:i]
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	args := []any{base}
	sql := `SELECT n.nspname, p.proname||'('||pg_get_function_arguments(p.oid)||')', pg_get_functiondef(p.oid), l.lanname, p.prokind
		FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang
		WHERE p.proname=$1`
	if schema != "" {
		sql += ` AND n.nspname=$2`
		args = append(args, schema)
	}
	sql += ` ORDER BY 1,2 LIMIT 20`
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"schema", "name", "def", "lang", "kind"}, data))
}

func (h *Handler) Constraints(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, table := r.URL.Query().Get("schema"), r.URL.Query().Get("table")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var sql string
	var args []any
	if table != "" {
		sql = `SELECT conname, CASE contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE' ELSE contype::text END, pg_get_constraintdef(oid, true) FROM pg_constraint WHERE conrelid=($1||'.'||$2)::regclass ORDER BY 2,1`
		s := schema
		if s == "" {
			s = "public"
		}
		args = []any{s, table}
	} else if schema != "" {
		sql = `SELECT c.conname, cl.relname, CASE c.contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE' ELSE c.contype::text END, pg_get_constraintdef(c.oid, true) FROM pg_constraint c JOIN pg_class cl ON cl.oid=c.conrelid JOIN pg_namespace n ON n.oid=cl.relnamespace WHERE n.nspname=$1 ORDER BY 2,3,1 LIMIT 500`
		args = []any{schema}
	} else {
		writeJSON(w, 400, map[string]string{"error": "schema or table required"})
		return
	}
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if table != "" {
		writeJSON(w, 200, rowsToMaps([]string{"name", "type", "def"}, data))
	} else {
		writeJSON(w, 200, rowsToMaps([]string{"name", "table", "type", "def"}, data))
	}
}

func (h *Handler) Triggers(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, table := r.URL.Query().Get("schema"), r.URL.Query().Get("table")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	sql := `SELECT trigger_name, event_object_table, event_manipulation, action_timing, action_statement FROM information_schema.triggers WHERE trigger_schema NOT IN ('pg_catalog','information_schema')`
	var args []any
	if schema != "" {
		sql += ` AND event_object_schema=$1`
		args = append(args, schema)
		if table != "" {
			sql += ` AND event_object_table=$2`
			args = append(args, table)
		}
	} else if table != "" {
		sql += ` AND event_object_table=$1`
		args = append(args, table)
	}
	sql += ` ORDER BY 2,1 LIMIT 500`
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "table", "event", "timing", "statement"}, data))
}

func (h *Handler) TableStats(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, table := r.URL.Query().Get("schema"), r.URL.Query().Get("table")
	if table == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	if schema == "" {
		schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `
		SELECT pg_size_pretty(pg_total_relation_size(($1||'.'||$2)::regclass)),
		       pg_size_pretty(pg_relation_size(($1||'.'||$2)::regclass)),
		       pg_size_pretty(pg_indexes_size(($1||'.'||$2)::regclass)),
		       COALESCE((SELECT reltuples::bigint FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2),0),
		       COALESCE(s.seq_scan,0), COALESCE(s.idx_scan,0), COALESCE(s.n_live_tup,0), COALESCE(s.n_dead_tup,0),
		       COALESCE(s.last_vacuum::text,''), COALESCE(s.last_autovacuum::text,''), COALESCE(s.last_analyze::text,''), COALESCE(s.last_autoanalyze::text,'')
		FROM pg_stat_user_tables s WHERE s.schemaname=$1 AND s.relname=$2`, schema, table)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		// table may have no stats row yet (never vacuumed); return sizes only
		var total, rel, idx string
		_ = q.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size(($1||'.'||$2)::regclass)), pg_size_pretty(pg_relation_size(($1||'.'||$2)::regclass)), pg_size_pretty(pg_indexes_size(($1||'.'||$2)::regclass))`, schema, table).Scan(&total, &rel, &idx)
		writeJSON(w, 200, map[string]any{"total_size": total, "table_size": rel, "indexes_size": idx})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"total_size", "table_size", "indexes_size", "est_rows", "seq_scan", "idx_scan", "live_tup", "dead_tup", "last_vacuum", "last_autovacuum", "last_analyze", "last_autoanalyze"}, data)[0])
}

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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var total int64
	_ = qq.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s%s", qt, whereClauseOnly(where))).Scan(&total)
	sql := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", qt, where, limit, offset)
	h.execQuery(w, r, qq, sid, sql, total)
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

// splitStatements splits SQL on semicolons outside strings/comments,
// including PostgreSQL dollar-quoted bodies.
func splitStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	n := len(sql)
	i := 0
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
				out = append(out, s)
			}
			cur.Reset()
			i++
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
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
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		writeJSON(w, 400, map[string]string{"error": "empty sql"})
		return
	}
	stmts := splitStatements(sql)
	if len(stmts) > 1 {
		start := time.Now()
		results := []map[string]any{}
		for _, st := range stmts {
			res, qerr := h.runSingle(r, qq, st, req.Limit)
			if qerr != nil {
				writeJSON(w, 400, map[string]any{"error": qerr.Error(), "statement": st, "results": results, "in_txn": h.Mgr.InTxn(id)})
				return
			}
			results = append(results, res)
		}
		writeJSON(w, 200, map[string]any{"results": results, "duration_ms": time.Since(start).Milliseconds(), "in_txn": h.Mgr.InTxn(id)})
		return
	}
	if req.Limit > 0 && req.Limit < 5000 && isSingleSelect(sql) {
		sql = fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", strings.TrimSuffix(sql, ";"), req.Limit)
	}
	h.execQuery(w, r, qq, id, sql, -1)
}

func isSingleSelect(sql string) bool {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	return strings.HasPrefix(strings.ToUpper(s), "SELECT") && strings.Count(s, ";") == 0
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
		data = append(data, vals)
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

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `SELECT pid, usename, application_name, state, COALESCE(wait_event_type||':'||wait_event,''), left(COALESCE(query,''),200), COALESCE((now()-query_start)::text,'') AS duration FROM pg_stat_activity WHERE datname=current_database() ORDER BY query_start NULLS LAST`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"pid", "user", "app", "state", "wait", "query", "duration"}, data))
}

func (h *Handler) Locks(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `
		SELECT l.pid, COALESCE(a.usename,''), l.locktype, COALESCE(l.database::regclass::text,''), COALESCE(l.relation::regclass::text,''),
		       l.mode, l.granted, COALESCE((now()-a.query_start)::text,''), left(COALESCE(a.query,''),160)
		FROM pg_locks l LEFT JOIN pg_stat_activity a USING (pid)
		WHERE l.database = (SELECT oid FROM pg_database WHERE datname=current_database())
		ORDER BY NOT l.granted DESC, a.query_start NULLS LAST LIMIT 200`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"pid", "user", "locktype", "database", "relation", "mode", "granted", "duration", "query"}, data))
}

func (h *Handler) ServerInfo(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var version, db, dbSize, uptime, verNum string
	var conns, maxConns int
	_ = q.QueryRow(ctx, `SELECT version(), current_database(), pg_size_pretty(pg_database_size(current_database())), COALESCE((now()-pg_postmaster_start_time())::text,''), current_setting('server_version_num'), (SELECT count(*) FROM pg_stat_activity), current_setting('max_connections')::int`).Scan(&version, &db, &dbSize, &uptime, &verNum, &conns, &maxConns)
	_, settings, _ := queryJSON(q, ctx, `SELECT name, setting, unit, context FROM pg_settings WHERE name IN ('shared_buffers','work_mem','effective_cache_size','maintenance_work_mem','max_connections','shared_preload_libraries','TimeZone','port','listen_addresses','max_parallel_workers') ORDER BY 1`)
	writeJSON(w, 200, map[string]any{
		"version": version, "database": db, "db_size": dbSize, "uptime": uptime,
		"version_num": verNum, "connections": conns, "max_connections": maxConns,
		"settings": rowsToMaps([]string{"name", "setting", "unit", "context"}, settings),
	})
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, dbs, err := queryJSON(q, ctx, `SELECT s.datname, s.numbackends, s.xact_commit, s.xact_rollback, s.blks_read, s.blks_hit, CASE WHEN s.blks_hit+s.blks_read=0 THEN 100 ELSE round(100.0*s.blks_hit/(s.blks_hit+s.blks_read),1) END, pg_size_pretty(pg_database_size(s.datname)) FROM pg_stat_database s JOIN pg_database d ON d.datname=s.datname WHERE NOT d.datistemplate ORDER BY 2 DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, tbls, _ := queryJSON(q, ctx, `SELECT schemaname, relname, pg_size_pretty(pg_total_relation_size((schemaname||'.'||relname)::regclass)), seq_scan, idx_scan, n_live_tup, n_dead_tup FROM pg_stat_user_tables ORDER BY pg_total_relation_size((schemaname||'.'||relname)::regclass) DESC LIMIT 20`)
	writeJSON(w, 200, map[string]any{
		"databases":  rowsToMaps([]string{"name", "backends", "commits", "rollbacks", "disk_reads", "cache_hits", "hit_ratio", "size"}, dbs),
		"top_tables": rowsToMaps([]string{"schema", "table", "size", "seq_scan", "idx_scan", "live", "dead"}, tbls),
	})
}

func (h *Handler) Roles(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `
		SELECT r.rolname, r.rolsuper, r.rolinherit, r.rolcreaterole, r.rolcreatedb, r.rolcanlogin, r.rolreplication, r.rolconnlimit,
		       COALESCE((SELECT string_agg(m.rolname, ', ' ORDER BY m.rolname) FROM pg_auth_members am JOIN pg_roles m ON m.oid=am.roleid WHERE am.member=r.oid), '')
		FROM pg_roles r ORDER BY r.rolname`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "superuser", "inherit", "createrole", "createdb", "login", "replication", "connlimit", "member_of"}, data))
}

func (h *Handler) Extensions(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(q, ctx, `SELECT name, default_version, installed_version, comment FROM pg_available_extensions ORDER BY installed_version IS NULL, name`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"name", "default_version", "installed_version", "comment"}, data))
}

func (h *Handler) Types(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	sql := `SELECT n.nspname, t.typname,
		CASE t.typtype WHEN 'e' THEN 'enum' WHEN 'd' THEN 'domain' WHEN 'c' THEN 'composite' WHEN 'b' THEN 'base' WHEN 'p' THEN 'pseudo' ELSE t.typtype::text END,
		COALESCE(obj_description(t.oid,'pg_type'),''),
		CASE WHEN t.typtype='e' THEN (SELECT string_agg(enumlabel, ', ' ORDER BY enumsortorder) FROM pg_enum WHERE enumtypid=t.oid) ELSE '' END
		FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
		WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_%' AND t.typtype IN ('e','d','c') AND t.oid NOT IN (SELECT reltype FROM pg_class WHERE relkind IN ('r','v','m','f','p'))`
	var args []any
	if schema != "" {
		sql += ` AND n.nspname=$1`
		args = append(args, schema)
	}
	sql += ` ORDER BY 1,2 LIMIT 500`
	_, data, err := queryJSON(q, ctx, sql, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"schema", "name", "kind", "comment", "labels"}, data))
}

func (h *Handler) ERD(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, nodes, err := queryJSON(q, ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 ORDER BY 1 LIMIT 100`, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, edges, err := queryJSON(q, ctx, `
		SELECT tc.constraint_name, tc.table_name, kcu.column_name, ccu.table_schema, ccu.table_name, ccu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON kcu.constraint_name=tc.constraint_name AND kcu.table_schema=tc.table_schema
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name=tc.constraint_name
		WHERE tc.constraint_type='FOREIGN KEY' AND tc.table_schema=$1 LIMIT 500`, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	nn := []string{}
	for _, n := range nodes {
		nn = append(nn, fmt.Sprint(n[0]))
	}
	writeJSON(w, 200, map[string]any{
		"schema": schema,
		"nodes":  nn,
		"edges":  rowsToMaps([]string{"fk", "src_table", "src_col", "dst_schema", "dst_table", "dst_col"}, edges),
	})
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

func (h *Handler) Maintenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string `json:"session_id"`
		Schema  string `json:"schema"`
		Table   string `json:"table"`
		Op      string `json:"op"`
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
	if req.Table == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	schema := req.Schema
	if schema == "" {
		schema = "public"
	}
	if h.Mgr.InTxn(id) {
		writeJSON(w, 400, map[string]string{"error": "VACUUM/ANALYZE cannot run inside a transaction — Commit/Rollback first (pgAdmin safety)"})
		return
	}
	qt := pgx.Identifier{schema, req.Table}.Sanitize()
	var sql string
	switch strings.ToLower(req.Op) {
	case "vacuum":
		sql = "VACUUM (VERBOSE false) " + qt
	case "vacuum_full":
		sql = "VACUUM (FULL, ANALYZE) " + qt
	case "analyze":
		sql = "ANALYZE " + qt
	case "reindex":
		sql = "REINDEX TABLE " + qt
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown op (vacuum|vacuum_full|analyze|reindex)"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	tag, err := qq.Exec(ctx, sql)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": tag.String()})
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

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
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
	if err := q.QueryRow(ctx, fmt.Sprintf("SELECT %s($1)", fn), pid).Scan(&res); err != nil {
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
	id := sessionFromBody(req.Session, r)
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
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

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, t, _ := queryJSON(q, ctx, `SELECT table_schema||'.'||table_name FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema') LIMIT 1000`)
	_, c, _ := queryJSON(q, ctx, `SELECT column_name FROM information_schema.columns GROUP BY 1 ORDER BY 1 LIMIT 1000`)
	_, f, _ := queryJSON(q, ctx, `SELECT DISTINCT proname FROM pg_proc JOIN pg_namespace n ON n.oid=pronamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') LIMIT 500`)
	tables, cols, funcs := []string{}, []string{}, []string{}
	for _, r := range t {
		tables = append(tables, fmt.Sprint(r[0]))
	}
	for _, r := range c {
		cols = append(cols, fmt.Sprint(r[0]))
	}
	for _, r := range f {
		funcs = append(funcs, fmt.Sprint(r[0]))
	}
	kw := []string{"SELECT", "FROM", "WHERE", "JOIN", "LEFT JOIN", "ORDER BY", "GROUP BY", "HAVING", "LIMIT", "OFFSET", "INSERT INTO", "VALUES", "UPDATE", "SET", "DELETE FROM", "EXPLAIN", "ANALYZE", "CREATE TABLE", "ALTER TABLE", "DROP TABLE", "CREATE INDEX", "VACUUM", "BEGIN", "COMMIT", "ROLLBACK", "WITH", "RETURNING", "ON CONFLICT", "DISTINCT", "COUNT", "SUM", "AVG", "NOW()", "COALESCE"}
	writeJSON(w, 200, map[string]any{"tables": tables, "columns": cols, "functions": funcs, "keywords": kw})
}

func (h *Handler) execQuery(w http.ResponseWriter, r *http.Request, qq db.Querier, sid string, sql string, total int64) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	rows, err := qq.Query(ctx, sql)
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
		"in_txn":        h.Mgr.InTxn(sid),
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
