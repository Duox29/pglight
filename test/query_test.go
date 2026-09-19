package test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func qBody(sid, sql string, limit int) string {
	if limit > 0 {
		return fmt.Sprintf(`{"session_id":%q,"sql":%q,"limit":%d}`, sid, sql, limit)
	}
	return fmt.Sprintf(`{"session_id":%q,"sql":%q}`, sid, sql)
}

func TestQueryHappySingle(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT 1 AS one, 'hi' AS greeting", 0))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	requireColumns(t, body, []string{"one", "greeting"})
	requireRows(t, body, [][]any{{1, "hi"}})
	// SELECT 1 is int4 (oid 23)
	types, _ := out["types"].([]any)
	if len(types) != 2 {
		t.Fatalf("want 2 type oids, got %v (%s)", types, body)
	}
	requireDeep(t, body, "types[0]", types[0], "23")
	requireDeep(t, body, "rows_affected", out["rows_affected"], 1)
	requireDeep(t, body, "in_txn", out["in_txn"], false)
	if ms, _ := out["duration_ms"].(float64); ms < 0 {
		t.Fatalf("negative duration_ms: %s", body)
	}
}

func TestQueryHappyMultiStatement(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT 1 AS a; SELECT 2 AS b;", 0))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	results, _ := out["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %s", body)
	}
	r0, _ := results[0].(map[string]any)
	r1, _ := results[1].(map[string]any)
	requireDeep(t, body, "results[0].statement", r0["statement"], "SELECT 1 AS a")
	requireDeep(t, body, "results[0].columns", r0["columns"], []any{"a"})
	requireDeep(t, body, "results[0].rows", r0["rows"], []any{[]any{1}})
	requireDeep(t, body, "results[1].statement", r1["statement"], "SELECT 2 AS b")
	requireDeep(t, body, "results[1].columns", r1["columns"], []any{"b"})
	requireDeep(t, body, "results[1].rows", r1["rows"], []any{[]any{2}})
}

func TestQueryHappyLimitWrapper(t *testing.T) {
	h, sid := newHandler(t)
	// limit applies to single SELECTs via the wrapper; 3 rows capped to 2.
	code, body := callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT generate_series(1,3) AS n ORDER BY 1", 2))
	requireStatus(t, body, code, 200)
	requireColumns(t, body, []string{"n"})
	requireRows(t, body, [][]any{{1}, {2}})
}

func TestQueryFail(t *testing.T) {
	h, sid := newHandler(t)
	// empty sql
	code, body := callPOST(t, h.Query, "/api/query", qBody(sid, "   ", 0))
	requireErrContains(t, body, code, 400, "empty sql")
	// syntax error carries code + line/column
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELEC 1", 0))
	out := requireErrContains(t, body, code, 400, "syntax")
	requireDeep(t, body, "code", out["code"], "42601")
	if out["code"] == nil || out["line"] == nil || out["column"] == nil {
		t.Fatalf("error location missing code/line/column: %s", body)
	}
	// multi-statement failure reports statement_index + partial results
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT 1; SELEC 2;", 0))
	out = requireErrContains(t, body, code, 400, "syntax")
	requireDeep(t, body, "statement_index", out["statement_index"], 1)
	requireDeep(t, body, "statements", out["statements"], 2)
	requireDeep(t, body, "statement", out["statement"], "SELEC 2")
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("want 1 partial result, got %s", body)
	}
	r0, _ := results[0].(map[string]any)
	requireDeep(t, body, "partial.rows", r0["rows"], []any{[]any{1}})
	// no session
	code, body = callPOST(t, h.Query, "/api/query", `{"sql":"SELECT 1"}`)
	requireErrContains(t, body, code, 401, "not connected")
	// invalid json
	code, body = callPOST(t, h.Query, "/api/query", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	// wrong method
	code, _ = callMethod(t, h.Query, "GET", "/api/query", "")
	requireStatus(t, "", code, 405)
}

func TestQueryEdge(t *testing.T) {
	h, sid := newHandler(t)
	// Port of TestSplitStatementsOffsets: comments between statements and
	// exact slicing — third statement error must land on line 6 with index 2.
	sql := "-- lead\nSELECT 1;\n\n/* mid */\nSELECT 2;\nSELEC 3;"
	code, body := callPOST(t, h.Query, "/api/query", qBody(sid, sql, 0))
	out := requireErrContains(t, body, code, 400, "syntax")
	if out["statement_index"] != float64(2) {
		t.Fatalf("want statement_index 2, got %v (%s)", out["statement_index"], body)
	}
	if out["line"] != float64(6) {
		t.Fatalf("want line 6, got %v (%s)", out["line"], body)
	}
	// Semicolons inside strings/comments/dollar-quotes must NOT split.
	// One statement => single shape (columns/rows), not results[].
	for _, sql := range []string{
		"SELECT ';' AS s",
		"SELECT 1 /* ; */",
		"-- ;\nSELECT 1",
	} {
		code, body := callPOST(t, h.Query, "/api/query", qBody(sid, sql, 0))
		requireStatus(t, body, code, 200)
		out := decodeObj(t, body)
		if _, ok := out["results"]; ok {
			t.Fatalf("semicolon inside literal split statements for %q: %s", sql, body)
		}
	}
	// exact content of the string-literal case: one row holding ";"
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT ';' AS s", 0))
	requireStatus(t, body, code, 200)
	requireColumns(t, body, []string{"s"})
	requireRows(t, body, [][]any{{";"}})
	// Dollar-quoted body keeps its inner semicolon: CREATE FUNCTION + DROP is
	// exactly 2 statements (not 3), and the first keeps "RETURN 1;" intact.
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid,
		"CREATE OR REPLACE FUNCTION pglight_t_fn() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql; DROP FUNCTION pglight_t_fn()", 0))
	requireStatus(t, body, code, 200)
	out = decodeObj(t, body)
	results, _ := out["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("dollar-quote split wrong, results=%s", body)
	}
	first, _ := results[0].(map[string]any)
	if stmt, _ := first["statement"].(string); !strings.Contains(stmt, "RETURN 1;") {
		t.Fatalf("function body truncated: %v", first)
	}
	// Non-SELECT single statement passes through untouched (no wrapper).
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT * FROM (SELECT 1 AS x) q", 0))
	requireStatus(t, body, code, 200)
	// unknown table error still maps a code
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELECT * FROM no_such_table_xyz", 0))
	out = requireErrContains(t, body, code, 400, "no_such_table_xyz")
	requireDeep(t, body, "code", out["code"], "42P01")
	// error inside txn still reports in_txn:true
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	code, body = callPOST(t, h.Query, "/api/query", qBody(sid, "SELEC 1", 0))
	out = requireErrContains(t, body, code, 400, "syntax")
	if out["in_txn"] != true {
		t.Fatalf("want in_txn:true, got %s", body)
	}
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	_ = strings.TrimSpace
}

// --- /api/explain ---

func TestExplainHappy(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Explain, "/api/explain",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT 1"}`, sid))
	requireStatus(t, body, code, 200)
	var plans []any
	if err := json.Unmarshal([]byte(body), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("explain must return 1-element plan array: %s", body)
	}
	p0, _ := plans[0].(map[string]any)
	plan, _ := p0["Plan"].(map[string]any)
	requireDeep(t, body, "Plan.Node Type", plan["Node Type"], "Result")
	code, body = callPOST(t, h.Explain, "/api/explain",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT 1","analyze":true}`, sid))
	requireStatus(t, body, code, 200)
	if err := json.Unmarshal([]byte(body), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("explain analyze must return 1-element plan array: %s", body)
	}
	p0, _ = plans[0].(map[string]any)
	plan, _ = p0["Plan"].(map[string]any)
	requireDeep(t, body, "analyze.Actual Rows", plan["Actual Rows"], 1)
	// Execution Time sits next to Plan, not inside it (PG JSON shape)
	if _, ok := p0["Execution Time"]; !ok {
		t.Fatalf("analyze lacks Execution Time: %s", body)
	}
}

func TestExplainFailEdge(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Explain, "/api/explain", postBody(sid, `"sql":""`))
	requireErrContains(t, body, code, 400, "empty sql")
	code, body = callPOST(t, h.Explain, "/api/explain",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELEC 1"}`, sid))
	requireErrContains(t, body, code, 400, "syntax")
	code, body = callPOST(t, h.Explain, "/api/explain", `{"sql":"SELECT 1"}`)
	requireErrContains(t, body, code, 401, "not connected")
	code, body = callPOST(t, h.Explain, "/api/explain", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	code, _ = callMethod(t, h.Explain, "GET", "/api/explain", "")
	requireStatus(t, "", code, 405)
}
