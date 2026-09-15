package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/logging"

	"github.com/jackc/pgx/v5"
)

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
	qq = logging.Wrap(qq, h.Log, id)
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
