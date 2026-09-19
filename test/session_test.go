package test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- /api/connect ---

func TestConnectHappy(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callPOST(t, h.Connect, "/api/connect",
		`{"host":"localhost","port":5432,"user":"postgres","password":"postgres","dbname":"postgres","sslmode":"disable"}`)
	if code != 200 {
		t.Skipf("test postgres unreachable: %d %s", code, body)
	}
	out := decodeObj(t, body)
	sid, _ := out["session_id"].(string)
	if sid == "" {
		t.Fatalf("missing session_id in %s", body)
	}
	info, _ := out["info"].(map[string]any)
	// exact connection identity echoed back
	requireDeep(t, body, "info.host", info["host"], "localhost")
	requireDeep(t, body, "info.port", info["port"], 5432)
	requireDeep(t, body, "info.user", info["user"], "postgres")
	requireDeep(t, body, "info.dbname", info["dbname"], "postgres")
	requireDeep(t, body, "info.sslmode", info["sslmode"], "disable")
	requireDeep(t, body, "info.id", info["id"], sid)
	requireDeep(t, body, "info.in_txn", info["in_txn"], false)
	// loopback + disable => not an insecure-TLS setup
	requireDeep(t, body, "info.tls_warn", info["tls_warn"], false)
	if ts, _ := info["connected_at"].(string); ts == "" {
		t.Fatalf("missing connected_at in %v", info)
	}
	h.Mgr.Close(sid)
}

func TestConnectFail(t *testing.T) {
	h := newStoreHandler(t)
	// invalid json
	code, body := callPOST(t, h.Connect, "/api/connect", `{oops`)
	requireErrContains(t, body, code, 400, "invalid json")
	// unreachable host must not create a session
	code, body = callPOST(t, h.Connect, "/api/connect",
		`{"host":"192.0.2.1","port":5432,"user":"u","password":"p","dbname":"d","sslmode":"disable","session_id":"nope-unreachable"}`)
	requireStatus(t, body, code, 400)
	if h.Mgr.Alive("nope-unreachable") {
		t.Fatal("failed connect left a live session")
	}
	// wrong method
	code, _ = callMethod(t, h.Connect, "GET", "/api/connect", "")
	requireStatus(t, "", code, 405)
}

func TestConnectEdge(t *testing.T) {
	h := newStoreHandler(t)
	// resume path: same session id twice reuses the pool, no leak
	const sid = "resume-edge"
	code, body := callPOST(t, h.Connect, "/api/connect",
		fmt.Sprintf(`{"host":"localhost","port":5432,"user":"postgres","password":"postgres","dbname":"postgres","sslmode":"disable","session_id":%q}`, sid))
	if code != 200 {
		t.Skipf("test postgres unreachable: %d %s", code, body)
	}
	code, body2 := callPOST(t, h.Connect, "/api/connect",
		fmt.Sprintf(`{"host":"localhost","port":5432,"user":"postgres","password":"postgres","dbname":"postgres","sslmode":"disable","session_id":%q}`, sid))
	requireStatus(t, body2, code, 200)
	if decodeObj(t, body2)["session_id"] != sid {
		t.Fatalf("resume changed sid: %s", body2)
	}
	// empty session -> server generates one
	code, body = callPOST(t, h.Connect, "/api/connect",
		`{"host":"localhost","port":5432,"user":"postgres","password":"postgres","dbname":"postgres","sslmode":"disable"}`)
	requireStatus(t, body, code, 200)
	if decodeObj(t, body)["session_id"] == "" {
		t.Fatalf("empty session not generated: %s", body)
	}
	h.Mgr.Close(sid)
}

// --- /api/sessions + /api/disconnect ---

func TestSessionsHappy(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callGET(t, h.Sessions, "/api/sessions")
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	list, _ := out["sessions"].([]any)
	if len(list) == 0 {
		t.Fatalf("empty sessions list: %s", body)
	}
	var mine map[string]any
	for _, s := range list {
		if m, _ := s.(map[string]any); m != nil && m["id"] == sid {
			mine = m
		}
	}
	if mine == nil {
		t.Fatalf("own session %s missing in %s", sid, body)
	}
	// display info only: identity fields exact, no password anywhere
	requireDeep(t, body, "session.user", mine["user"], "postgres")
	requireDeep(t, body, "session.dbname", mine["dbname"], "postgres")
	requireDeep(t, body, "session.host", mine["host"], "localhost")
	requireDeep(t, body, "session.in_txn", mine["in_txn"], false)
	if strings.Contains(body, "postgres\"") && strings.Contains(body, "password") {
		t.Fatalf("sessions leak credentials: %s", body)
	}
}

func TestDisconnectHappyAndEdge(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callGET(t, h.Disconnect, withSID(sid, "/api/disconnect"))
	requireStatus(t, body, code, 200)
	if h.Mgr.Alive(sid) {
		t.Fatal("session still alive after disconnect")
	}
	// edge: disconnecting an unknown session is still ok:true (idempotent)
	code, body = callGET(t, h.Disconnect, "/api/disconnect?session_id=ghost-xyz")
	requireStatus(t, body, code, 200)
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("expected ok:true, got %s", body)
	}
}

// --- /api/txn ---

func TestTxnHappyCycle(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	// begin -> status -> commit with exact in_txn transitions + ok:true
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "begin", decodeObj(t, body), map[string]any{"ok": true, "in_txn": true})
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"status"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "status-in-txn", decodeObj(t, body), map[string]any{"ok": true, "in_txn": true})
	// uncommitted write is visible inside the session (txn-aware Querier)
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1)`, tbl))
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, fmt.Sprintf(`SELECT id FROM %s`, tbl), 0))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1}})
	requireDeep(t, body, "query.in_txn", decodeObj(t, body)["in_txn"], true)
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"commit"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "commit", decodeObj(t, body), map[string]any{"ok": true, "in_txn": false})
	if h.Mgr.InTxn(sid) {
		t.Fatal("commit did not close txn")
	}
	// committed row survives
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, fmt.Sprintf(`SELECT id FROM %s ORDER BY 1`, tbl), 0))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1}})

	// rollback path discards the write
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (2)`, tbl))
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "rollback", decodeObj(t, body), map[string]any{"ok": true, "in_txn": false})
	if h.Mgr.InTxn(sid) {
		t.Fatal("rollback did not close txn")
	}
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, fmt.Sprintf(`SELECT id FROM %s ORDER BY 1`, tbl), 0))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1}})
}

func TestTxnFail(t *testing.T) {
	h, sid := newHandler(t)
	// unknown action
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"explode"`))
	requireErrContains(t, body, code, 400, "unknown action")
	// no session
	code, body = callPOST(t, h.Txn, "/api/txn", `{"action":"begin"}`)
	requireErrContains(t, body, code, 401, "not connected")
	// invalid json
	code, body = callPOST(t, h.Txn, "/api/txn", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	// double begin
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireErrContains(t, body, code, 400, "transaction already open")
	// cleanup
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	// commit with no txn
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"commit"`))
	requireErrContains(t, body, code, 400, "no open transaction")
	// wrong method
	code, _ = callMethod(t, h.Txn, "GET", "/api/txn", "")
	requireStatus(t, "", code, 405)
}

func TestTxnEdge(t *testing.T) {
	h, sid := newHandler(t)
	// action is case-insensitive and trimmed
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"  BEGIN "}`))
	requireStatus(t, body, code, 200)
	if h.Mgr.InTxn(sid) != true {
		t.Fatalf("uppercase/padded BEGIN ignored: %s", body)
	}
	// status is a no-op report
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"status"`))
	requireStatus(t, body, code, 200)
	if decodeObj(t, body)["in_txn"] != true {
		t.Fatalf("status must report in_txn:true: %s", body)
	}
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	// session via query param instead of body also works
	r := httptest.NewRequest("POST", withSID(sid, "/api/txn"), strings.NewReader(`{"action":"status"}`))
	w := httptest.NewRecorder()
	h.Txn(w, r)
	requireStatus(t, w.Body.String(), w.Code, 200)
}
