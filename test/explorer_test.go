package test

import (
	"fmt"
	"strings"
	"testing"
)

func TestExplorerBasics(t *testing.T) {
	h, sid := newHandler(t)
	// databases: postgres entry has exact shape + allow_conn true
	code, body := callGET(t, h.Databases, withSID(sid, "/api/databases"))
	requireStatus(t, body, code, 200)
	dbs := arrObjs(t, body)
	pg := findObj(t, body, "name", "postgres", dbs)
	requireKeys(t, body, pg, "name", "size", "allow_conn")
	requireDeep(t, body, "postgres.allow_conn", pg["allow_conn"], true)
	if sz, _ := pg["size"].(string); sz == "" {
		t.Fatalf("postgres size empty: %s", body)
	}
	// schemas: exact string list containing public
	code, body = callGET(t, h.Schemas, withSID(sid, "/api/schemas"))
	requireStatus(t, body, code, 200)
	var schemas []string
	if err := jsonUnmarshal(body, &schemas); err != nil || len(schemas) == 0 {
		t.Fatalf("schemas must be a non-empty string array: %s", body)
	}
	found := false
	for _, s := range schemas {
		if s == "public" {
			found = true
		}
		if s == "" {
			t.Fatalf("empty schema name in %s", body)
		}
	}
	if !found {
		t.Fatalf("schemas lack public: %s", body)
	}
	// tables: own entry with exact shape + values
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	// reltuples is a planner estimate and is -1 on a fresh, unanalyzed table.
	// Make the statistic required by this assertion explicit for seeded data.
	execSQL(t, h, sid, "ANALYZE public.authors")
	code, body = callGET(t, h.Tables, withSID(sid, "/api/tables?schema=public"))
	requireStatus(t, body, code, 200)
	mine := findObj(t, body, "name", tbl, arrObjs(t, body))
	requireKeys(t, body, mine, "schema", "name", "type", "columns", "est_rows")
	requireDeep(t, body, "table.schema", mine["schema"], "public")
	requireDeep(t, body, "table.type", mine["type"], "BASE TABLE")
	// count(*) is int8: exact numerics cross as strings, not numbers
	requireDeep(t, body, "table.columns", mine["columns"], "2")
	// seeded demo data is intact: authors has 3 rows estimated
	authors := findObj(t, body, "name", "authors", arrObjs(t, body))
	requireDeep(t, body, "authors.est_rows", authors["est_rows"], "3")
	// fail: no session
	code, body = callGET(t, h.Databases, "/api/databases")
	requireErrContains(t, body, code, 401, "not connected")
	code, body = callGET(t, h.Schemas, "/api/schemas")
	requireErrContains(t, body, code, 401, "not connected")
	code, body = callGET(t, h.Tables, "/api/tables")
	requireErrContains(t, body, code, 401, "not connected")
	// edge: tables without schema filter still lists ours
	code, body = callGET(t, h.Tables, withSID(sid, "/api/tables"))
	requireStatus(t, body, code, 200)
	findObj(t, body, "name", tbl, arrObjs(t, body))
}

// --- /api/objects (all kinds) ---

func TestObjectsKinds(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE VIEW %s_v AS SELECT id FROM %s`, tbl, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP VIEW %s_v`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE INDEX %s_idx ON %s (id)`, tbl, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP INDEX %s_idx`, tbl))
	for _, kind := range []string{"views", "matviews", "foreign", "functions", "sequences", "indexes", "types", "triggers"} {
		code, body := callGET(t, h.Objects, withSID(sid, "/api/objects?kind="+kind+"&schema=public"))
		requireStatus(t, body, code, 200)
	}
	// view entry: exact {schema, name}
	code, body := callGET(t, h.Objects, withSID(sid, "/api/objects?kind=views&schema=public"))
	requireStatus(t, body, code, 200)
	v := findObj(t, body, "name", tbl+"_v", arrObjs(t, body))
	requireKeys(t, body, v, "schema", "name")
	requireDeep(t, body, "view.schema", v["schema"], "public")
	// index entry: exact {schema, name}
	code, body = callGET(t, h.Objects, withSID(sid, "/api/objects?kind=indexes&schema=public"))
	requireStatus(t, body, code, 200)
	idx := findObj(t, body, "name", tbl+"_idx", arrObjs(t, body))
	requireKeys(t, body, idx, "schema", "name")
	requireDeep(t, body, "index.schema", idx["schema"], "public")
	// functions carry the lang third column; pg_catalog is excluded by design,
	// so the seeded public function is the content probe
	code, body = callGET(t, h.Objects, withSID(sid, "/api/objects?kind=functions&schema=public"))
	requireStatus(t, body, code, 200)
	fns := arrObjs(t, body)
	var seeded map[string]any
	for _, f := range fns {
		if strings.HasPrefix(fmt.Sprint(f["name"]), "book_count_by_status(") {
			seeded = f
		}
	}
	if seeded == nil {
		t.Fatalf("seeded book_count_by_status missing: %s", body)
	}
	requireKeys(t, body, seeded, "schema", "name", "lang")
	requireDeep(t, body, "fn.schema", seeded["schema"], "public")
	requireDeep(t, body, "fn.name", seeded["name"], "book_count_by_status(s text)")
	requireDeep(t, body, "fn.lang", seeded["lang"], "sql")
	// fail: unknown kind + no session
	code, body = callGET(t, h.Objects, withSID(sid, "/api/objects?kind=bogus"))
	requireErrContains(t, body, code, 400, "unknown kind")
	code, body = callGET(t, h.Objects, "/api/objects?kind=views")
	requireErrContains(t, body, code, 401, "not connected")
	// edge: foreign kind without rows still returns [] (not null)
	code, body = callGET(t, h.Objects, withSID(sid, "/api/objects?kind=foreign&schema=public"))
	requireStatus(t, body, code, 200)
	if strings.TrimSpace(body) == "null" {
		t.Fatalf("foreign must be [], not null: %s", body)
	}
}

// --- /api/columns /api/ddl (ported quoted-identifier coverage) ---

func TestColumnsDDLQuoted(t *testing.T) {
	h, sid := newHandler(t)
	execSQL(t, h, sid, `DROP SCHEMA IF EXISTS "My Schema" CASCADE`)
	execSQL(t, h, sid, `CREATE SCHEMA "My Schema"`)
	defer execSQL(t, h, sid, `DROP SCHEMA "My Schema" CASCADE`)
	execSQL(t, h, sid, `CREATE TABLE "My Schema"."Order.Items" (id int PRIMARY KEY, note text)`)
	execSQL(t, h, sid, `COMMENT ON TABLE "My Schema"."Order.Items" IS 'weird but valid'`)
	execSQL(t, h, sid, `CREATE VIEW "My Schema"."Odd View" AS SELECT id FROM "My Schema"."Order.Items"`)

	q := fmt.Sprintf("session_id=%s&schema=%s", sid, "My+Schema")
	code, body := callGET(t, h.Columns, "/api/columns?"+q+"&table=Order.Items")
	requireStatus(t, body, code, 200)
	cols := arrObjs(t, body)
	if len(cols) != 2 {
		t.Fatalf("want 2 columns, got %s", body)
	}
	id := findObj(t, body, "name", "id", cols)
	requireKeys(t, body, id, "name", "type", "nullable", "default", "pk", "comment")
	requireDeep(t, body, "col.id", id, map[string]any{
		"name": "id", "type": "integer", "nullable": "NO",
		"default": nil, "pk": true, "comment": "",
	})
	note := findObj(t, body, "name", "note", cols)
	requireDeep(t, body, "col.note.type", note["type"], "text")
	requireDeep(t, body, "col.note.nullable", note["nullable"], "YES")
	requireDeep(t, body, "col.note.pk", note["pk"], false)
	code, body = callGET(t, h.ViewDef, "/api/view-def?"+q+"&name=Odd+View")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "view-def.keys", mapKeys(decodeObj(t, body)), []string{"definition"})
	if !strings.Contains(body, "Order.Items") {
		t.Fatalf("view-def on quoted view: %s", body)
	}
	code, body = callGET(t, h.Constraints, "/api/constraints?"+q+"&table=Order.Items")
	requireStatus(t, body, code, 200)
	cons := arrObjs(t, body)
	if len(cons) != 1 {
		t.Fatalf("want 1 constraint, got %s", body)
	}
	requireDeep(t, body, "constraint.type", cons[0]["type"], "PRIMARY KEY")
	if !strings.Contains(fmt.Sprint(cons[0]["def"]), "id") {
		t.Fatalf("pk def must reference id: %s", body)
	}
	code, body = callGET(t, h.DDL, "/api/ddl?"+q+"&table=Order.Items")
	requireStatus(t, body, code, 200)
	ddl := decodeObj(t, body)
	if !strings.Contains(fmt.Sprint(ddl["ddl"]), `"Order.Items"`) {
		t.Fatalf("ddl must quote weird name: %s", body)
	}
	requireDeep(t, body, "ddl.comment", ddl["comment"], "weird but valid")
}

func TestColumnsDDLEdge(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, note text DEFAULT 'x' NOT NULL)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`COMMENT ON COLUMN %s.note IS 'cmt'`, tbl))
	code, body := callGET(t, h.Columns, withSID(sid, "/api/columns?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	cols := arrObjs(t, body)
	note := findObj(t, body, "name", "note", cols)
	requireDeep(t, body, "col.note", note, map[string]any{
		"name": "note", "type": "text", "nullable": "NO",
		"default": "'x'::text", "pk": false, "comment": "cmt",
	})
	// ddl full shape: owner postgres, 1 pk constraint, no indexes yet
	code, body = callGET(t, h.DDL, withSID(sid, "/api/ddl?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	ddl := decodeObj(t, body)
	requireKeys(t, body, ddl, "ddl", "owner", "comment", "indexes", "foreign_keys", "constraints", "triggers")
	requireDeep(t, body, "ddl.owner", ddl["owner"], "postgres")
	if !strings.Contains(fmt.Sprint(ddl["ddl"]), "note text NOT NULL") {
		t.Fatalf("ddl must carry NOT NULL + default: %s", body)
	}
	cons, _ := ddl["constraints"].([]any)
	if len(cons) != 1 {
		t.Fatalf("want 1 pk constraint, got %s", body)
	}
	idxs, _ := ddl["indexes"].([]any)
	if len(idxs) != 1 {
		t.Fatalf("want the auto PK-backing index, got %s", body)
	}
	idx0, _ := idxs[0].(map[string]any)
	requireDeep(t, body, "ddl.index.name", idx0["name"], tbl+"_pkey")
	if !strings.Contains(fmt.Sprint(idx0["def"]), "UNIQUE") || !strings.Contains(fmt.Sprint(idx0["def"]), "(id)") {
		t.Fatalf("pk index def wrong: %s", body)
	}
	requireDeep(t, body, "ddl.foreign_keys", ddl["foreign_keys"], []any{})
	// foreign_keys carries only real FKs (PK must not leak in — regression)
	child := tbl + "_child"
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, pid int REFERENCES %s(id))`, child, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, child))
	code, body = callGET(t, h.DDL, withSID(sid, "/api/ddl?schema=public&table="+child))
	requireStatus(t, body, code, 200)
	fks, _ := decodeObj(t, body)["foreign_keys"].([]any)
	if len(fks) != 1 {
		t.Fatalf("want exactly 1 FK, got %s", body)
	}
	fk0, _ := fks[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(fk0["def"]), "REFERENCES") || !strings.Contains(fmt.Sprint(fk0["def"]), tbl) {
		t.Fatalf("fk def wrong: %s", body)
	}
	// fail: ddl missing table, no session
	code, body = callGET(t, h.DDL, withSID(sid, "/api/ddl?schema=public"))
	requireErrContains(t, body, code, 400, "table required")
	code, body = callGET(t, h.Columns, "/api/columns?table=x")
	requireErrContains(t, body, code, 401, "not connected")
	// edge: columns on missing table => 200 empty list (not 500/null)
	code, body = callGET(t, h.Columns, withSID(sid, "/api/columns?schema=public&table=no_such_xyz"))
	requireStatus(t, body, code, 200)
	if strings.TrimSpace(body) != "[]" {
		t.Fatalf("missing table must yield [], got %s", body)
	}
}

// --- view/func/seq/type defs ---

func TestDefs(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE VIEW %s_v AS SELECT id FROM %s`, tbl, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP VIEW %s_v`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE SEQUENCE %s_seq`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP SEQUENCE %s_seq`, tbl))
	execSQL(t, h, sid, `DROP TYPE IF EXISTS pglight_t_mood`)
	execSQL(t, h, sid, `CREATE TYPE pglight_t_mood AS ENUM ('a','b')`)
	defer execSQL(t, h, sid, `DROP TYPE pglight_t_mood`)

	code, body := callGET(t, h.ViewDef, withSID(sid, "/api/view-def?schema=public&name="+tbl+"_v"))
	requireStatus(t, body, code, 200)
	def := decodeObj(t, body)
	requireKeys(t, body, def, "definition")
	d, _ := def["definition"].(string)
	if !strings.Contains(d, "SELECT") || !strings.Contains(d, tbl) || !strings.Contains(d, "id") {
		t.Fatalf("view-def content wrong: %q", d)
	}

	code, body = callGET(t, h.FuncDef, withSID(sid, "/api/func-def?schema=pg_catalog&name=now"))
	requireStatus(t, body, code, 200)
	fns := arrObjs(t, body)
	if len(fns) == 0 {
		t.Fatalf("now() not found: %s", body)
	}
	requireKeys(t, body, fns[0], "schema", "name", "def", "lang", "kind")
	requireDeep(t, body, "now.schema", fns[0]["schema"], "pg_catalog")
	requireDeep(t, body, "now.name", fns[0]["name"], "now()")
	if !strings.Contains(fmt.Sprint(fns[0]["def"]), "now") {
		t.Fatalf("now() def wrong: %s", body)
	}

	code, body = callGET(t, h.SeqDef, withSID(sid, "/api/seq-def?schema=public&name="+tbl+"_seq"))
	requireStatus(t, body, code, 200)
	seq := decodeObj(t, body)
	requireDeep(t, body, "seq.schema", seq["schema"], "public")
	requireDeep(t, body, "seq.name", seq["name"], tbl+"_seq")
	requireDeep(t, body, "seq.start_value", seq["start_value"], "1")
	requireDeep(t, body, "seq.increment", seq["increment"], "1")
	requireDeep(t, body, "seq.cycle_option", seq["cycle_option"], "NO")
	requireDeep(t, body, "seq.cache_size", seq["cache_size"], 1)
	requireDeep(t, body, "seq.definition", seq["definition"],
		fmt.Sprintf(`CREATE SEQUENCE "public".%q START WITH 1 INCREMENT BY 1 MINVALUE 1 NO MAXVALUE CACHE 1 NO CYCLE;`, tbl+"_seq"))

	code, body = callGET(t, h.TypeDef, withSID(sid, "/api/type-def?schema=public&name=pglight_t_mood"))
	requireStatus(t, body, code, 200)
	typ := decodeObj(t, body)
	requireDeep(t, body, "type.kind", typ["kind"], "enum")
	requireDeep(t, body, "type.labels", typ["labels"], "a, b")
	requireDeep(t, body, "type.definition", typ["definition"],
		`CREATE TYPE "public"."pglight_t_mood" AS ENUM ('a', 'b');`)
	// fail: missing name
	code, body = callGET(t, h.ViewDef, withSID(sid, "/api/view-def?schema=public"))
	requireErrContains(t, body, code, 400, "name required")
	code, body = callGET(t, h.FuncDef, withSID(sid, "/api/func-def?schema=public"))
	requireErrContains(t, body, code, 400, "name required")
	code, body = callGET(t, h.SeqDef, withSID(sid, "/api/seq-def?schema=public&name=no_seq_xyz"))
	requireErrContains(t, body, code, 400, "sequence not found")
	code, body = callGET(t, h.TypeDef, withSID(sid, "/api/type-def?schema=public&name=no_type_xyz"))
	requireErrContains(t, body, code, 400, "type not found")
	// edge: func-def strips "(args)" signature from explorer
	code, body = callGET(t, h.FuncDef, withSID(sid, "/api/func-def?schema=pg_catalog&name=now()"))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "now().name", arrObjs(t, body)[0]["name"], "now()")
}

// --- constraints / triggers / tablestats / types ---

func TestConstraintsTriggersStatsTypes(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v int UNIQUE CHECK (v > 0))`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE %s`, tbl))

	code, body := callGET(t, h.Constraints, withSID(sid, "/api/constraints?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	cons := arrObjs(t, body)
	if len(cons) != 3 {
		t.Fatalf("want PK+UNIQUE+CHECK = 3 constraints, got %s", body)
	}
	pk := findObj(t, body, "name", tbl+"_pkey", cons)
	requireDeep(t, body, "pk.type", pk["type"], "PRIMARY KEY")
	if !strings.Contains(fmt.Sprint(pk["def"]), "id") {
		t.Fatalf("pk def: %s", body)
	}
	uq := findObj(t, body, "name", tbl+"_v_key", cons)
	requireDeep(t, body, "uq.type", uq["type"], "UNIQUE")
	ck := findObj(t, body, "name", tbl+"_v_check", cons)
	requireDeep(t, body, "ck.type", ck["type"], "CHECK")
	if !strings.Contains(fmt.Sprint(ck["def"]), "v > 0") {
		t.Fatalf("check def: %s", body)
	}
	// schema mode adds the table column
	code, body = callGET(t, h.Constraints, withSID(sid, "/api/constraints?schema=public"))
	requireStatus(t, body, code, 200)
	scons := arrObjs(t, body)
	mine := findObj(t, body, "name", tbl+"_pkey", scons)
	requireKeys(t, body, mine, "name", "table", "type", "def")
	requireDeep(t, body, "schema-mode.table", mine["table"], tbl)
	code, body = callGET(t, h.Constraints, withSID(sid, "/api/constraints"))
	requireErrContains(t, body, code, 400, "schema or table required")

	// triggers: empty table => [] (exact, not null)
	code, body = callGET(t, h.Triggers, withSID(sid, "/api/triggers?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	if strings.TrimSpace(body) != "[]" {
		t.Fatalf("no triggers must be [], got %s", body)
	}
	code, body = callGET(t, h.Triggers, withSID(sid, "/api/triggers?table="+tbl))
	requireStatus(t, body, code, 200)

	code, body = callGET(t, h.TableStats, withSID(sid, "/api/table-stats?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	st := decodeObj(t, body)
	requireKeys(t, body, st,
		"total_size", "table_size", "indexes_size", "est_rows",
		"seq_scan", "idx_scan", "live_tup", "dead_tup",
		"last_vacuum", "last_autovacuum", "last_analyze", "last_autoanalyze")
	// reltuples is -1 until first ANALYZE/VACUUM; bigint stats cross as strings
	requireDeep(t, body, "stats.est_rows", st["est_rows"], "-1")
	requireDeep(t, body, "stats.live_tup", st["live_tup"], "0")
	requireDeep(t, body, "stats.seq_scan", st["seq_scan"], "0")
	for _, k := range []string{"total_size", "table_size", "indexes_size"} {
		if s, _ := st[k].(string); s == "" {
			t.Fatalf("stats.%s empty: %s", k, body)
		}
	}
	code, body = callGET(t, h.TableStats, withSID(sid, "/api/table-stats?schema=public"))
	requireErrContains(t, body, code, 400, "table required")

	// types: own enum entry with exact content
	execSQL(t, h, sid, `DROP TYPE IF EXISTS pglight_t_kind`)
	execSQL(t, h, sid, `CREATE TYPE pglight_t_kind AS ENUM ('x','y')`)
	defer execSQL(t, h, sid, `DROP TYPE pglight_t_kind`)
	code, body = callGET(t, h.Types, withSID(sid, "/api/types?schema=public"))
	requireStatus(t, body, code, 200)
	mine2 := findObj(t, body, "name", "pglight_t_kind", arrObjs(t, body))
	requireKeys(t, body, mine2, "schema", "name", "kind", "comment", "labels")
	requireDeep(t, body, "type.kind", mine2["kind"], "enum")
	requireDeep(t, body, "type.labels", mine2["labels"], "x, y")
	code, body = callGET(t, h.Types, "/api/types")
	requireErrContains(t, body, code, 401, "not connected")
}

// --- /api/erd graph + layout CRUD + no-cross-match port ---

func TestERDNoCrossMatch(t *testing.T) {
	h, sid := newHandler(t)
	execSQL(t, h, sid, `DROP SCHEMA IF EXISTS pglight_erd CASCADE`)
	execSQL(t, h, sid, `CREATE SCHEMA pglight_erd`)
	defer execSQL(t, h, sid, `DROP SCHEMA pglight_erd CASCADE`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.users (id int PRIMARY KEY)`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.companies (id int PRIMARY KEY)`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.orders (id int PRIMARY KEY, user_id int CONSTRAINT fk_user_id REFERENCES pglight_erd.users(id))`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.invoices (id int PRIMARY KEY, user_id int CONSTRAINT fk_user_id REFERENCES pglight_erd.companies(id))`)

	code, body := callGET(t, h.ERD, withSID(sid, "/api/erd?schema=pglight_erd"))
	requireStatus(t, body, code, 200)
	g := decodeObj(t, body)
	requireDeep(t, body, "erd.schema", g["schema"], "pglight_erd")
	requireDeep(t, body, "erd.nodes", g["nodes"], []any{"companies", "invoices", "orders", "users"})
	edges, _ := g["edges"].([]any)
	if len(edges) != 2 {
		t.Fatalf("want exactly 2 FK edges, got %s", body)
	}
	for _, want := range []string{
		`"dst_table":"users","fk":"fk_user_id","src_col":"user_id","src_table":"orders"`,
		`"dst_table":"companies","fk":"fk_user_id","src_col":"user_id","src_table":"invoices"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing edge %s in %s", want, body)
		}
	}
	for _, bad := range []string{
		`"dst_table":"companies","fk":"fk_user_id","src_col":"user_id","src_table":"orders"`,
		`"dst_table":"users","fk":"fk_user_id","src_col":"user_id","src_table":"invoices"`,
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("cross-matched edge %s in %s", bad, body)
		}
	}
	// per-table columns with exact PK/FK badges
	cols, _ := g["columns"].(map[string]any)
	orders, _ := cols["orders"].([]any)
	o0, _ := orders[0].(map[string]any)
	requireDeep(t, body, "erd.orders.id", o0, map[string]any{"name": "id", "type": "integer", "pk": true, "fk": false})
	o1, _ := orders[1].(map[string]any)
	requireDeep(t, body, "erd.orders.user_id", o1, map[string]any{"name": "user_id", "type": "integer", "pk": false, "fk": true})
}

func TestERDEdgeAndLayout(t *testing.T) {
	h, sid := newHandler(t)
	// edge: empty schema defaults to public
	code, body := callGET(t, h.ERD, withSID(sid, "/api/erd"))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "erd.schema", decodeObj(t, body)["schema"], "public")
	// fail: graph without session
	code, body = callGET(t, h.ERD, "/api/erd?schema=public")
	requireErrContains(t, body, code, 401, "not connected")

	// layout CRUD (store-backed, no PG needed beyond handler)
	code, body = callGET(t, h.ERD, "/api/erd?layout=1&connection_id=c1&schema=public")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "layout.empty", decodeObj(t, body)["layout"], map[string]any{})
	code, body = callGET(t, h.ERD, "/api/erd?layout=1&connection_id=&schema=")
	requireErrContains(t, body, code, 400, "connection_id and schema required")
	code, body = callPOST(t, h.ERD, "/api/erd?layout=1", `{"connection_id":"c1","schema":"public","layout":{"a":{"x":1}},"viewport":{"zoom":1}}`)
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "layout.save", decodeObj(t, body), map[string]any{"ok": true})
	code, body = callGET(t, h.ERD, "/api/erd?layout=1&connection_id=c1&schema=public")
	requireStatus(t, body, code, 200)
	got := decodeObj(t, body)
	requireDeep(t, body, "layout.roundtrip", got["layout"], map[string]any{"a": map[string]any{"x": 1}})
	requireDeep(t, body, "layout.viewport", got["viewport"], map[string]any{"zoom": 1})
	code, body = callMethod(t, h.ERD, "DELETE", "/api/erd?layout=1&connection_id=c1&schema=public", "")
	requireStatus(t, body, code, 200)
	code, body = callGET(t, h.ERD, "/api/erd?layout=1&connection_id=c1&schema=public")
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "layout.deleted", decodeObj(t, body)["layout"], map[string]any{})
	code, body = callMethod(t, h.ERD, "DELETE", "/api/erd?layout=1&connection_id=&schema=", "")
	requireErrContains(t, body, code, 400, "connection_id and schema required")
	code, body = callPOST(t, h.ERD, "/api/erd?layout=1", `{"connection_id":"","schema":""}`)
	requireErrContains(t, body, code, 400, "connection_id and schema required")
}
