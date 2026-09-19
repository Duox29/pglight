package test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var mockIntRe = regexp.MustCompile(`^[+-]?\d+$`)

// Mock-data endpoint tests (black-box through the HTTP surface).

func TestMockMeta(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table=mock_users"))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	cols, ok := out["columns"].([]any)
	if !ok || len(cols) == 0 {
		t.Fatalf("missing columns in %s", body)
	}
	byName := map[string]map[string]any{}
	for _, c := range cols {
		m := c.(map[string]any)
		byName[m["name"].(string)] = m
	}
	for _, want := range []string{"id", "email", "age", "status", "created_at", "updated_at"} {
		if byName[want] == nil {
			t.Fatalf("missing column %s in %s", want, body)
		}
	}
	if byName["email"]["semantic_hint"] != "email" {
		t.Fatalf("email semantic_hint = %v", byName["email"]["semantic_hint"])
	}
	if byName["email"]["unique"] != true {
		t.Fatalf("email should be unique: %v", byName["email"])
	}
	if byName["id"]["primary_key"] != true {
		t.Fatalf("id should be pk: %v", byName["id"])
	}
	fks, _ := out["foreign_keys"].([]any)
	if fks == nil {
		t.Fatalf("missing foreign_keys in %s", body)
	}
	chks, ok := out["checks"].([]any)
	if !ok || len(chks) < 3 {
		t.Fatalf("want >=3 checks in %s", body)
	}
	kinds := map[string]bool{}
	for _, c := range chks {
		kinds[c.(map[string]any)["kind"].(string)] = true
	}
	if !kinds["between"] || !kinds["in"] {
		t.Fatalf("want between+in check kinds, got %v (%s)", kinds, body)
	}

	// Fail: unknown table.
	code, body = callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table=nope_missing"))
	requireErrContains(t, body, code, 400, "not found")
	// Fail: no session.
	code, body = callGET(t, h.MockMeta, "/api/mock-data/meta?schema=public&table=mock_users&session_id=bogus")
	requireStatus(t, body, code, 401)
}

func TestMockPreviewWritesNothing(t *testing.T) {
	h, sid := newHandler(t)
	before := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users")[0][0]
	code, body := callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":20,"seed":12345`))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	rows, ok := out["rows"].([]any)
	if !ok || len(rows) != 20 {
		t.Fatalf("want 20 preview rows in %s", body)
	}
	cols := colsOf(t, body)
	if len(cols) != 6 {
		t.Fatalf("preview shows all table columns, got %v", cols)
	}
	after := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users")[0][0]
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("preview modified the table: %v -> %v", before, after)
	}
	// Seeded preview is deterministic.
	code2, body2 := callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":20,"seed":12345`))
	requireStatus(t, body2, code2, 200)
	if body != body2 {
		t.Fatal("same seed gave different preview")
	}
}

func TestMockGenerateSimpleAndPrecision(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id BIGSERIAL PRIMARY KEY, big BIGINT NOT NULL, amount NUMERIC(12,4) NOT NULL,
		name TEXT NOT NULL, active BOOLEAN NOT NULL DEFAULT true)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })

	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"simple","count":50,"seed":99`, tbl)))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	if out["inserted"] != float64(50) || out["generated"] != float64(50) {
		t.Fatalf("bad counts in %s", body)
	}
	if out["seed"] != float64(99) {
		t.Fatalf("seed echo wrong in %s", body)
	}
	// bigint values are exact integers; numeric keeps its exact decimal text.
	rows := queryRows(t, h, sid, "SELECT big::text, amount::text FROM public."+tbl+" LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(rows))
	}
	for _, r := range rows {
		big, amt := fmt.Sprint(r[0]), fmt.Sprint(r[1])
		if !mockIntRe.MatchString(big) {
			t.Fatalf("bigint not exact integer text: %q", big)
		}
		f, err := strconv.ParseFloat(amt, 64)
		if err != nil || f < 0 || f > 10000 {
			t.Fatalf("numeric out of range: %q", amt)
		}
		if !strings.Contains(amt, ".") {
			t.Fatalf("numeric lost scale: %q", amt)
		}
	}
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "50" {
		t.Fatalf("count = %v want 50", n)
	}
}

func TestMockGenerateRollsBackOnFailure(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id SERIAL PRIMARY KEY, status TEXT NOT NULL CHECK (status='fixed'))`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })

	// Simple mode ignores the CHECK and generates random strings, so every
	// row violates it; the private transaction must roll everything back
	// and hint at Advanced mode.
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"simple","count":20,"seed":5`, tbl)))
	requireErrContains(t, body, code, 400, "Advanced mode")
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("failed generation left %v rows (want atomic rollback)", n)
	}
}

func TestMockGenerateExplicitTxnUncommitted(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })

	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"simple","count":10,"seed":11`, tbl)))
	requireStatus(t, body, code, 200)
	if out := decodeObj(t, body); out["in_txn"] != true {
		t.Fatalf("want in_txn=true in %s", body)
	}
	// Visible inside the txn…
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "10" {
		t.Fatalf("count in txn = %v want 10", n)
	}
	// …and rolled back afterwards, proving the endpoint did not commit.
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	execSQL(t, h, sid, "SELECT 1")
	n = queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("count after rollback = %v want 0", n)
	}
}

func TestMockGenerateAdvanced(t *testing.T) {
	h, sid := newHandler(t)
	execSQL(t, h, sid, "DELETE FROM public.mock_users")
	t.Cleanup(func() { execSQL(t, h, sid, "DELETE FROM public.mock_users") })

	body := postBody(sid, `"schema":"public","table":"mock_users","mode":"advanced","count":30,"seed":2024,`+
		`"fields":[
			{"column":"email","generator":"email","unique":true},
			{"column":"age","generator":"integer","params":{"min":18,"max":100}},
			{"column":"status","generator":"choice","params":{"values":["active","disabled"]}},
			{"column":"created_at","generator":"datetime"},
			{"column":"updated_at","generator":"relative_datetime","params":{"source":"created_at","min_offset_days":0,"max_offset_days":30}}],`+
		`"constraints":[{"kind":"compare","left":{"field":"created_at"},"operator":"<=","right":{"field":"updated_at"}}]`)
	code, resp := callPOST(t, h.MockGenerate, "/api/mock-data/generate", body)
	requireStatus(t, resp, code, 200)
	if out := decodeObj(t, resp); out["inserted"] != float64(30) {
		t.Fatalf("bad insert count in %s", resp)
	}
	bad := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users WHERE age NOT BETWEEN 18 AND 100 OR status NOT IN ('active','disabled') OR created_at > updated_at")[0][0]
	if fmt.Sprint(bad) != "0" {
		t.Fatalf("%v rows violate advanced rules", bad)
	}
	dups := queryRows(t, h, sid, "SELECT count(*) - count(DISTINCT email) FROM public.mock_users")[0][0]
	if fmt.Sprint(dups) != "0" {
		t.Fatalf("%v duplicate emails", dups)
	}
}

func TestMockGenerateEmptyFKFails(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY)", parent))
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, pid INT NOT NULL REFERENCES public.%s(id))", child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,"seed":1`, child)))
	requireErrContains(t, body, code, 400, "no valid values")
}

func TestMockGenerateFKSourceOverride(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY)", parent))
	execSQL(t, h, sid, fmt.Sprintf("INSERT INTO public.%s (id) VALUES (101),(102),(103)", parent))
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, pid INT NOT NULL REFERENCES public.%s(id))", child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	// Explicit source pin (what the dialog's FK source picker sends).
	body := postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":20,"seed":2,`+
		`"fields":[{"column":"pid","generator":"foreign_key",`+
		`"params":{"ref_schema":"public","ref_table":%q,"ref_column":"id"}}]`, child, parent))
	code, resp := callPOST(t, h.MockGenerate, "/api/mock-data/generate", body)
	requireStatus(t, resp, code, 200)
	if out := decodeObj(t, resp); out["inserted"] != float64(20) {
		t.Fatalf("bad insert count in %s", resp)
	}
	stray := queryRows(t, h, sid, fmt.Sprintf("SELECT count(*) FROM public.%s WHERE pid NOT IN (101,102,103)", child))[0][0]
	if fmt.Sprint(stray) != "0" {
		t.Fatalf("%v rows outside the pinned source pool", stray)
	}
	// Unknown source table errors clearly (identifiers stay sanitized).
	badBody := postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
		`"fields":[{"column":"pid","generator":"foreign_key",`+
		`"params":{"ref_schema":"public","ref_table":"nope_missing","ref_column":"id"}}]`, child))
	code, resp = callPOST(t, h.MockGenerate, "/api/mock-data/generate", badBody)
	requireErrContains(t, resp, code, 400, "foreign key pid")
}

func TestMockPreviewUnsupportedCheckWarns(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id SERIAL PRIMARY KEY, name TEXT NOT NULL CHECK (length(name) > 2))`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5`, tbl)))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	warns, _ := out["warnings"].([]any)
	if len(warns) == 0 {
		t.Fatalf("want unsupported-CHECK warning in %s", body)
	}
}

func TestMockGenerateLimits(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":20001`))
	requireErrContains(t, body, code, 400, "max 20000")
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"weird","count":5`))
	requireErrContains(t, body, code, 400, "unknown mode")
}
