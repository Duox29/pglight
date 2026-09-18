package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pglight/internal/db"
)

func testMgr(t *testing.T) (*Handler, string) {
	t.Helper()
	mgr := db.New()
	cs := db.ConnString("localhost", 5432, "postgres", "postgres", "postgres", "disable")
	if err := mgr.Add("t", cs); err != nil {
		t.Skipf("test postgres unreachable: %v", err)
	}
	t.Cleanup(func() { mgr.Close("t") })
	return &Handler{Mgr: mgr}, "t"
}

func execSQL(t *testing.T, h *Handler, sid, sql string, args ...any) {
	t.Helper()
	qq, ok := h.Mgr.Q(sid)
	if !ok {
		t.Fatal("no session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := qq.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func callRowOp(t *testing.T, h *Handler, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/row", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RowOp(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return w.Code, out
}

func TestNumValKeepsInt64(t *testing.T) {
	v := numVal(json.Number("9223372036854775807"))
	i, ok := v.(int64)
	if !ok || i != 9223372036854775807 {
		t.Fatalf("bigint collapsed: %T %v", v, v)
	}
	if f, ok := numVal(json.Number("1.5")).(float64); !ok || f != 1.5 {
		t.Fatalf("decimal mishandled: %T %v", numVal(json.Number("1.5")), f)
	}
}

func TestCoerceValTyped(t *testing.T) {
	if v := coerceVal("9223372036854775807", "int8"); v != int64(9223372036854775807) {
		t.Fatalf("int8 string not coerced: %T %v", v, v)
	}
	if v := coerceVal("__NULL__", "text"); v != "__NULL__" {
		t.Fatalf("text must pass through: %v", v)
	}
	if v := coerceVal("abc", "int8"); v != "abc" {
		t.Fatalf("non-numeric int input changed: %v", v)
	}
}

func TestBuildDeleteRefusesBare(t *testing.T) {
	if _, _, err := buildDelete(`"public"."events"`, map[string]any{}, nil); err == nil {
		t.Fatal("bare delete allowed")
	}
}

// Exact numerics cross the wire as strings; the UI sends them back the same
// way and typed coercion restores them for typed comparisons.
func TestBigintPKRoundTrip(t *testing.T) {
	h, sid := testMgr(t)
	execSQL(t, h, sid, `DROP TABLE IF EXISTS pglight_test_big`)
	execSQL(t, h, sid, `CREATE TABLE pglight_test_big (id bigint PRIMARY KEY, v numeric(18,6), note text)`)
	defer execSQL(t, h, sid, `DROP TABLE pglight_test_big`)
	const big = "9223372036854775807"

	code, out := callRowOp(t, h, fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_big","op":"insert","values":{"id":%s,"v":"3.140000","note":"__NULL__"}}`, sid, big))
	if code != 200 || out["error"] != nil {
		t.Fatalf("insert: code=%d out=%v", code, out)
	}
	// Literal __NULL__ must be storable now (no sentinel).
	qq, _ := h.Mgr.Q(sid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cols, data, err := queryJSON(qq, ctx, `SELECT id, v, note FROM pglight_test_big`)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("rows=%v", data)
	}
	_ = cols
	if s, ok := data[0][0].(string); !ok || s != big {
		t.Fatalf("bigint not stringified exactly: %T %v", data[0][0], data[0][0])
	}
	if s, ok := data[0][1].(string); !ok || s != "3.140000" {
		t.Fatalf("numeric not stringified: %T %v", data[0][1], data[0][1])
	}
	if data[0][2] != "__NULL__" {
		t.Fatalf("literal __NULL__ corrupted: %v", data[0][2])
	}
	// Real NULL via JSON null still works.
	code, out = callRowOp(t, h, fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_big","op":"update","values":{"note":null},"where":{"id":"%s"},"single":true}`, sid, big))
	if code != 200 || out["error"] != nil {
		t.Fatalf("update null: code=%d out=%v", code, out)
	}
	// Stringified-PK delete resolves to the typed row exactly once.
	code, out = callRowOp(t, h, fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_big","op":"delete","where":{"id":"%s"},"single":true}`, sid, big))
	if code != 200 || out["error"] != nil {
		t.Fatalf("delete: code=%d out=%v", code, out)
	}
	if out["rows_affected"] != float64(1) {
		t.Fatalf("rows_affected=%v", out["rows_affected"])
	}
}

// Duplicate rows without a unique predicate: single delete must refuse and
// change nothing.
func TestSingleDeleteRefusesDuplicates(t *testing.T) {
	h, sid := testMgr(t)
	execSQL(t, h, sid, `DROP TABLE IF EXISTS pglight_test_dup`)
	execSQL(t, h, sid, `CREATE TABLE pglight_test_dup (a text, b text)`)
	defer execSQL(t, h, sid, `DROP TABLE pglight_test_dup`)
	execSQL(t, h, sid, `INSERT INTO pglight_test_dup VALUES ('x','y'),('x','y')`)

	code, out := callRowOp(t, h, fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_dup","op":"delete","where":{"a":"x","b":"y"},"single":true}`, sid))
	if code != 409 {
		t.Fatalf("expected 409, got %d (%v)", code, out)
	}
	qq, _ := h.Mgr.Q(sid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(qq, ctx, `SELECT * FROM pglight_test_dup`)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 {
		t.Fatalf("duplicate rows were touched: %d remain", len(data))
	}
}

// Batch deletes are atomic: one bad entry rolls the whole batch back.
func TestBatchDeleteAtomic(t *testing.T) {
	h, sid := testMgr(t)
	execSQL(t, h, sid, `DROP TABLE IF EXISTS pglight_test_batch`)
	execSQL(t, h, sid, `CREATE TABLE pglight_test_batch (id int PRIMARY KEY)`)
	defer execSQL(t, h, sid, `DROP TABLE pglight_test_batch`)
	execSQL(t, h, sid, `INSERT INTO pglight_test_batch VALUES (1),(2),(3)`)

	body := fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_batch","where":[{"id":1},{"id":2},{"id":999}]}`, sid)
	r := httptest.NewRequest("POST", "/api/rows-delete", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.BatchDelete(w, r)
	if w.Code != 409 {
		t.Fatalf("expected 409, got %d (%s)", w.Code, w.Body.String())
	}
	qq, _ := h.Mgr.Q(sid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(qq, ctx, `SELECT * FROM pglight_test_batch ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 3 {
		t.Fatalf("partial batch delete: %d rows remain", len(data))
	}

	body = fmt.Sprintf(`{"session_id":"%s","schema":"public","table":"pglight_test_batch","where":[{"id":1},{"id":2}]}`, sid)
	r = httptest.NewRequest("POST", "/api/rows-delete", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.BatchDelete(w, r)
	if w.Code != 200 {
		t.Fatalf("good batch: %d (%s)", w.Code, w.Body.String())
	}
	_, data, err = queryJSON(qq, ctx, `SELECT * FROM pglight_test_batch ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 row left, got %d", len(data))
	}
}
