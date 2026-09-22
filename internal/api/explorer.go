package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

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
			sql += ` AND foreign_table_schema=$1`
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
		CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END AS is_pk,
		COALESCE(col_description(to_regclass(format('%I.%I', c.table_schema, c.table_name)), c.ordinal_position), '') AS comment
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT src_ns.nspname AS sn, src.relname AS tn, a.attname AS column_name
			FROM pg_constraint con
			JOIN pg_class src ON src.oid = con.conrelid
			JOIN pg_namespace src_ns ON src_ns.oid = src.relnamespace
			JOIN LATERAL unnest(con.conkey) AS k(attnum) ON true
			JOIN pg_attribute a ON a.attrelid = src.oid AND a.attnum = k.attnum
			WHERE con.contype = 'p'
		) pk ON pk.sn = c.table_schema AND pk.tn = c.table_name AND pk.column_name = c.column_name
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
	_, fk, _ := queryJSON(q, ctx, `SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass(format('%I.%I', $1::text, $2::text)) AND contype='f'`, schema, table)
	_, cons, _ := queryJSON(q, ctx, `SELECT conname, CASE contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE' ELSE contype::text END, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass(format('%I.%I', $1::text, $2::text)) ORDER BY 2,1`, schema, table)
	_, trg, _ := queryJSON(q, ctx, `SELECT trigger_name, event_manipulation||' '||action_timing||' '||action_statement FROM information_schema.triggers WHERE event_object_schema=$1 AND event_object_table=$2`, schema, table)
	var owner, comment string
	_ = q.QueryRow(ctx, `SELECT COALESCE((SELECT relowner::regrole::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2),''), COALESCE(obj_description(to_regclass(format('%I.%I', $1::text, $2::text)),'pg_class'),'')`, schema, table).Scan(&owner, &comment)
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
	if err := q.QueryRow(ctx, `SELECT pg_get_viewdef(to_regclass(format('%I.%I', $1::text, $2::text)), true)`, schema, name).Scan(&def); err != nil {
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

func (h *Handler) SeqDef(w http.ResponseWriter, r *http.Request) {
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
	_, data, err := queryJSON(q, ctx, `SELECT data_type, start_value, minimum_value, maximum_value, increment, cycle_option FROM information_schema.sequences WHERE sequence_schema=$1 AND sequence_name=$2`, schema, name)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		writeJSON(w, 400, map[string]string{"error": "sequence not found"})
		return
	}
	str := func(v any) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	dataType, startVal, minVal, maxVal, incr, cycle := str(data[0][0]), str(data[0][1]), str(data[0][2]), str(data[0][3]), str(data[0][4]), str(data[0][5])
	var cacheSize int64 = 1
	_ = q.QueryRow(ctx, `SELECT seqcache FROM pg_sequence WHERE seqrelid=to_regclass(format('%I.%I', $1::text, $2::text))`, schema, name).Scan(&cacheSize)
	var lastVal string
	_ = q.QueryRow(ctx, `SELECT last_value::text FROM `+pgx.Identifier{schema, name}.Sanitize()).Scan(&lastVal)
	qi := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	var sb strings.Builder
	sb.WriteString("CREATE SEQUENCE " + qi(schema) + "." + qi(name))
	if startVal != "" {
		sb.WriteString(" START WITH " + startVal)
	}
	if incr != "" {
		sb.WriteString(" INCREMENT BY " + incr)
	}
	if minVal != "" {
		sb.WriteString(" MINVALUE " + minVal)
	} else {
		sb.WriteString(" NO MINVALUE")
	}
	if maxVal != "" && maxVal != "9223372036854775807" {
		sb.WriteString(" MAXVALUE " + maxVal)
	} else {
		sb.WriteString(" NO MAXVALUE")
	}
	sb.WriteString(fmt.Sprintf(" CACHE %d", cacheSize))
	if strings.EqualFold(cycle, "yes") {
		sb.WriteString(" CYCLE")
	} else {
		sb.WriteString(" NO CYCLE")
	}
	sb.WriteString(";")
	writeJSON(w, 200, map[string]any{
		"schema": schema, "name": name,
		"data_type": dataType, "start_value": startVal,
		"minimum_value": minVal, "maximum_value": maxVal,
		"increment": incr, "cycle_option": cycle,
		"cache_size": cacheSize, "last_value": lastVal,
		"definition": sb.String(),
	})
}

func (h *Handler) TypeDef(w http.ResponseWriter, r *http.Request) {
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
	_, data, err := queryJSON(q, ctx, `SELECT
		CASE t.typtype WHEN 'e' THEN 'enum' WHEN 'd' THEN 'domain' WHEN 'c' THEN 'composite' WHEN 'b' THEN 'base' WHEN 'p' THEN 'pseudo' ELSE t.typtype::text END,
		COALESCE(obj_description(t.oid,'pg_type'),''),
		COALESCE(CASE WHEN t.typtype='e' THEN (SELECT string_agg(enumlabel, chr(31) ORDER BY enumsortorder) FROM pg_enum WHERE enumtypid=t.oid) ELSE '' END, '')
		FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
		WHERE n.nspname=$1 AND t.typname=$2 AND t.typtype IN ('e','d','c') AND t.oid NOT IN (SELECT reltype FROM pg_class WHERE relkind IN ('r','v','m','f','p'))`, schema, name)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		writeJSON(w, 400, map[string]string{"error": "type not found"})
		return
	}
	str := func(v any) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	kind, comment, labels := str(data[0][0]), str(data[0][1]), str(data[0][2])
	qi := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	var def, display string
	if kind == "enum" {
		vals := []string{}
		if labels != "" {
			for _, l := range strings.Split(labels, "\x1f") {
				vals = append(vals, `'`+strings.ReplaceAll(l, `'`, `''`)+`'`)
			}
		}
		def = fmt.Sprintf("CREATE TYPE %s.%s AS ENUM (%s);", qi(schema), qi(name), strings.Join(vals, ", "))
	} else {
		_ = q.QueryRow(ctx, `SELECT format_type(t.oid, NULL) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname=$1 AND t.typname=$2`, schema, name).Scan(&display)
	}
	writeJSON(w, 200, map[string]any{
		"schema": schema, "name": name,
		"kind": kind, "comment": comment, "labels": strings.ReplaceAll(labels, "\x1f", ", "),
		"definition": def, "display": display,
	})
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
		sql = `SELECT conname, CASE contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE' ELSE contype::text END, pg_get_constraintdef(oid, true) FROM pg_constraint WHERE conrelid=to_regclass(format('%I.%I', $1::text, $2::text)) ORDER BY 2,1`
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
	sql := `SELECT trigger_name, event_object_table, event_manipulation, action_timing, action_statement,
		COALESCE((SELECT tgenabled::text FROM pg_trigger t WHERE t.tgrelid=to_regclass(format('%I.%I', event_object_schema, event_object_table)) AND t.tgname=trigger_name LIMIT 1),'O')
		FROM information_schema.triggers WHERE trigger_schema NOT IN ('pg_catalog','information_schema')`
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
	writeJSON(w, 200, rowsToMaps([]string{"name", "table", "event", "timing", "statement", "enabled"}, data))
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
		SELECT pg_size_pretty(pg_total_relation_size(to_regclass(format('%I.%I', $1::text, $2::text)))),
		       pg_size_pretty(pg_relation_size(to_regclass(format('%I.%I', $1::text, $2::text)))),
		       pg_size_pretty(pg_indexes_size(to_regclass(format('%I.%I', $1::text, $2::text)))),
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
		_ = q.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size(to_regclass(format('%I.%I', $1::text, $2::text)))), pg_size_pretty(pg_relation_size(to_regclass(format('%I.%I', $1::text, $2::text)))), pg_size_pretty(pg_indexes_size(to_regclass(format('%I.%I', $1::text, $2::text))))`, schema, table).Scan(&total, &rel, &idx)
		writeJSON(w, 200, map[string]any{"total_size": total, "table_size": rel, "indexes_size": idx})
		return
	}
	writeJSON(w, 200, rowsToMaps([]string{"total_size", "table_size", "indexes_size", "est_rows", "seq_scan", "idx_scan", "live_tup", "dead_tup", "last_vacuum", "last_autovacuum", "last_analyze", "last_autoanalyze"}, data)[0])
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
	if r.Method == http.MethodGet && r.URL.Query().Get("layout") == "1" {
		cid := strings.TrimSpace(r.URL.Query().Get("connection_id"))
		schema := strings.TrimSpace(r.URL.Query().Get("schema"))
		if cid == "" || schema == "" {
			writeJSON(w, 400, map[string]string{"error": "connection_id and schema required"})
			return
		}
		x, err := h.Store.GetErdLayout(r.Context(), h.UserID, cid, schema)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, 200, map[string]any{"layout": map[string]any{}, "viewport": nil})
				return
			}
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var layout, viewport any
		_ = json.Unmarshal([]byte(x.LayoutJSON), &layout)
		_ = json.Unmarshal([]byte(x.ViewportJSON), &viewport)
		writeJSON(w, 200, map[string]any{"layout": layout, "viewport": viewport, "updated_at": x.UpdatedAt})
		return
	}
	if r.Method == http.MethodDelete && r.URL.Query().Get("layout") == "1" {
		cid := strings.TrimSpace(r.URL.Query().Get("connection_id"))
		schema := strings.TrimSpace(r.URL.Query().Get("schema"))
		if cid == "" || schema == "" {
			writeJSON(w, 400, map[string]string{"error": "connection_id and schema required"})
			return
		}
		if err := h.Store.DeleteErdLayout(r.Context(), h.UserID, cid, schema); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == http.MethodPost && r.URL.Query().Get("layout") == "1" {
		var req struct {
			ConnectionID string `json:"connection_id"`
			Schema       string `json:"schema"`
			Layout       any    `json:"layout"`
			Viewport     any    `json:"viewport"`
		}
		if decodeBody(r, &req) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		if req.ConnectionID == "" || req.Schema == "" {
			writeJSON(w, 400, map[string]string{"error": "connection_id and schema required"})
			return
		}
		lb, _ := json.Marshal(req.Layout)
		vb, _ := json.Marshal(req.Viewport)
		if err := h.Store.UpsertErdLayout(r.Context(), h.UserID, req.ConnectionID, req.Schema, string(lb), string(vb)); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
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
	// Foreign keys come from pg_constraint/pg_class/pg_attribute (OID joins),
	// never information_schema constraint-name joins: names like fk_user_id
	// repeat across tables and cross-match unrelated constraints.
	_, edges, err := queryJSON(q, ctx, `
		SELECT con.conname, src.relname, src_att.attname, dst_ns.nspname, dst.relname, dst_att.attname
		FROM pg_constraint con
		JOIN pg_class src ON src.oid = con.conrelid
		JOIN pg_namespace src_ns ON src_ns.oid = src.relnamespace
		JOIN pg_class dst ON dst.oid = con.confrelid
		JOIN pg_namespace dst_ns ON dst_ns.oid = dst.relnamespace
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS sk(attnum, ord) ON true
		JOIN pg_attribute src_att ON src_att.attrelid = src.oid AND src_att.attnum = sk.attnum
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS tk(attnum, ord) ON tk.ord = sk.ord
		JOIN pg_attribute dst_att ON dst_att.attrelid = dst.oid AND dst_att.attnum = tk.attnum
		WHERE con.contype = 'f' AND src_ns.nspname = $1
		ORDER BY con.conname, sk.ord LIMIT 500`, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Per-table columns with PK flags + FK columns derived from edges, so the
	// ERD canvas can render PK/FK badges without one query per table. One
	// capped catalog query for all tables in the schema (additive: nodes and
	// edges keep their existing shapes).
	_, crows, err := queryJSON(q, ctx, `
		SELECT c.table_name, c.column_name, c.data_type,
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END AS is_pk
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT src_ns.nspname AS sn, src.relname AS tn, a.attname AS column_name
			FROM pg_constraint con
			JOIN pg_class src ON src.oid = con.conrelid
			JOIN pg_namespace src_ns ON src_ns.oid = src.relnamespace
			JOIN LATERAL unnest(con.conkey) AS k(attnum) ON true
			JOIN pg_attribute a ON a.attrelid = src.oid AND a.attnum = k.attnum
			WHERE con.contype = 'p' AND src_ns.nspname = $1
		) pk ON pk.sn = c.table_schema AND pk.tn = c.table_name AND pk.column_name = c.column_name
		WHERE c.table_schema=$1
		ORDER BY c.table_name, c.ordinal_position
		LIMIT 5000`, schema)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	nn := []string{}
	for _, n := range nodes {
		nn = append(nn, fmt.Sprint(n[0]))
	}
	fkCols := map[string]map[string]bool{}
	for _, e := range edges {
		if len(e) < 3 {
			continue
		}
		src, col := fmt.Sprint(e[1]), fmt.Sprint(e[2])
		if fkCols[src] == nil {
			fkCols[src] = map[string]bool{}
		}
		fkCols[src][col] = true
	}
	cols := map[string][]map[string]any{}
	for _, r := range crows {
		if len(r) < 4 {
			continue
		}
		tbl, col := fmt.Sprint(r[0]), fmt.Sprint(r[1])
		isPK := r[3] == true
		if s, ok := r[3].(string); ok {
			isPK = strings.EqualFold(s, "t") || strings.EqualFold(s, "true")
		}
		cols[tbl] = append(cols[tbl], map[string]any{
			"name": col, "type": fmt.Sprint(r[2]),
			"pk": isPK, "fk": fkCols[tbl][col],
		})
	}
	for _, n := range nn {
		if cols[n] == nil {
			cols[n] = []map[string]any{}
		}
	}
	writeJSON(w, 200, map[string]any{
		"schema":  schema,
		"nodes":   nn,
		"edges":   rowsToMaps([]string{"fk", "src_table", "src_col", "dst_schema", "dst_table", "dst_col"}, edges),
		"columns": cols,
	})
}
