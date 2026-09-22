// Integration tests: end-to-end user journeys over real HTTP.
//
// Unit-style tests in this package call one Handler method directly via
// httptest request/recorder. These tests instead boot the full route table
// (mirroring main.go, behind logging.Middleware) with httptest.NewServer and
// drive multi-endpoint workflows with a real HTTP client: connect → query →
// txn → table-data/row/import → explorer/erd/search → disconnect.
//
// That covers what per-endpoint tests cannot: routing, JSON wire shapes,
// session propagation (query param vs body vs header), txn-awareness ACROSS
// endpoints (backend rule §2.2), and the error contract through middleware.
// Only exported symbols are used. PG-dependent journeys skip when the docker
// test DB is unreachable, exactly like the handler-level tests.
package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pglight/internal/api"
	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"
)

// integrationMux mirrors the route table in main.go so requests flow through
// the same paths (and logging.Middleware) as production.
func integrationMux(h *api.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/connect", h.Connect)
	mux.HandleFunc("/api/sessions", h.Sessions)
	mux.HandleFunc("/api/disconnect", h.Disconnect)
	mux.HandleFunc("/api/databases", h.Databases)
	mux.HandleFunc("/api/schemas", h.Schemas)
	mux.HandleFunc("/api/tables", h.Tables)
	mux.HandleFunc("/api/objects", h.Objects)
	mux.HandleFunc("/api/columns", h.Columns)
	mux.HandleFunc("/api/ddl", h.DDL)
	mux.HandleFunc("/api/table-data", h.TableData)
	mux.HandleFunc("/api/query", h.Query)
	mux.HandleFunc("/api/explain", h.Explain)
	mux.HandleFunc("/api/activity", h.Activity)
	mux.HandleFunc("/api/cancel", h.Cancel)
	mux.HandleFunc("/api/row", h.RowOp)
	mux.HandleFunc("/api/rows-delete", h.BatchDelete)
	mux.HandleFunc("/api/complete", h.Complete)
	mux.HandleFunc("/api/aliases", h.Aliases)
	mux.HandleFunc("/api/snippets", h.Snippets)
	mux.HandleFunc("/api/connections", h.Connections)
	mux.HandleFunc("/api/preferences", h.Preferences)
	mux.HandleFunc("/api/preferences/shortcuts", h.ShortcutPreferences)
	mux.HandleFunc("/api/history", h.History)
	mux.HandleFunc("/api/txn", h.Txn)
	mux.HandleFunc("/api/server-info", h.ServerInfo)
	mux.HandleFunc("/api/stats", h.Stats)
	mux.HandleFunc("/api/locks", h.Locks)
	mux.HandleFunc("/api/roles", h.Roles)
	mux.HandleFunc("/api/extensions", h.Extensions)
	mux.HandleFunc("/api/types", h.Types)
	mux.HandleFunc("/api/triggers", h.Triggers)
	mux.HandleFunc("/api/constraints", h.Constraints)
	mux.HandleFunc("/api/view-def", h.ViewDef)
	mux.HandleFunc("/api/func-def", h.FuncDef)
	mux.HandleFunc("/api/seq-def", h.SeqDef)
	mux.HandleFunc("/api/type-def", h.TypeDef)
	mux.HandleFunc("/api/table-stats", h.TableStats)
	mux.HandleFunc("/api/erd", h.ERD)
	mux.HandleFunc("/api/search", h.Search)
	mux.HandleFunc("/api/maintenance", h.Maintenance)
	mux.HandleFunc("/api/import", h.Import)
	mux.HandleFunc("/api/mock-data/meta", h.MockMeta)
	mux.HandleFunc("/api/mock-data/preview", h.MockPreview)
	mux.HandleFunc("/api/mock-data/generate", h.MockGenerate)
	mux.HandleFunc("/api/alter-table", h.AlterTable)
	mux.HandleFunc("/api/settings", h.Settings)
	mux.HandleFunc("/api/logs", h.Logs)
	// NOTE: /api/shutdown is deliberately unrouted here — it calls os.Exit
	// after replying, which would kill the test runner. Its building block
	// (Manager.CloseAll) is covered by TestDBCloseAll; the 405 method guard
	// is covered per-endpoint style below via direct handler call.
	return logging.Middleware(h.Log, mux)
}

var integrationClient = &http.Client{Timeout: 30 * time.Second}

// newIntegrationServer boots the full stack and connects over real HTTP.
// It returns the server base URL and a live session id. Skips when PG is down.
func newIntegrationServer(t *testing.T) (string, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	const user = "test-user"
	if err := s.EnsureUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	h := &api.Handler{Mgr: db.New(), Log: logging.New(""), Store: s, UserID: user}
	srv := httptest.NewServer(integrationMux(h))
	t.Cleanup(srv.Close)

	code, body := ipost(t, srv.URL+"/api/connect",
		`{"host":"localhost","port":5432,"user":"postgres","password":"postgres","dbname":"postgres","sslmode":"disable"}`)
	if code != 200 {
		t.Skipf("test postgres unreachable: %d %s", code, body)
	}
	sid, _ := decodeObj(t, body)["session_id"].(string)
	if sid == "" {
		t.Fatalf("connect returned no session_id: %s", body)
	}
	t.Cleanup(func() { h.Mgr.Close(sid) })
	return srv.URL, sid
}

func iget(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := integrationClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

func ipost(t *testing.T, url, payload string) (int, string) {
	t.Helper()
	resp, err := integrationClient.Post(url, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// iquery runs one statement through POST /api/query over HTTP.
func iquery(t *testing.T, base, sid, sql string) (int, string) {
	t.Helper()
	return ipost(t, base+"/api/query", fmt.Sprintf(`{"session_id":%q,"sql":%q}`, sid, sql))
}

// --- session lifecycle over the wire ---

func TestIntegrationSessionLifecycle(t *testing.T) {
	base, sid := newIntegrationServer(t)

	// sessions lists the freshly connected session (display info, no password)
	code, body := iget(t, base+"/api/sessions")
	requireStatus(t, body, code, 200)
	mine := findSession(t, body, sid)
	requireDeep(t, body, "session.user", mine["user"], "postgres")
	requireDeep(t, body, "session.dbname", mine["dbname"], "postgres")
	requireDeep(t, body, "session.in_txn", mine["in_txn"], false)
	if strings.Contains(body, "password") {
		t.Fatalf("sessions leak credentials: %s", body)
	}

	// a simple query works over HTTP with the exact single-statement shape
	code, body = iquery(t, base, sid, "SELECT 1 AS one")
	requireStatus(t, body, code, 200)
	requireColumns(t, body, []string{"one"})
	requireRows(t, body, [][]any{{1}})
	requireDeep(t, body, "in_txn", decodeObj(t, body)["in_txn"], false)

	// server-info reports this database
	code, body = iget(t, base+"/api/server-info?session_id="+sid)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "database", decodeObj(t, body)["database"], "postgres")
	if v, _ := decodeObj(t, body)["version"].(string); !strings.Contains(v, "PostgreSQL") {
		t.Fatalf("server-info version not PostgreSQL: %s", body)
	}

	// disconnect then reuse: the session is gone (401 + {error} contract)
	code, body = ipost(t, base+"/api/disconnect?session_id="+sid, "")
	requireStatus(t, body, code, 200)
	code, body = iquery(t, base, sid, "SELECT 1")
	requireErrContains(t, body, code, 401, "not connected")
}

// findSession extracts one entry by id from a {"sessions":[...]} body.
func findSession(t *testing.T, body, sid string) map[string]any {
	t.Helper()
	out := decodeObj(t, body)
	list, _ := out["sessions"].([]any)
	if list == nil {
		t.Fatalf("missing sessions array in %s", body)
	}
	entries := make([]map[string]any, 0, len(list))
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("session entry not an object: %v (%s)", e, body)
		}
		entries = append(entries, m)
	}
	return findObj(t, body, "id", sid, entries)
}

// --- txn visibility across endpoints (backend rule §2.2) ---
//
// The defining integration property: an explicit txn opened via /api/txn
// must be honored by every other data path (/api/query, /api/table-data,
// /api/row, /api/import). Unit tests cover each endpoint alone; only a
// cross-endpoint journey proves the session Querier is shared.
func TestIntegrationTxnVisibilityAcrossEndpoints(t *testing.T) {
	base, sid := newIntegrationServer(t)
	tbl := tempTable(t)

	code, body := iquery(t, base, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	requireStatus(t, body, code, 200)
	defer func() {
		_, _ = ipost(t, base+"/api/txn", postBody(sid, `"action":"rollback"`))
		_, _ = iquery(t, base, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl))
	}()
	tableData := func() (int, string) {
		return iget(t, base+"/api/table-data?session_id="+sid+"&schema=public&table="+tbl+"&order=id")
	}

	// begin → uncommitted insert is visible through a DIFFERENT endpoint
	code, body = ipost(t, base+"/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	code, body = iquery(t, base, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1)`, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "insert.in_txn", decodeObj(t, body)["in_txn"], true)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1}})
	requireDeep(t, body, "table-data.in_txn", decodeObj(t, body)["in_txn"], true)

	// rollback → the row is gone everywhere
	code, body = ipost(t, base+"/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "rows-after-rollback", rowsOf(t, body), []any{})
	requireDeep(t, body, "in_txn-after-rollback", decodeObj(t, body)["in_txn"], false)

	// commit → the row survives and is visible without a txn
	code, body = ipost(t, base+"/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	code, body = iquery(t, base, sid, fmt.Sprintf(`INSERT INTO %s VALUES (2)`, tbl))
	requireStatus(t, body, code, 200)
	code, body = ipost(t, base+"/api/txn", postBody(sid, `"action":"commit"`))
	requireStatus(t, body, code, 200)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{2}})
}

// --- write flow: import → browse → cell edit → bulk delete ---

func TestIntegrationImportEditDeleteFlow(t *testing.T) {
	base, sid := newIntegrationServer(t)
	tbl := tempTable(t)

	code, body := iquery(t, base, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, name text)`, tbl))
	requireStatus(t, body, code, 200)
	defer func() { _, _ = iquery(t, base, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl)) }()
	tableData := func() (int, string) {
		return iget(t, base+"/api/table-data?session_id="+sid+"&schema=public&table="+tbl+"&order=id&limit=100")
	}

	// bulk import two rows
	code, body = ipost(t, base+"/api/import", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"columns":["id","name"],"rows":[[1,"a"],[2,"b"]]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "imported", decodeObj(t, body)["rows_affected"], 2)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1, "a"}, {2, "b"}})

	// single-cell edit via /api/row (exactly-one-row guard)
	code, body = ipost(t, base+"/api/row", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"op":"update","values":{"name":"b2"},"where":{"id":2},"single":true}`,
		sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "updated", decodeObj(t, body)["rows_affected"], 1)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1, "a"}, {2, "b2"}})

	// atomic bulk delete of one row; the other survives
	code, body = ipost(t, base+"/api/rows-delete", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"where":[{"id":1}]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "deleted", decodeObj(t, body)["deleted"], 1)
	code, body = tableData()
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{2, "b2"}})
}

// --- explorer chain agrees on the same object ---
//
// schemas → tables → columns → ddl → search → erd must all describe the
// table created through /api/query. This is the catalog-consistency journey
// a user performs when opening a new table in the UI.
func TestIntegrationExplorerChainAgrees(t *testing.T) {
	base, sid := newIntegrationServer(t)
	tbl := tempTable(t)

	code, body := iquery(t, base, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	requireStatus(t, body, code, 200)
	defer func() { _, _ = iquery(t, base, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl)) }()
	q := "session_id=" + sid

	code, body = iget(t, base+"/api/schemas?"+q)
	requireStatus(t, body, code, 200)
	var schemas []string
	if err := jsonUnmarshal(body, &schemas); err != nil {
		t.Fatalf("schemas not a string array: %s", body)
	}
	found := false
	for _, s := range schemas {
		if s == "public" {
			found = true
		}
	}
	if !found {
		t.Fatalf("schemas lack public: %s", body)
	}

	code, body = iget(t, base+"/api/tables?"+q+"&schema=public")
	requireStatus(t, body, code, 200)
	mine := findObj(t, body, "name", tbl, arrObjs(t, body))
	requireDeep(t, body, "table.schema", mine["schema"], "public")
	requireDeep(t, body, "table.type", mine["type"], "BASE TABLE")

	code, body = iget(t, base+"/api/columns?"+q+"&schema=public&table="+tbl)
	requireStatus(t, body, code, 200)
	cols := arrObjs(t, body)
	if len(cols) != 2 {
		t.Fatalf("want 2 columns, got %s", body)
	}
	requireDeep(t, body, "col.id.pk", findObj(t, body, "name", "id", cols)["pk"], true)

	code, body = iget(t, base+"/api/ddl?"+q+"&schema=public&table="+tbl)
	requireStatus(t, body, code, 200)
	if ddl, _ := decodeObj(t, body)["ddl"].(string); !strings.Contains(ddl, tbl) {
		t.Fatalf("ddl missing table name: %s", body)
	}

	code, body = iget(t, base+"/api/search?"+q+"&q="+tbl)
	requireStatus(t, body, code, 200)
	found = false
	for _, e := range arrObjs(t, body) {
		if strings.Contains(fmt.Sprint(e["name"]), tbl) {
			found = true
		}
	}
	if !found {
		t.Fatalf("search missed new table: %s", body)
	}

	code, body = iget(t, base+"/api/erd?"+q+"&schema=public")
	requireStatus(t, body, code, 200)
	if !strings.Contains(body, tbl) {
		t.Fatalf("erd missing new table: %s", body)
	}
}

// --- error contract through real transport ---
//
// Every failure over HTTP must be {error: string} with the documented status;
// query-like failures additionally carry in_txn. Method violations stay 405
// (plain-text from net/http is fine — only JSON errors carry the contract).
func TestIntegrationErrorContract(t *testing.T) {
	base, sid := newIntegrationServer(t)

	// bad SQL → 400 with code + line/column
	code, body := iquery(t, base, sid, "SELEC 1")
	out := requireErrContains(t, body, code, 400, "syntax")
	if out["code"] == nil || out["line"] == nil || out["column"] == nil {
		t.Fatalf("error location missing code/line/column: %s", body)
	}
	requireDeep(t, body, "in_txn", out["in_txn"], false)

	// missing table param → 400 {error}
	code, body = iget(t, base+"/api/table-data?session_id="+sid)
	requireErrContains(t, body, code, 400, "table required")

	// ghost session → 401 {error} on both GET and POST paths
	code, body = iget(t, base+"/api/tables?session_id=ghost-xyz")
	requireErrContains(t, body, code, 401, "not connected")
	code, body = iquery(t, base, "ghost-xyz", "SELECT 1")
	requireErrContains(t, body, code, 401, "not connected")

	// invalid JSON → 400 {error}
	code, body = ipost(t, base+"/api/query", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")

	// wrong method → 405
	code, _ = iget(t, base+"/api/query?session_id="+sid)
	requireStatus(t, "", code, 405)
	code, _ = iget(t, base+"/api/mock-data/preview?session_id="+sid)
	requireStatus(t, "", code, 405)
	code, _ = iget(t, base+"/api/mock-data/generate?session_id="+sid)
	requireStatus(t, "", code, 405)
}

// --- mock-data generation journey over the wire ---
//
// meta → preview → generate → table-data must agree through real HTTP:
// routing, session propagation, the {columns,rows,warnings,seed} preview
// shape, atomic insertion and the {error} contract on every failure.
func TestIntegrationMockDataJourney(t *testing.T) {
	base, sid := newIntegrationServer(t)
	tbl := tempTable(t)

	code, body := iquery(t, base, sid, fmt.Sprintf(
		`CREATE TABLE %s (id SERIAL PRIMARY KEY, name TEXT NOT NULL, age INT CHECK (age BETWEEN 18 AND 99))`, tbl))
	requireStatus(t, body, code, 200)
	defer func() { _, _ = iquery(t, base, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl)) }()

	// meta describes columns, checks and the (empty) FK list.
	code, body = iget(t, base+"/api/mock-data/meta?session_id="+sid+"&schema=public&table="+tbl)
	requireStatus(t, body, code, 200)
	meta := decodeObj(t, body)
	requireKeys(t, "meta", meta, "columns", "foreign_keys", "checks")
	names := map[string]bool{}
	for _, c := range meta["columns"].([]any) {
		names[c.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"id", "name", "age"} {
		if !names[want] {
			t.Fatalf("meta missing column %s: %s", want, body)
		}
	}
	if kinds := checkKinds(t, body, meta); !kinds["between"] {
		t.Fatalf("meta missing between CHECK kind: %s", body)
	}

	// preview: shape, row count, no database writes.
	code, body = ipost(t, base+"/api/mock-data/preview", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"mode":"advanced","count":5,"seed":8,`+
			`"fields":[{"column":"age","generator":"integer"}]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	prev := decodeObj(t, body)
	requireKeys(t, "preview", prev, "columns", "rows", "warnings", "seed", "in_txn")
	requireDeep(t, body, "preview.seed", prev["seed"], 8)
	if len(prev["rows"].([]any)) != 5 {
		t.Fatalf("want 5 preview rows: %s", body)
	}
	code, body = iquery(t, base, sid, fmt.Sprintf(`SELECT count(*) FROM %s`, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{"0"}}) // int8 counts cross JSON as strings

	// generate: atomic insert visible through table-data, ages inferred.
	code, body = ipost(t, base+"/api/mock-data/generate", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"mode":"advanced","count":10,"seed":8,`+
			`"fields":[{"column":"age","generator":"integer"}]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	gen := decodeObj(t, body)
	requireKeys(t, "generate", gen, "generated", "inserted", "seed", "duration_ms", "in_txn")
	requireDeep(t, body, "gen.counts", []any{gen["generated"], gen["inserted"]}, []any{10, 10})
	code, body = iget(t, base+"/api/table-data?session_id="+sid+"&schema=public&table="+tbl+"&order=id&limit=100")
	requireStatus(t, body, code, 200)
	if len(rowsOf(t, body)) != 10 {
		t.Fatalf("want 10 generated rows via table-data: %s", body)
	}
	code, body = iquery(t, base, sid, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE age NOT BETWEEN 18 AND 99`, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{"0"}})

	// error contract over the wire: ghost session, bad mode, bad column.
	code, body = iget(t, base+"/api/mock-data/meta?session_id=ghost-xyz&schema=public&table="+tbl)
	requireErrContains(t, body, code, 401, "not connected")
	code, body = ipost(t, base+"/api/mock-data/generate", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"mode":"weird","count":5}`, sid, tbl))
	requireErrContains(t, body, code, 400, "unknown mode")
	code, body = ipost(t, base+"/api/mock-data/generate", fmt.Sprintf(
		`{"session_id":%q,"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"ghost","generator":"integer"}]}`, sid, tbl))
	requireErrContains(t, body, code, 400, "unknown column")
}

func checkKinds(t *testing.T, body string, meta map[string]any) map[string]bool {
	t.Helper()
	kinds := map[string]bool{}
	chks, ok := meta["checks"].([]any)
	if !ok {
		t.Fatalf("meta missing checks array: %s", body)
	}
	for _, c := range chks {
		kinds[c.(map[string]any)["kind"].(string)] = true
	}
	return kinds
}
