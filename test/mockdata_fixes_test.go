package test

// DB-backed regression coverage for the mock-data correctness fixes:
// composite UNIQUE/partial-index metadata, composite FK generation and
// default-only table generation.

import (
	"fmt"
	"testing"
)

func TestMockMetaCompositeUnique(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		a BOOLEAN NOT NULL, b UUID NOT NULL, c TEXT NOT NULL,
		UNIQUE (a, b), UNIQUE (c))`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	byName := mockColsByName(t, body)
	// Composite UNIQUE(a,b) must NOT mark members individually unique:
	// BOOLEAN a could never satisfy >2 rows otherwise.
	requireDeep(t, body, "a.unique", byName["a"]["unique"], false)
	requireDeep(t, body, "b.unique", byName["b"]["unique"], false)
	// Single-column UNIQUE(c) still flags c.
	requireDeep(t, body, "c.unique", byName["c"]["unique"], true)
}

func TestMockMetaPartialAndIncludeIndex(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		email TEXT NOT NULL, name TEXT NOT NULL, deleted_at TIMESTAMPTZ)`, tbl))
	execSQL(t, h, sid, fmt.Sprintf(
		"CREATE UNIQUE INDEX %s_partial ON public.%s(email) WHERE deleted_at IS NULL", tbl, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	// Partial index holds only within its predicate — not globally unique.
	requireDeep(t, body, "email.unique-partial", mockColsByName(t, body)["email"]["unique"], false)

	tbl2 := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (email TEXT NOT NULL, name TEXT NOT NULL)`, tbl2))
	execSQL(t, h, sid, fmt.Sprintf(
		"CREATE UNIQUE INDEX %s_inc ON public.%s(email) INCLUDE (name)", tbl2, tbl2))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl2) })
	code, body = callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+tbl2))
	requireStatus(t, body, code, 200)
	// Single-key unique index with INCLUDE payload is still unique.
	requireDeep(t, body, "email.unique-include", mockColsByName(t, body)["email"]["unique"], true)
}

func TestMockGenerateCompositeFK(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		country TEXT NOT NULL, customer_id INT NOT NULL,
		PRIMARY KEY (country, customer_id))`, parent))
	execSQL(t, h, sid, fmt.Sprintf(
		"INSERT INTO public.%s VALUES ('US',1),('JP',2)", parent))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id SERIAL PRIMARY KEY, country TEXT NOT NULL, customer_id INT NOT NULL,
		FOREIGN KEY (country, customer_id) REFERENCES public.%s(country, customer_id))`, child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	// Composite members land in one FK group in metadata.
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+child))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	fks, ok := out["foreign_keys"].([]any)
	if !ok || len(fks) != 2 {
		t.Fatalf("want 2 FK members, got %s", body)
	}
	// Advanced auto-generation must only emit existing parent tuples:
	// (US,2)/(JP,1) mixing would violate the FK.
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":50,"seed":42`, child)))
	requireStatus(t, body, code, 200)
	got := queryRows(t, h, sid, fmt.Sprintf("SELECT country, customer_id::text FROM public.%s", child))
	if len(got) != 50 {
		t.Fatalf("want 50 rows, got %d", len(got))
	}
	allowed := map[string]bool{"US|1": true, "JP|2": true}
	for i, r := range got {
		key := fmt.Sprint(r[0]) + "|" + fmt.Sprint(r[1])
		if !allowed[key] {
			t.Fatalf("row %d mixed composite tuple %v", i, r)
		}
	}
}

func TestMockGenerateDefaultOnly(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ DEFAULT now())`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	for _, mode := range []string{"simple", "advanced"} {
		execSQL(t, h, sid, "DELETE FROM public."+tbl)
		code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
			postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":%q,"count":5,"seed":7`, tbl, mode)))
		requireStatus(t, body, code, 200)
		n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
		if fmt.Sprint(n) != "5" {
			t.Fatalf("%s default-only generated %v rows, want 5 (%s)", mode, n, body)
		}
	}
}

func TestMockGenerateRejectsDangerousParams(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (a TEXT[] NOT NULL, v TEXT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	bad := []string{
		`"column":"a","generator":"array","params":{"min_items":-1,"max_items":-1}`,
		`"column":"a","generator":"array","params":{"max_items":1000000000}`,
		`"column":"v","generator":"string","params":{"max_length":1000000000}`,
	}
	for _, f := range bad {
		code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
			postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":3,"fields":[{%s}]`, tbl, f)))
		requireErrContains(t, body, code, 400, "column")
	}
	// The table stays empty: validation fails before any insert.
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("invalid params inserted %v rows", n)
	}
}
