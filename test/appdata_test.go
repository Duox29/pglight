package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"pglight/internal/api"
)

// jsonNewDecoder decodes with UseNumber so int64 versions keep precision.
func jsonNewDecoder(body string) *json.Decoder {
	dec := json.NewDecoder(bytes.NewBufferString(body))
	dec.UseNumber()
	return dec
}

// --- /api/snippets ---

func TestSnippetsCRUD(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callPOST(t, h.Snippets, "/api/snippets", `{"name":"s1","sql":"SELECT 1"}`)
	requireStatus(t, body, code, 200)
	created := decodeObj(t, body)["snippet"].(map[string]any)
	requireDeep(t, body, "snippet.name", created["name"], "s1")
	requireDeep(t, body, "snippet.sql", created["sql"], "SELECT 1")
	if fmt.Sprint(created["id"]) == "" {
		t.Fatalf("snippet id empty: %s", body)
	}
	code, body = callGET(t, h.Snippets, "/api/snippets")
	requireStatus(t, body, code, 200)
	list, _ := decodeObj(t, body)["snippets"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 snippet, got %s", body)
	}
	requireDeep(t, body, "snippets[0]", list[0], map[string]any{
		"id": created["id"], "name": "s1", "sql": "SELECT 1",
		"created_at": list[0].(map[string]any)["created_at"],
		"updated_at": list[0].(map[string]any)["updated_at"],
	})
	// upsert same name replaces the sql in place
	code, body = callPOST(t, h.Snippets, "/api/snippets", `{"name":"s1","sql":"SELECT 2"}`)
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.Snippets, "/api/snippets")
	list, _ = decodeObj(t, body)["snippets"].([]any)
	if len(list) != 1 {
		t.Fatalf("upsert duplicated: %s", body)
	}
	requireDeep(t, body, "upsert.sql", list[0].(map[string]any)["sql"], "SELECT 2")
	// fail: empty name, empty sql, bad json
	code, body = callPOST(t, h.Snippets, "/api/snippets", `{"name":"","sql":"SELECT 1"}`)
	requireErrContains(t, body, code, 400, "name must be")
	code, body = callPOST(t, h.Snippets, "/api/snippets", `{"name":"s2","sql":""}`)
	requireErrContains(t, body, code, 400, "sql must be")
	code, body = callPOST(t, h.Snippets, "/api/snippets", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	// delete happy empties the list; second delete is 404
	code, body = callMethod(t, h.Snippets, "DELETE", "/api/snippets?name=s1", "")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "delete", decodeObj(t, body), map[string]any{"ok": true})
	code, body = callGET(t, h.Snippets, "/api/snippets")
	requireDeep(t, body, "snippets.empty", decodeObj(t, body)["snippets"], []any{})
	code, body = callMethod(t, h.Snippets, "DELETE", "/api/snippets?name=s1", "")
	requireErrContains(t, body, code, 404, "not found")
	code, body = callMethod(t, h.Snippets, "DELETE", "/api/snippets", "")
	requireErrContains(t, body, code, 400, "name is required")
	code, _ = callMethod(t, h.Snippets, "PUT", "/api/snippets", "")
	requireStatus(t, "", code, 405)
}

// --- /api/history ---

func TestHistoryCRUD(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callPOST(t, h.History, "/api/history", `{"sql":"SELECT 1","ms":5,"n":1}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "add", decodeObj(t, body), map[string]any{"ok": true})
	code, body = callGET(t, h.History, "/api/history")
	requireStatus(t, body, code, 200)
	list, _ := decodeObj(t, body)["history"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 history entry, got %s", body)
	}
	e := list[0].(map[string]any)
	requireDeep(t, body, "history.sql", e["sql"], "SELECT 1")
	requireDeep(t, body, "history.ms", e["ms"], 5)
	requireDeep(t, body, "history.n", e["n"], 1)
	if fmt.Sprint(e["id"]) == "" || fmt.Sprint(e["at"]) == "" {
		t.Fatalf("history entry lacks id/at: %s", body)
	}
	code, body = callPOST(t, h.History, "/api/history", `{"sql":""}`)
	requireErrContains(t, body, code, 400, "sql is required")
	code, body = callMethod(t, h.History, "DELETE", "/api/history", "")
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.History, "/api/history")
	requireDeep(t, body, "history.cleared", decodeObj(t, body)["history"], []any{})
	code, _ = callMethod(t, h.History, "PATCH", "/api/history", "")
	requireStatus(t, "", code, 405)
}

// --- /api/connections ---

func TestConnectionsCRUD(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callPOST(t, h.Connections, "/api/connections",
		`{"Name":"local","Host":"localhost","port":5432,"User":"postgres","DBName":"postgres","SSLMode":"disable"}`)
	requireStatus(t, body, code, 200)
	c := decodeObj(t, body)["connection"].(map[string]any)
	requireDeep(t, body, "connection", c, map[string]any{
		"id": c["id"], "name": "local", "host": "localhost", "port": 5432,
		"user": "postgres", "dbname": "postgres", "sslmode": "disable",
	})
	code, body = callGET(t, h.Connections, "/api/connections")
	requireStatus(t, body, code, 200)
	list, _ := decodeObj(t, body)["connections"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 connection, got %s", body)
	}
	requireDeep(t, body, "connections[0]", list[0], c)
	// fail: missing fields
	code, body = callPOST(t, h.Connections, "/api/connections", `{"Name":"x"}`)
	requireErrContains(t, body, code, 400, "required")
	code, body = callMethod(t, h.Connections, "DELETE", "/api/connections", "")
	requireErrContains(t, body, code, 400, "name is required")
	code, body = callMethod(t, h.Connections, "DELETE", "/api/connections?name=ghost", "")
	requireErrContains(t, body, code, 404, "not found")
	code, body = callMethod(t, h.Connections, "DELETE", "/api/connections?name=local", "")
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.Connections, "/api/connections")
	requireDeep(t, body, "connections.empty", decodeObj(t, body)["connections"], []any{})
}

// --- /api/preferences ---

func TestPreferencesMerge(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callGET(t, h.Preferences, "/api/preferences")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "prefs.initial", decodeObj(t, body)["preferences"], map[string]any{})
	code, body = callPOST(t, h.Preferences, "/api/preferences", `{"theme":"dark"}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "prefs.theme", decodeObj(t, body)["preferences"], map[string]any{"theme": "dark"})
	code, body = callPOST(t, h.Preferences, "/api/preferences", `{"fontSize":14}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "prefs.merged", decodeObj(t, body)["preferences"],
		map[string]any{"theme": "dark", "fontSize": 14})
	code, body = callPOST(t, h.Preferences, "/api/preferences", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	code, _ = callMethod(t, h.Preferences, "DELETE", "/api/preferences", "")
	requireStatus(t, "", code, 405)
}

// --- /api/aliases ---

func TestAliasesCRUD(t *testing.T) {
	h := newStoreHandler(t)
	byTrigger := func(body string) map[string]map[string]any {
		out := decodeObj(t, body)["aliases"].([]any)
		m := map[string]map[string]any{}
		for _, e := range out {
			a := e.(map[string]any)
			m[fmt.Sprint(a["trigger"])] = a
		}
		return m
	}
	code, body := callGET(t, h.Aliases, "/api/aliases")
	requireStatus(t, body, code, 200)
	all := byTrigger(body)
	// builtin content exact (8 shipped triggers)
	if len(all) != 8 {
		t.Fatalf("want 8 builtin aliases, got %d (%s)", len(all), body)
	}
	requireDeep(t, body, "ssf", all["ssf"], map[string]any{
		"trigger": "ssf", "expansion": "SELECT * FROM ",
		"detail": "SELECT * FROM …", "builtin": true,
	})
	code, body = callPOST(t, h.Aliases, "/api/aliases", `{"trigger":"myq","expansion":"SELECT 1"}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "alias", decodeObj(t, body)["alias"], map[string]any{
		"trigger": "myq", "expansion": "SELECT 1", "builtin": false,
	})
	// override builtin trigger: served entry flips to the user expansion
	code, body = callPOST(t, h.Aliases, "/api/aliases", `{"trigger":"ssf","expansion":"SELECT 9"}`)
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.Aliases, "/api/aliases")
	all = byTrigger(body)
	if len(all) != 9 {
		t.Fatalf("want 8 builtins + 1 custom, got %d (%s)", len(all), body)
	}
	requireDeep(t, body, "ssf.override", all["ssf"], map[string]any{
		"trigger": "ssf", "expansion": "SELECT 9", "builtin": false,
	})
	// fail: bad trigger, empty expansion
	code, body = callPOST(t, h.Aliases, "/api/aliases", `{"trigger":"9bad","expansion":"x"}`)
	requireErrContains(t, body, code, 400, "trigger must be")
	code, body = callPOST(t, h.Aliases, "/api/aliases", `{"trigger":"ok1","expansion":""}`)
	requireErrContains(t, body, code, 400, "expansion must be")
	// delete one: override removed, builtin restored; delete all: customs gone
	code, body = callMethod(t, h.Aliases, "DELETE", "/api/aliases?trigger=myq", "")
	requireStatus(t, body, code, 200)
	code, body = callMethod(t, h.Aliases, "DELETE", "/api/aliases?trigger=myq", "")
	requireErrContains(t, body, code, 404, "not found")
	code, body = callMethod(t, h.Aliases, "DELETE", "/api/aliases", "")
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.Aliases, "/api/aliases")
	all = byTrigger(body)
	if len(all) != 8 {
		t.Fatalf("want 8 builtins after clear, got %d (%s)", len(all), body)
	}
	requireDeep(t, body, "ssf.restored", all["ssf"], map[string]any{
		"trigger": "ssf", "expansion": "SELECT * FROM ",
		"detail": "SELECT * FROM …", "builtin": true,
	})
}

// --- /api/complete ---

func TestCompleteSnapshot(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	code, body := callGET(t, h.Complete, withSID(sid, "/api/complete?refresh=1"))
	requireStatus(t, body, code, 200)
	snap := decodeObj(t, body)
	requireDeep(t, body, "in_txn", snap["in_txn"], false)
	if v, _ := snap["version"].(float64); v <= 0 {
		t.Fatalf("version wrong: %s", body)
	}
	// own table present with exact nested columns
	tables, _ := snap["tables"].([]any)
	var mine map[string]any
	for _, e := range tables {
		if m, _ := e.(map[string]any); m != nil && m["name"] == tbl {
			mine = m
		}
	}
	if mine == nil {
		t.Fatalf("own table %s missing from snapshot: %s", tbl, body)
	}
	requireDeep(t, body, "table.schema", mine["schema"], "public")
	requireDeep(t, body, "table.columns", mine["columns"], []any{
		map[string]any{"name": "id", "type": "integer"},
		map[string]any{"name": "v", "type": "text"},
	})
	// keywords include core verbs
	kws, _ := snap["keywords"].([]any)
	for _, k := range []string{"SELECT", "FROM", "WHERE", "JOIN"} {
		found := false
		for _, kw := range kws {
			if kw == k {
				found = true
			}
		}
		if !found {
			t.Fatalf("keyword %s missing: %s", k, body)
		}
	}
	// fail: no session
	code, body = callGET(t, h.Complete, "/api/complete")
	requireErrContains(t, body, code, 401, "not connected")
}

func TestCompleteETag(t *testing.T) {
	h, sid := newHandler(t)
	r1 := newReq("GET", withSID(sid, "/api/complete"))
	w1 := newRec()
	h.Complete(w1, r1)
	if w1.Code != 200 {
		t.Fatalf("complete: %d %s", w1.Code, w1.Body.String())
	}
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("complete missing ETag")
	}
	// ETag quotes the snapshot version (int64 UnixNano — decode with
	// UseNumber so the assertion itself loses no precision)
	if len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Fatalf("ETag must be a quoted int, got %s", etag)
	}
	var ev int64
	if _, err := fmt.Sscanf(etag, `"%d"`, &ev); err != nil || ev <= 0 {
		t.Fatalf("ETag must quote a positive version, got %s", etag)
	}
	dec := jsonNewDecoder(w1.Body.String())
	var v1 map[string]any
	if err := dec.Decode(&v1); err != nil {
		t.Fatal(err)
	}
	if n, _ := v1["version"].(json.Number); string(n) != fmt.Sprint(ev) {
		t.Fatalf("ETag %s must quote version %s", etag, n)
	}
	r2 := newReq("GET", withSID(sid, "/api/complete"))
	r2.Header.Set("If-None-Match", etag)
	w2 := newRec()
	h.Complete(w2, r2)
	if w2.Code != 304 {
		t.Fatalf("want 304, got %d", w2.Code)
	}
	// refresh=1 bypasses the 304
	r3 := newReq("GET", withSID(sid, "/api/complete?refresh=1"))
	r3.Header.Set("If-None-Match", etag)
	w3 := newRec()
	h.Complete(w3, r3)
	if w3.Code != 200 {
		t.Fatalf("refresh must bypass 304, got %d", w3.Code)
	}
}

// --- /api/settings + /api/logs ---

func TestSettingsLogs(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callGET(t, h.Settings, "/api/settings")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "settings.keys", mapKeys(decodeObj(t, body)), []string{"logging"})
	code, body = callPOST(t, h.Settings, "/api/settings", `{"logging":{"enabled":true,"level":"debug","log_http":true,"log_query":true,"slow_ms":100,"max_entries":50}}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "settings.saved", decodeObj(t, body)["logging"], map[string]any{
		"enabled": true, "level": "debug", "log_http": true, "log_query": true,
		"slow_ms": 100, "max_entries": 50,
	})
	// GET reflects the saved config exactly
	code, body = callGET(t, h.Settings, "/api/settings")
	requireDeep(t, body, "settings.roundtrip", decodeObj(t, body)["logging"], map[string]any{
		"enabled": true, "level": "debug", "log_http": true, "log_query": true,
		"slow_ms": 100, "max_entries": 50,
	})
	code, body = callPOST(t, h.Settings, "/api/settings", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	// nil-log edge => 500 (unit test is standard: handler must not panic)
	bare := &api.Handler{Mgr: h.Mgr, Store: h.Store, UserID: h.UserID}
	code, body = callGET(t, bare.Settings, "/api/settings")
	requireErrContains(t, body, code, 500, "not initialized")
	code, body = callGET(t, bare.Logs, "/api/logs")
	requireErrContains(t, body, code, 500, "not initialized")

	// logs: fast queries are debug-gated (default info drops them), so flip
	// to debug first — exactly like the web Settings panel does — then a
	// real query lands in the ring buffer with exact content
	h2, sid := newHandler(t)
	code, body = callPOST(t, h2.Settings, "/api/settings", `{"logging":{"enabled":true,"level":"debug","log_http":true,"log_query":true,"slow_ms":500,"max_entries":500}}`)
	requireStatus(t, body, code, 200)
	_, _ = callPOST(t, h2.Query, "/api/query", qBody(sid, "SELECT 42 AS answer", 0))
	code, body = callGET(t, h2.Logs, "/api/logs?limit=50")
	requireStatus(t, body, code, 200)
	entries, _ := decodeObj(t, body)["entries"].([]any)
	var hit map[string]any
	for _, e := range entries {
		if m, _ := e.(map[string]any); m != nil && fmt.Sprint(m["message"]) == "SELECT 42 AS answer" {
			hit = m
		}
	}
	if hit == nil {
		t.Fatalf("query not logged: %s", body)
	}
	requireDeep(t, body, "log.level", hit["level"], "debug")
	requireDeep(t, body, "log.category", hit["category"], "query")
	requireDeep(t, body, "log.message", hit["message"], "SELECT 42 AS answer")
	code, body = callMethod(t, h2.Logs, "DELETE", "/api/logs", "")
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h2.Logs, "/api/logs?limit=50")
	requireDeep(t, body, "logs.cleared", decodeObj(t, body)["entries"], []any{})
	code, _ = callMethod(t, h.Settings, "DELETE", "/api/settings", "")
	requireStatus(t, "", code, 405)
}
