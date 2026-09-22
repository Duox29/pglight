package test

import (
	"fmt"
	"strings"
	"testing"
)

// --- /api/table-data ---

func TestTableDataHappy(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1,'a'),(2,'b'),(3,'c')`, tbl))

	code, body := callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&limit=2&offset=0"))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	requireColumns(t, body, []string{"id", "v"})
	requireRows(t, body, [][]any{{1, "a"}, {2, "b"}})
	requireDeep(t, body, "has_more", out["has_more"], true)
	// second page: last row, no more
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&limit=2&offset=2"))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{3, "c"}})
	requireDeep(t, body, "has_more", decodeObj(t, body)["has_more"], false)
	// filter id>1 + order DESC: exact ordered content
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&filter="+`id+%3E+1`+"&order=id+DESC"))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{3, "c"}, {2, "b"}})
	// ASC variant flips the order
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&filter="+`id+%3E+1`+"&order=id+ASC"))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{2, "b"}, {3, "c"}})
}

func TestTableDataFailEdge(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	code, body := callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public"))
	requireErrContains(t, body, code, 400, "table required")
	code, body = callGET(t, h.TableData, "/api/table-data?table="+tbl)
	requireErrContains(t, body, code, 401, "not connected")
	// edge: limit clamp — 0 and huge both fall back to 100 (no crash, 200)
	for _, lim := range []string{"0", "9999", "-5"} {
		code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&limit="+lim))
		requireStatus(t, body, code, 200)
	}
	// edge: hostile order is identifier-sanitized, never executes as SQL.
	// "id; DROP ..." becomes a quoted unknown column => 400, table intact.
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1)`, tbl))
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&order=id%3B+DROP+TABLE+"+tbl))
	requireStatus(t, body, code, 400)
	if n := len(queryRows(t, h, sid, fmt.Sprintf(`SELECT * FROM %s`, tbl))); n != 1 {
		t.Fatalf("order injection touched table: %d rows", n)
	}
	// edge: unknown direction collapses to ASC
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&order=id+SIDEWAYS"))
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.TableData, withSID(sid, "/api/table-data?schema=public&table="+tbl+"&filter=id+%3E+1+OR+1%3D1"))
	requireErrContains(t, body, code, 400, "unsupported filter")
}

// --- /api/search ---

func TestSearchHappyFailEdge(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, needle_col_xyz int)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	code, body := callGET(t, h.Search, withSID(sid, "/api/search?q=needle_col_xyz"))
	requireStatus(t, body, code, 200)
	hits := arrObjs(t, body)
	if len(hits) == 0 {
		t.Fatalf("no search hits: %s", body)
	}
	col := findObj(t, body, "name", tbl+".needle_col_xyz", hits)
	requireKeys(t, body, col, "kind", "schema", "name", "detail")
	requireDeep(t, body, "hit.kind", col["kind"], "column")
	requireDeep(t, body, "hit.schema", col["schema"], "public")
	requireDeep(t, body, "hit.detail", col["detail"], "integer")
	code, body = callGET(t, h.Search, "/api/search?q=needle_col_xyz")
	requireErrContains(t, body, code, 401, "not connected")
	// edge: short/empty term returns [] without hitting PG hard
	for _, q := range []string{"", "x"} {
		code, body = callGET(t, h.Search, withSID(sid, "/api/search?q="+q))
		requireStatus(t, body, code, 200)
		if strings.TrimSpace(body) != "[]" {
			t.Fatalf("short term %q must yield [], got %s", q, body)
		}
	}
}

// --- /api/row (ports rowop_test.go unit checks to endpoint behavior) ---

func TestRowOpBigintRoundTrip(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id bigint PRIMARY KEY, v numeric(18,6), note text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	const big = "9223372036854775807"

	const precise = "123456789012.123456"
	code, body := callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"insert","values":{"id":%s,"v":%s,"note":"__NULL__"}}`, sid, tbl, big, precise))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "insert", decodeObj(t, body), map[string]any{"rows_affected": 1, "in_txn": false})
	// Exact numerics cross the wire as strings (JS-safe): verify through the
	// query endpoint, which applies the same jsonSafeCells serialization as
	// the UI path. Raw pgx decodes int8 as int64 (exact in Go).
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT id, v, note FROM %s"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	for _, want := range []string{`"` + big + `"`, `"` + precise + `"`, `"__NULL__"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("wire value %s missing in %s", want, body)
		}
	}
	data := queryRows(t, h, sid, fmt.Sprintf(`SELECT id, v, note FROM %s`, tbl))
	if len(data) != 1 {
		t.Fatalf("rows=%v", data)
	}
	// Raw pgx layer: int8 decodes as exact int64 (no precision loss in Go;
	// the string wire format was asserted above through /api/query).
	if v, ok := data[0][0].(int64); !ok || v != 9223372036854775807 {
		t.Fatalf("bigint inexact: %T %v", data[0][0], data[0][0])
	}
	// numeric(18,6) keeps its scale: compare the text rendering.
	ntxt := queryRows(t, h, sid, fmt.Sprintf(`SELECT v::text FROM %s`, tbl))
	if len(ntxt) != 1 || fmt.Sprint(ntxt[0][0]) != precise {
		t.Fatalf("numeric corrupted: %v", ntxt)
	}
	if data[0][2] != "__NULL__" {
		t.Fatalf("literal __NULL__ corrupted: %v", data[0][2])
	}
	// real NULL via JSON null: update reports 1 row, content goes NULL
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"update","values":{"note":null},"where":{"id":%q},"single":true}`, sid, tbl, big))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "update", decodeObj(t, body), map[string]any{"rows_affected": 1, "in_txn": false})
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT note FROM %s"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{nil}})
	// stringified-PK delete resolves exactly once
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"delete","where":{"id":%q},"single":true}`, sid, tbl, big))
	requireStatus(t, body, code, 200)
	if decodeObj(t, body)["rows_affected"] != float64(1) {
		t.Fatalf("rows_affected=%s", body)
	}
}

func TestRowOpGuards(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (a text, b text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES ('x','y'),('x','y')`, tbl))

	// bare update/delete refused (pgAdmin safety, port of buildDelete unit)
	code, body := callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"delete","where":{}}`, sid, tbl))
	requireErrContains(t, body, code, 400, "WHERE")
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"update","values":{"a":"z"},"where":{}}`, sid, tbl))
	requireErrContains(t, body, code, 400, "WHERE")
	// single delete on duplicates => 409, nothing touched
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"delete","where":{"a":"x","b":"y"},"single":true}`, sid, tbl))
	requireStatus(t, body, code, 409)
	if n := len(queryRows(t, h, sid, fmt.Sprintf(`SELECT * FROM %s`, tbl))); n != 2 {
		t.Fatalf("duplicate rows touched: %d remain", n)
	}
	// unknown op / no session / bad json / wrong method
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"frobnicate"}`, sid, tbl))
	requireErrContains(t, body, code, 400, "unknown op")
	code, body = callPOST(t, h.RowOp, "/api/row",
		fmt.Sprintf(`{"schema":"public","table":%q,"op":"delete","where":{"a":"x"}}`, tbl))
	requireErrContains(t, body, code, 401, "not connected")
	code, body = callPOST(t, h.RowOp, "/api/row", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	code, _ = callMethod(t, h.RowOp, "GET", "/api/row", "")
	requireStatus(t, "", code, 405)
}

// --- /api/rows-delete ---

func TestBatchDeleteAtomic(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1),(2),(3)`, tbl))

	// one bad entry rolls everything back
	code, body := callPOST(t, h.BatchDelete, "/api/rows-delete",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"where":[{"id":1},{"id":2},{"id":999}]}`, sid, tbl))
	requireStatus(t, body, code, 409)
	if n := len(queryRows(t, h, sid, fmt.Sprintf(`SELECT * FROM %s ORDER BY 1`, tbl))); n != 3 {
		t.Fatalf("partial batch delete: %d rows remain", n)
	}
	// good batch: exact deleted count, one row left
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"where":[{"id":1},{"id":2}]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "batch", decodeObj(t, body), map[string]any{"deleted": 2})
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT id FROM %s"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{3}})
	// in-txn path reports in_txn:true and defers the commit to the session
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"where":[{"id":3}]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "batch.in_txn", decodeObj(t, body), map[string]any{"deleted": 1, "in_txn": true})
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT id FROM %s"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{3}})
	// fail: empty / oversized / missing table / no session
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"where":[]}`, sid, tbl))
	requireErrContains(t, body, code, 400, "where list")
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","where":[{"id":1}]}`, sid))
	requireErrContains(t, body, code, 400, "table required")
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete", `{"table":"x","where":[{"id":1}]}`)
	requireErrContains(t, body, code, 401, "not connected")
}

func TestExplicitTxnRowOperationsRollbackOnlyFailedOperation(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`INSERT INTO %s VALUES (1,'a'),(2,'b'),(3,'c')`, tbl))

	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	code, body := callPOST(t, h.RowOp, "/api/row", fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"op":"update","values":{"v":"changed"},"where":{"v":"missing"},"single":true}`, sid, tbl))
	requireErrContains(t, body, code, 409, "stale data")
	code, body = callPOST(t, h.BatchDelete, "/api/rows-delete", fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"where":[{"id":1},{"id":999}]}`, sid, tbl))
	requireErrContains(t, body, code, 409, "expected exactly 1")
	_, _ = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"commit"`))

	rows := queryRows(t, h, sid, fmt.Sprintf(`SELECT id,v FROM %s ORDER BY id`, tbl))
	if len(rows) != 3 || fmt.Sprint(rows[0][1]) != "a" {
		t.Fatalf("failed explicit-txn operations changed committed data: %v", rows)
	}
}

// --- /api/import ---

func TestImportHappyFailEdge(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	code, body := callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"columns":["id","v"],"rows":[[1,"a"],[2,"b"]]}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "import", decodeObj(t, body), map[string]any{"rows_affected": 2})
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT id, v FROM %s ORDER BY 1"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{1, "a"}, {2, "b"}})
	// on_conflict: conflicting row skipped, count is 0, stored value kept
	code, body = callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"schema":"public","table":%q,"columns":["id","v"],"rows":[[1,"a2"]],"on_conflict_do_nothing":true}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "import.conflict", decodeObj(t, body), map[string]any{"rows_affected": 0})
	code, body = callPOST(t, h.Query, "/api/query",
		fmt.Sprintf(`{"session_id":%q,"sql":"SELECT v FROM %s WHERE id = 1"}`, sid, tbl))
	requireStatus(t, body, code, 200)
	requireRows(t, body, [][]any{{"a"}})

	// fail cases
	code, body = callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"table":%q,"columns":[],"rows":[[1]]}`, sid, tbl))
	requireErrContains(t, body, code, 400, "table, columns and rows required")
	code, body = callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"table":%q,"columns":["id"],"rows":[[1,2]]}`, sid, tbl))
	requireErrContains(t, body, code, 400, "row width")
	code, body = callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"table":%q,"columns":[""],"rows":[[1]]}`, sid, tbl))
	requireErrContains(t, body, code, 400, "empty column name")
	code, body = callPOST(t, h.Import, "/api/import", `{"table":"x","columns":["a"],"rows":[[1]]}`)
	requireErrContains(t, body, code, 401, "not connected")
	code, _ = callMethod(t, h.Import, "GET", "/api/import", "")
	requireStatus(t, "", code, 405)
	// edge: atomic — second row violates PK, nothing is added
	before := len(queryRows(t, h, sid, fmt.Sprintf(`SELECT * FROM %s`, tbl)))
	code, body = callPOST(t, h.Import, "/api/import",
		fmt.Sprintf(`{"session_id":%q,"table":%q,"columns":["id","v"],"rows":[[9,"n"],[1,"dup"]]}`, sid, tbl))
	requireStatus(t, body, code, 400)
	after := len(queryRows(t, h, sid, fmt.Sprintf(`SELECT * FROM %s`, tbl)))
	if after != before {
		t.Fatalf("import not atomic: before=%d after=%d (%s)", before, after, body)
	}
	// edge: 20001 rows rejected before touching the DB
	big := `{"session_id":` + fmt.Sprintf(`%q`, sid) + `,"table":` + fmt.Sprintf(`%q`, tbl) + `,"columns":["id","v"],"rows":[`
	for i := 0; i < 20001; i++ {
		if i > 0 {
			big += ","
		}
		big += fmt.Sprintf(`[%d,"x"]`, 100000+i)
	}
	big += `]}`
	code, body = callPOST(t, h.Import, "/api/import", big)
	requireErrContains(t, body, code, 400, "max 20000")
}
