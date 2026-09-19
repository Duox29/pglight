package test

import (
	"fmt"
	"strings"
	"testing"
)

func TestAdminReads(t *testing.T) {
	h, sid := newHandler(t)
	cases := []struct {
		name string
		fn   func() (int, string)
		want []string
	}{
		{"activity", func() (int, string) { return callGET(t, h.Activity, withSID(sid, "/api/activity")) }, []string{`"pid"`}},
		{"locks", func() (int, string) { return callGET(t, h.Locks, withSID(sid, "/api/locks")) }, []string{`"mode"`}},
		{"server-info", func() (int, string) { return callGET(t, h.ServerInfo, withSID(sid, "/api/server-info")) }, []string{`"version"`, `"database"`}},
		{"stats", func() (int, string) { return callGET(t, h.Stats, withSID(sid, "/api/stats")) }, []string{`"databases"`, `"top_tables"`}},
		{"roles", func() (int, string) { return callGET(t, h.Roles, withSID(sid, "/api/roles")) }, []string{`"name"`}},
		{"extensions", func() (int, string) { return callGET(t, h.Extensions, withSID(sid, "/api/extensions")) }, []string{`"name"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/happy", func(t *testing.T) {
			code, body := tc.fn()
			requireStatus(t, body, code, 200)
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Fatalf("%s lacks %s: %s", tc.name, w, body)
				}
			}
		})
	}
	// deep content per endpoint
	t.Run("activity/content", func(t *testing.T) {
		code, body := callGET(t, h.Activity, withSID(sid, "/api/activity"))
		requireStatus(t, body, code, 200)
		rows := arrObjs(t, body)
		if len(rows) == 0 {
			t.Fatalf("activity empty: %s", body)
		}
		requireKeys(t, body, rows[0], "pid", "user", "app", "state", "wait", "query", "duration")
		mine := findObj(t, body, "user", "postgres", rows)
		if pid, _ := mine["pid"].(float64); pid <= 0 {
			t.Fatalf("bad pid %v: %s", mine["pid"], body)
		}
	})
	t.Run("locks/content", func(t *testing.T) {
		code, body := callGET(t, h.Locks, withSID(sid, "/api/locks"))
		requireStatus(t, body, code, 200)
		rows := arrObjs(t, body)
		if len(rows) == 0 {
			t.Fatalf("locks empty (own session holds locks): %s", body)
		}
		requireKeys(t, body, rows[0], "pid", "user", "locktype", "database", "relation", "mode", "granted", "duration", "query")
	})
	t.Run("server-info/content", func(t *testing.T) {
		code, body := callGET(t, h.ServerInfo, withSID(sid, "/api/server-info"))
		requireStatus(t, body, code, 200)
		info := decodeObj(t, body)
		requireDeep(t, body, "database", info["database"], "postgres")
		if !strings.Contains(fmt.Sprint(info["version"]), "PostgreSQL") {
			t.Fatalf("version wrong: %s", body)
		}
		if n, _ := info["max_connections"].(float64); n <= 0 {
			t.Fatalf("max_connections wrong: %s", body)
		}
		if n, _ := info["connections"].(float64); n < 1 {
			t.Fatalf("connections wrong: %s", body)
		}
		settings, _ := info["settings"].([]any)
		var sb map[string]any
		for _, e := range settings {
			if m, _ := e.(map[string]any); m != nil && m["name"] == "shared_buffers" {
				sb = m
			}
		}
		if sb == nil {
			t.Fatalf("shared_buffers missing: %s", body)
		}
		requireKeys(t, body, sb, "name", "setting", "unit", "context")
		if fmt.Sprint(sb["setting"]) == "" || fmt.Sprint(sb["unit"]) == "" {
			t.Fatalf("shared_buffers content empty: %s", body)
		}
	})
	t.Run("stats/content", func(t *testing.T) {
		code, body := callGET(t, h.Stats, withSID(sid, "/api/stats"))
		requireStatus(t, body, code, 200)
		st := decodeObj(t, body)
		dbs, _ := st["databases"].([]any)
		var pg map[string]any
		for _, e := range dbs {
			if m, _ := e.(map[string]any); m != nil && m["name"] == "postgres" {
				pg = m
			}
		}
		if pg == nil {
			t.Fatalf("postgres missing from stats: %s", body)
		}
		requireKeys(t, body, pg, "name", "backends", "commits", "rollbacks", "disk_reads", "cache_hits", "hit_ratio", "size")
		if n, _ := pg["backends"].(float64); n < 1 {
			t.Fatalf("backends wrong: %s", body)
		}
		if fmt.Sprint(pg["size"]) == "" {
			t.Fatalf("size empty: %s", body)
		}
		tops, _ := st["top_tables"].([]any)
		if len(tops) == 0 {
			t.Fatalf("top_tables empty: %s", body)
		}
		var authors map[string]any
		for _, e := range tops {
			if m, _ := e.(map[string]any); m != nil && m["table"] == "authors" {
				authors = m
			}
		}
		if authors == nil {
			t.Fatalf("seeded authors missing from top_tables: %s", body)
		}
		requireDeep(t, body, "authors.schema", authors["schema"], "public")
		// seeded demo rows survive: 3 live tuples
		requireDeep(t, body, "authors.live", authors["live"], "3")
	})
	t.Run("roles/content", func(t *testing.T) {
		code, body := callGET(t, h.Roles, withSID(sid, "/api/roles"))
		requireStatus(t, body, code, 200)
		pg := findObj(t, body, "name", "postgres", arrObjs(t, body))
		requireKeys(t, body, pg, "name", "superuser", "inherit", "createrole", "createdb", "login", "replication", "connlimit", "member_of")
		requireDeep(t, body, "postgres.superuser", pg["superuser"], true)
		requireDeep(t, body, "postgres.login", pg["login"], true)
		requireDeep(t, body, "postgres.connlimit", pg["connlimit"], -1)
	})
	t.Run("extensions/content", func(t *testing.T) {
		code, body := callGET(t, h.Extensions, withSID(sid, "/api/extensions"))
		requireStatus(t, body, code, 200)
		pl := findObj(t, body, "name", "plpgsql", arrObjs(t, body))
		requireKeys(t, body, pl, "name", "default_version", "installed_version", "comment")
		if fmt.Sprint(pl["installed_version"]) == "" {
			t.Fatalf("plpgsql not installed: %s", body)
		}
	})
	// fail: every read needs a session
	t.Run("no-session", func(t *testing.T) {
		for _, fn := range []func(string) (int, string){
			func(p string) (int, string) { return callGET(t, h.Activity, p) },
			func(p string) (int, string) { return callGET(t, h.Locks, p) },
			func(p string) (int, string) { return callGET(t, h.ServerInfo, p) },
			func(p string) (int, string) { return callGET(t, h.Stats, p) },
			func(p string) (int, string) { return callGET(t, h.Roles, p) },
			func(p string) (int, string) { return callGET(t, h.Extensions, p) },
		} {
			code, body := fn("/api/x")
			requireErrContains(t, body, code, 401, "not connected")
		}
	})
}

func TestMaintenance(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	// happy: analyze returns the exact command tag
	code, body := callPOST(t, h.Maintenance, "/api/maintenance",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"analyze"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "analyze", decodeObj(t, body), map[string]any{"ok": true, "result": "ANALYZE"})
	// fail: unknown op, missing table, bad json, no session, wrong method
	code, body = callPOST(t, h.Maintenance, "/api/maintenance",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"optimize"}`, sid, tbl))
	requireErrContains(t, body, code, 400, "unknown op")
	code, body = callPOST(t, h.Maintenance, "/api/maintenance",
		fmt.Sprintf(`{"session_id":%q,"op":"analyze"}`, sid))
	requireErrContains(t, body, code, 400, "table required")
	code, body = callPOST(t, h.Maintenance, "/api/maintenance", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	code, body = callPOST(t, h.Maintenance, "/api/maintenance", `{"table":"x","op":"analyze"}`)
	requireErrContains(t, body, code, 401, "not connected")
	code, _ = callMethod(t, h.Maintenance, "GET", "/api/maintenance", "")
	requireStatus(t, "", code, 405)
	// edge: refused inside an open txn (VACUUM can't run in a txn block)
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	code, body = callPOST(t, h.Maintenance, "/api/maintenance",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"analyze"}`, sid, tbl))
	requireErrContains(t, body, code, 400, "transaction")
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
}

func TestCancel(t *testing.T) {
	h, sid := newHandler(t)
	// happy: cancelling a bogus pid returns ok:false but 200
	code, body := callGET(t, h.Cancel, withSID(sid, "/api/cancel?pid=0"))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "cancel", decodeObj(t, body), map[string]any{"ok": false})
	// edge: kill=1 variant also 200
	code, body = callGET(t, h.Cancel, withSID(sid, "/api/cancel?pid=0&kill=1"))
	requireStatus(t, body, code, 200)
	// fail: no session
	code, body = callGET(t, h.Cancel, "/api/cancel?pid=0")
	requireErrContains(t, body, code, 401, "not connected")
}
