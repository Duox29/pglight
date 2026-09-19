// Package test is the canonical home for all pglight Go tests.
//
// Historically tests lived next to the code (internal/*/*_test.go). They are
// consolidated here as black-box tests: only exported symbols
// (api.Handler methods, db.Manager, logging, store) are exercised, always
// through the real HTTP-handler surface with httptest. Pure white-box checks
// of unexported helpers (splitStatements, errLocation, numVal, coerceVal,
// buildDelete) are ported to equivalent endpoint behavior — e.g. multi-
// statement scripts via /api/query, numeric fidelity via /api/row — so a
// failing test means the product contract broke, not an implementation
// detail drifted.
//
// Convention: every endpoint gets happy + fail + edge cases. Tests that need
// PostgreSQL skip when the docker test DB is unreachable; pure unit tests
// (db escaping, store sqlite, logging wrap) always run.
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pglight/internal/api"
	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"
)

var sidSeq atomic.Int64

// testConnStr points at the docker test DB (see docker/docker-compose.yml).
func testConnStr() string {
	return db.ConnString("localhost", 5432, "postgres", "postgres", "postgres", "disable")
}

// newHandler builds a Handler backed by a fresh Manager + temp sqlite Store.
// It skips the test when PostgreSQL is unreachable.
func newHandler(t *testing.T) (*api.Handler, string) {
	t.Helper()
	mgr := db.New()
	sid := fmt.Sprintf("t%d", sidSeq.Add(1))
	if err := mgr.Add(sid, testConnStr()); err != nil {
		t.Skipf("test postgres unreachable: %v", err)
	}
	// Mirror /api/connect: production sessions always carry display meta.
	mgr.SetMeta(sid, db.ConnMeta{Host: "localhost", Port: 5432, User: "postgres", DbName: "postgres", SSLMode: "disable"})
	t.Cleanup(func() { mgr.Close(sid) })
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	const user = "test-user"
	if err := s.EnsureUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return &api.Handler{Mgr: mgr, Log: logging.New(""), Store: s, UserID: user}, sid
}

// newStoreHandler builds a Handler without PostgreSQL for store-only domains
// (aliases, snippets, history, connections, preferences, settings, logs).
func newStoreHandler(t *testing.T) *api.Handler {
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
	return &api.Handler{Mgr: db.New(), Log: logging.New(""), Store: s, UserID: user}
}

func execSQL(t *testing.T, h *api.Handler, sid, sql string, args ...any) {
	t.Helper()
	qq, ok := h.Mgr.Q(sid)
	if !ok {
		t.Fatal("no session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := qq.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func queryRows(t *testing.T, h *api.Handler, sid, sql string, args ...any) [][]any {
	t.Helper()
	qq, ok := h.Mgr.Q(sid)
	if !ok {
		t.Fatal("no session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := qq.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// tempTable returns a unique public table name for isolation between tests.
func tempTable(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("pglight_t_%d_%d", time.Now().UnixNano(), sidSeq.Add(1))
}

type callFunc func(w http.ResponseWriter, r *http.Request)

// callGET invokes a GET handler and returns status + raw body.
func callGET(t *testing.T, fn callFunc, path string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	fn(w, r)
	return w.Code, w.Body.String()
}

// callPOST invokes a POST handler with a JSON body.
func callPOST(t *testing.T, fn callFunc, path, body string) (int, string) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(http.MethodPost, path, reader)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fn(w, r)
	return w.Code, w.Body.String()
}

// callMethod invokes a handler with an arbitrary method (for 405 checks).
func callMethod(t *testing.T, fn callFunc, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	fn(w, r)
	return w.Code, w.Body.String()
}

func decodeObj(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad json object %q: %v", body, err)
	}
	return out
}

func decodeArr(t *testing.T, body string) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad json array %q: %v", body, err)
	}
	return out
}

func requireStatus(t *testing.T, body string, code, want int) {
	t.Helper()
	if code != want {
		t.Fatalf("status=%d want %d body=%s", code, want, body)
	}
}

func requireErrContains(t *testing.T, body string, code, wantCode int, frags ...string) map[string]any {
	t.Helper()
	requireStatus(t, body, code, wantCode)
	out := decodeObj(t, body)
	msg, _ := out["error"].(string)
	if msg == "" {
		t.Fatalf("missing error field in %s", body)
	}
	for _, f := range frags {
		if !strings.Contains(msg, f) {
			t.Fatalf("error %q lacks %q (body=%s)", msg, f, body)
		}
	}
	return out
}

func withSID(sid, path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "session_id=" + sid
}

func newReq(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func postBody(sid, inner string) string {
	if inner == "" {
		return fmt.Sprintf(`{"session_id":%q}`, sid)
	}
	inner = strings.TrimSpace(inner)
	inner = strings.TrimSuffix(strings.TrimPrefix(inner, "{"), "}")
	if inner == "" {
		return fmt.Sprintf(`{"session_id":%q}`, sid)
	}
	return fmt.Sprintf(`{"session_id":%q,%s}`, sid, inner)
}

// --- content assertions (exact values, not just key presence) ---

// normJSON round-trips a value through JSON so Go ints and JSON float64s
// compare equal. All HTTP bodies decode numbers as float64; expected values
// written as int literals must match them.
func normJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func requireDeep(t *testing.T, body, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(normJSON(got), normJSON(want)) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Fatalf("%s mismatch:\n got=%s\nwant=%s\nbody=%s", what, gj, wj, body)
	}
}

// rowsOf extracts the "rows" array (array of arrays) from a query-like body.
func rowsOf(t *testing.T, body string) []any {
	t.Helper()
	out := decodeObj(t, body)
	rows, ok := out["rows"].([]any)
	if !ok {
		t.Fatalf("missing rows array in %s", body)
	}
	return rows
}

// colsOf extracts the "columns" string array from a query-like body.
func colsOf(t *testing.T, body string) []string {
	t.Helper()
	out := decodeObj(t, body)
	raw, ok := out["columns"].([]any)
	if !ok {
		t.Fatalf("missing columns array in %s", body)
	}
	cols := make([]string, len(raw))
	for i, c := range raw {
		s, ok := c.(string)
		if !ok {
			t.Fatalf("column %d not a string: %v (%s)", i, c, body)
		}
		cols[i] = s
	}
	return cols
}

// requireRows asserts the exact content of a query-like "rows" payload.
func requireRows(t *testing.T, body string, want [][]any) {
	t.Helper()
	requireDeep(t, body, "rows", rowsOf(t, body), want)
}

// requireColumns asserts the exact "columns" payload.
func requireColumns(t *testing.T, body string, want []string) {
	t.Helper()
	requireDeep(t, body, "columns", colsOf(t, body), want)
}

// arrObjs decodes a top-level JSON array of objects (explorer/admin shape).
func arrObjs(t *testing.T, body string) []map[string]any {
	t.Helper()
	raw := decodeArr(t, body)
	out := make([]map[string]any, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry %d not an object: %v (%s)", i, e, body)
		}
		out = append(out, m)
	}
	return out
}

// findObj returns the first array entry where key == val.
func findObj(t *testing.T, body, key string, val any, entries []map[string]any) map[string]any {
	t.Helper()
	for _, e := range entries {
		if reflect.DeepEqual(normJSON(e[key]), normJSON(val)) {
			return e
		}
	}
	t.Fatalf("no entry with %s=%v in %s", key, val, body)
	return nil
}

// requireKeys asserts an object carries exactly the expected key set.
func requireKeys(t *testing.T, what string, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	requireDeep(t, what, "keys", sortedStrs(got), sortedStrs(want))
}

func sortedStrs(s []string) []string {
	out := append([]string{}, s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// jsonUnmarshal decodes a body into v (for non-object top levels).
func jsonUnmarshal(body string, v any) error {
	return json.Unmarshal([]byte(body), v)
}

// mapKeys returns the key set of an object as a sorted slice (for requireDeep).
func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return sortedStrs(out)
}
