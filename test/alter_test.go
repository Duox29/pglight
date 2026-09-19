package test

import (
	"fmt"
	"strings"
	"testing"
)

func alterBody(sid, tbl, inner string) string {
	inner = strings.TrimSpace(inner)
	inner = strings.TrimSuffix(strings.TrimPrefix(inner, "{"), "}")
	base := fmt.Sprintf(`"schema":"public","table":%q`, tbl)
	if inner == "" {
		return fmt.Sprintf(`{"session_id":%q,%s}`, sid, base)
	}
	return fmt.Sprintf(`{"session_id":%q,%s,%s}`, sid, base, inner)
}

func TestAlterColumns(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v text)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl))

	colDefs := func() []map[string]any {
		t.Helper()
		code, body := callGET(t, h.Columns, withSID(sid, "/api/columns?schema=public&table="+tbl))
		requireStatus(t, body, code, 200)
		return arrObjs(t, body)
	}
	// add_column happy (with default): exact new column row
	code, body := callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"add_column","column":"extra","type":"int","default":"0"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "add_column", decodeObj(t, body), map[string]any{"ok": true, "in_txn": false})
	extra := findObj(t, body, "name", "extra", colDefs())
	requireDeep(t, body, "extra", extra, map[string]any{
		"name": "extra", "type": "integer", "nullable": "YES",
		"default": "0", "pk": false, "comment": "",
	})
	// set_default + drop default: default value transitions 0 -> 7 -> nil
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_default","column":"extra","default":"7"`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "default", findObj(t, body, "name", "extra", colDefs())["default"], "7")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_default","column":"extra","drop_default":true}`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "drop_default", findObj(t, body, "name", "extra", colDefs())["default"], nil)
	// set_nullable false -> NO, then true -> YES (real transitions on v)
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_nullable","column":"v","nullable":false}`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "not-null", findObj(t, body, "name", "v", colDefs())["nullable"], "NO")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_nullable","column":"v","nullable":true}`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "nullable", findObj(t, body, "name", "v", colDefs())["nullable"], "YES")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_nullable","column":"v"}`))
	requireErrContains(t, body, code, 400, "nullable required")
	// alter_type int -> bigint: type cell flips
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"alter_type","column":"extra","type":"bigint"}`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "alter_type", findObj(t, body, "name", "extra", colDefs())["type"], "bigint")
	// rename_column: old name gone, new name present with same type
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"rename_column","column":"extra","new_name":"extra2"}`))
	requireStatus(t, body, code, 200)
	cols := colDefs()
	findObj(t, body, "name", "extra2", cols)
	for _, c := range cols {
		if c["name"] == "extra" {
			t.Fatalf("old column name still listed: %v", cols)
		}
	}
	requireDeep(t, body, "renamed.type", findObj(t, body, "name", "extra2", cols)["type"], "bigint")
	// drop_column: gone from the listing
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"drop_column","column":"extra2"}`))
	requireStatus(t, body, code, 200)
	for _, c := range colDefs() {
		if c["name"] == "extra2" {
			t.Fatalf("dropped column still listed: %v", c)
		}
	}
	// fail: bad type, missing column, expression with ';'
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"add_column","column":"bad","type":"no_such_type_xyz"}`))
	requireErrContains(t, body, code, 400, "unknown type")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"drop_column"}`))
	requireErrContains(t, body, code, 400, "column required")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"set_default","column":"v","default":"1; DROP TABLE x"}`))
	requireErrContains(t, body, code, 400, "';'")
}

func TestAlterConstraintsIndexes(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY, v int)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s_renamed`, tbl))

	consList := func() []map[string]any {
		t.Helper()
		code, body := callGET(t, h.Constraints, withSID(sid, "/api/constraints?schema=public&table="+tbl))
		requireStatus(t, body, code, 200)
		return arrObjs(t, body)
	}
	// add + drop constraint: CHECK def stored normalized, then gone
	code, body := callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"add_constraint","constraint":"ck_pos","def":"CHECK (v > 0)"}`))
	requireStatus(t, body, code, 200)
	ck := findObj(t, body, "name", "ck_pos", consList())
	requireDeep(t, body, "ck", ck, map[string]any{"name": "ck_pos", "type": "CHECK", "def": "CHECK (v > 0)"})
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"drop_constraint","constraint":"ck_pos"}`))
	requireStatus(t, body, code, 200)
	for _, c := range consList() {
		if c["name"] == "ck_pos" {
			t.Fatalf("dropped constraint still listed: %s", body)
		}
	}
	// fail: def without CHECK/UNIQUE head, missing def
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"add_constraint","def":"WHATEVER (v)"}`))
	requireErrContains(t, body, code, 400, "CHECK|UNIQUE")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"add_constraint"}`))
	requireErrContains(t, body, code, 400, "def required")
	idxList := func() []map[string]any {
		t.Helper()
		code, body := callGET(t, h.Objects, withSID(sid, "/api/objects?kind=indexes&schema=public"))
		requireStatus(t, body, code, 200)
		return arrObjs(t, body)
	}
	// create + rename + drop index: object listing tracks each step
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"create_index","index":%q,"columns":["v"]}`, tbl+"_idx")))
	requireStatus(t, body, code, 200)
	findObj(t, body, "name", tbl+"_idx", idxList())
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"rename_index","index":%q,"new_name":%q}`, tbl+"_idx", tbl+"_idx2")))
	requireStatus(t, body, code, 200)
	idxs := idxList()
	findObj(t, body, "name", tbl+"_idx2", idxs)
	for _, x := range idxs {
		if x["name"] == tbl+"_idx" {
			t.Fatalf("old index name still listed")
		}
	}
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"drop_index","index":%q}`, tbl+"_idx2")))
	requireStatus(t, body, code, 200)
	for _, x := range idxList() {
		if x["name"] == tbl+"_idx2" {
			t.Fatalf("dropped index still listed")
		}
	}
	// fail: bad method, bad direction
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"create_index","columns":["v"],"method":"weird"}`))
	requireErrContains(t, body, code, 400, "unknown method")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"create_index","columns":["v SIDEWAYS"]}`))
	requireErrContains(t, body, code, 400, "direction")
	// rename_table: tables listing flips names, then flips back
	tblList := func() []map[string]any {
		t.Helper()
		code, body := callGET(t, h.Tables, withSID(sid, "/api/tables?schema=public"))
		requireStatus(t, body, code, 200)
		return arrObjs(t, body)
	}
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"rename_table","new_name":%q}`, tbl+"_renamed")))
	requireStatus(t, body, code, 200)
	renamed := findObj(t, body, "name", tbl+"_renamed", tblList())
	requireDeep(t, body, "renamed.schema", renamed["schema"], "public")
	for _, x := range tblList() {
		if x["name"] == tbl {
			t.Fatalf("old table name still listed")
		}
	}
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl+"_renamed", fmt.Sprintf(`"op":"rename_table","new_name":%q}`, tbl)))
	requireStatus(t, body, code, 200)
	findObj(t, body, "name", tbl, tblList())
}

func TestAlterTriggersAndGuards(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE %s (id int PRIMARY KEY)`, tbl))
	defer execSQL(t, h, sid, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl))
	execSQL(t, h, sid, `CREATE OR REPLACE FUNCTION pglight_t_trg() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql`)
	defer execSQL(t, h, sid, `DROP FUNCTION pglight_t_trg()`)

	trg := tbl + "_trg"
	trgList := func() []map[string]any {
		t.Helper()
		code, body := callGET(t, h.Triggers, withSID(sid, "/api/triggers?schema=public&table="+tbl))
		requireStatus(t, body, code, 200)
		return arrObjs(t, body)
	}
	code, body := callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"create_trigger","trigger":%q,"timing":"BEFORE","events":["INSERT"],"function":"pglight_t_trg()"}`, trg)))
	requireStatus(t, body, code, 200)
	created := findObj(t, body, "name", trg, trgList())
	requireDeep(t, body, "trg.event", created["event"], "INSERT")
	requireDeep(t, body, "trg.timing", created["timing"], "BEFORE")
	requireDeep(t, body, "trg.enabled", created["enabled"], "O")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"disable_trigger","trigger":%q}`, trg)))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "trg.disabled", findObj(t, body, "name", trg, trgList())["enabled"], "D")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"enable_trigger","trigger":%q}`, trg)))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "trg.enabled-again", findObj(t, body, "name", trg, trgList())["enabled"], "O")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, fmt.Sprintf(`"op":"drop_trigger","trigger":%q}`, trg)))
	requireStatus(t, body, code, 200)
	for _, x := range trgList() {
		if x["name"] == trg {
			t.Fatalf("dropped trigger still listed")
		}
	}
	// fail: bad timing / event / function
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"create_trigger","trigger":"t1","timing":"SOMETIMES","events":["INSERT"],"function":"pglight_t_trg()"}`))
	requireErrContains(t, body, code, 400, "timing")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"create_trigger","trigger":"t1","timing":"BEFORE","events":["FROBNICATE"],"function":"pglight_t_trg()"}`))
	requireErrContains(t, body, code, 400, "bad event")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table",
		alterBody(sid, tbl, `"op":"create_trigger","trigger":"t1","timing":"BEFORE","events":["INSERT"],"function":"; DROP"}`))
	requireErrContains(t, body, code, 400, "function")
	// guards: unknown op, missing table, bad ident, no session, wrong method, bad json
	code, body = callPOST(t, h.AlterTable, "/api/alter-table", alterBody(sid, tbl, `"op":"levitate"}`))
	requireErrContains(t, body, code, 400, "unknown op")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table", postBody(sid, `"op":"add_column","column":"c","type":"int"}`))
	requireErrContains(t, body, code, 400, "table required")
	code, body = callPOST(t, h.AlterTable, "/api/alter-table", `{"op":"x"}`)
	requireErrContains(t, body, code, 401, "not connected")
	code, _ = callMethod(t, h.AlterTable, "GET", "/api/alter-table", "")
	requireStatus(t, "", code, 405)
	code, body = callPOST(t, h.AlterTable, "/api/alter-table", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
}
