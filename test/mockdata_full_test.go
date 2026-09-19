package test

// Full endpoint coverage for /api/mock-data/*: exact metadata shapes,
// request validation edges, deterministic behavior, txn-awareness and
// end-to-end CHECK/FK inference through real PostgreSQL.

import (
	"fmt"
	"strings"
	"testing"
)

func mockColsByName(t *testing.T, body string) map[string]map[string]any {
	t.Helper()
	out := decodeObj(t, body)
	cols, ok := out["columns"].([]any)
	if !ok {
		t.Fatalf("missing columns in %s", body)
	}
	byName := map[string]map[string]any{}
	for _, c := range cols {
		m, ok := c.(map[string]any)
		if !ok {
			t.Fatalf("column not an object in %s", body)
		}
		byName[m["name"].(string)] = m
	}
	return byName
}

func TestMockMetaColumnKeys(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table=mock_users"))
	requireStatus(t, body, code, 200)
	byName := mockColsByName(t, body)
	wantKeys := []string{"name", "data_type", "udt", "nullable", "default", "identity",
		"generated", "primary_key", "unique", "enum_values", "semantic_hint"}
	for _, c := range byName {
		requireKeys(t, "column", c, wantKeys...)
	}
	// mock_users.id: serial default, PK, non-nullable, no semantic hint.
	id := byName["id"]
	requireDeep(t, body, "id.pk", id["primary_key"], true)
	requireDeep(t, body, "id.unique", id["unique"], true)
	requireDeep(t, body, "id.nullable", id["nullable"], false)
	requireDeep(t, body, "id.hint", id["semantic_hint"], "")
	if id["default"] == nil || !strings.Contains(fmt.Sprint(id["default"]), "nextval") {
		t.Fatalf("id default should be nextval: %v", id)
	}
	// created_at/updated_at carry the datetime semantic hint.
	requireDeep(t, body, "created.hint", byName["created_at"]["semantic_hint"], "datetime")
	requireDeep(t, body, "age.hint", byName["age"]["semantic_hint"], "")
	requireDeep(t, body, "status.nullable", byName["status"]["nullable"], false)
}

func TestMockMetaRichTable(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, code TEXT NOT NULL)", parent))
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		code TEXT NOT NULL DEFAULT 'x',
		slug TEXT UNIQUE,
		tag TEXT,
		pid INT REFERENCES public.%s(id),
		gen TEXT GENERATED ALWAYS AS (code || '-g') STORED)`, child, parent))
	execSQL(t, h, sid, fmt.Sprintf("CREATE UNIQUE INDEX %s_tag_uidx ON public.%s(tag)", child, child))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+child))
	requireStatus(t, body, code, 200)
	byName := mockColsByName(t, body)
	if len(byName) != 6 {
		t.Fatalf("want 6 columns, got %v", body)
	}
	requireDeep(t, body, "id.identity", byName["id"]["identity"], true)
	requireDeep(t, body, "id.pk", byName["id"]["primary_key"], true)
	requireDeep(t, body, "gen.generated", byName["gen"]["generated"], true)
	if def := fmt.Sprint(byName["code"]["default"]); !strings.Contains(def, "'x'") {
		t.Fatalf("code default should mention 'x': %q", def)
	}
	requireDeep(t, body, "slug.unique", byName["slug"]["unique"], true)
	requireDeep(t, body, "tag.unique-index", byName["tag"]["unique"], true)
	requireDeep(t, body, "code.unique", byName["code"]["unique"], false)
	requireDeep(t, body, "pid.nullable", byName["pid"]["nullable"], true)
	out := decodeObj(t, body)
	fks, ok := out["foreign_keys"].([]any)
	if !ok || len(fks) != 1 {
		t.Fatalf("want exactly 1 FK, got %s", body)
	}
	fk := fks[0].(map[string]any)
	requireKeys(t, "fk", fk, "name", "column", "ref_schema", "ref_table", "ref_column")
	requireDeep(t, body, "fk.column", fk["column"], "pid")
	requireDeep(t, body, "fk.ref", []any{fk["ref_schema"], fk["ref_table"], fk["ref_column"]},
		[]any{"public", parent, "id"})
}

func TestMockMetaEdges(t *testing.T) {
	h, sid := newHandler(t)
	// Schema defaults to public.
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?table=mock_users"))
	requireStatus(t, body, code, 200)
	if len(mockColsByName(t, body)) != 6 {
		t.Fatalf("default schema should resolve public: %s", body)
	}
	// Missing table param.
	code, body = callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public"))
	requireErrContains(t, body, code, 400, "table required")
	// Ghost schema.
	code, body = callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=nope&table=mock_users"))
	requireErrContains(t, body, code, 400, "not found")
}

func TestMockPreviewCountEdges(t *testing.T) {
	h, sid := newHandler(t)
	prev := func(count string) (int, string) {
		inner := `"schema":"public","table":"mock_users","mode":"simple","seed":1`
		if count != "" {
			inner += `,"count":` + count
		}
		return callPOST(t, h.MockPreview, "/api/mock-data/preview", postBody(sid, inner))
	}
	// Missing/zero/negative count defaults to 20 rows.
	for _, c := range []string{"", "0", "-3"} {
		code, body := prev(c)
		requireStatus(t, body, code, 200)
		if n := len(decodeObj(t, body)["rows"].([]any)); n != 20 {
			t.Fatalf("count %q: want 20 rows, got %d", c, n)
		}
	}
	code, body := prev("100")
	requireStatus(t, body, code, 200)
	if n := len(decodeObj(t, body)["rows"].([]any)); n != 100 {
		t.Fatalf("want 100 rows, got %d", n)
	}
	code, body = prev("101")
	requireErrContains(t, body, code, 400, "at most 100")
}

func TestMockPreviewRequestErrors(t *testing.T) {
	h, sid := newHandler(t)
	// Invalid JSON.
	code, body := callPOST(t, h.MockPreview, "/api/mock-data/preview", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	// Ghost session.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		`{"session_id":"ghost","schema":"public","table":"mock_users","mode":"simple","count":5}`)
	requireStatus(t, body, code, 401)
	// Unknown mode / table.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"weird","count":5`))
	requireErrContains(t, body, code, 400, "unknown mode")
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"nope_missing","mode":"simple","count":5`))
	requireErrContains(t, body, code, 400, "not found")

	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		v INT NOT NULL, a TIMESTAMPTZ NOT NULL, b TIMESTAMPTZ NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	// Unknown field column.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"ghost","generator":"integer"}]`, tbl)))
	requireErrContains(t, body, code, 400, "unknown column")
	// NULL generator on NOT NULL.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"v","generator":"null"}]`, tbl)))
	requireErrContains(t, body, code, 400, "NOT NULL")
	// Bad constraint operator.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"constraints":[{"kind":"compare","left":{"field":"a"},"operator":"<=>","right":{"field":"b"}}]`, tbl)))
	requireErrContains(t, body, code, 400, "unsupported operator")
	// Relative generator without source.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"b","generator":"relative_datetime"}]`, tbl)))
	requireErrContains(t, body, code, 400, "params.source")
}

func TestMockPreviewShapeAndSeed(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":5,"seed":77`))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	requireKeys(t, "preview", out, "columns", "rows", "warnings", "seed", "in_txn")
	requireDeep(t, body, "preview.seed", out["seed"], 77)
	requireDeep(t, body, "preview.in_txn", out["in_txn"], false)
	if warns, ok := out["warnings"].([]any); !ok || len(warns) != 0 {
		t.Fatalf("simple preview warnings should be empty array: %s", body)
	}
	// Every row spans all table columns; the identity column previews as default.
	rows := out["rows"].([]any)
	cols := colsOf(t, body)
	if len(rows) != 5 || len(cols) != 6 {
		t.Fatalf("shape wrong: %d rows x %d cols", len(rows), len(cols))
	}
	for _, r := range rows {
		cells := r.([]any)
		if len(cells) != 6 {
			t.Fatalf("row width %d want 6", len(cells))
		}
		if cells[0] != "<database default>" {
			t.Fatalf("identity should preview as default: %v", cells)
		}
	}
	// No seed → server mints a numeric one.
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":2`))
	requireStatus(t, body, code, 200)
	if _, ok := decodeObj(t, body)["seed"].(float64); !ok {
		t.Fatalf("unseeded preview should mint a numeric seed: %s", body)
	}
}

func TestMockPreviewInTxn(t *testing.T) {
	h, sid := newHandler(t)
	before := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users")[0][0]
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":5,"seed":9`))
	requireStatus(t, body, code, 200)
	requireDeep(t, body, "in_txn", decodeObj(t, body)["in_txn"], true)
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	after := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users")[0][0]
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("preview in txn modified table: %v -> %v", before, after)
	}
}

func TestMockGenerateRequestErrors(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate", `{bad`)
	requireErrContains(t, body, code, 400, "invalid json")
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		`{"session_id":"ghost","schema":"public","table":"mock_users","mode":"simple","count":5}`)
	requireStatus(t, body, code, 401)
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, `"schema":"public","table":"mock_users","mode":"simple","count":0`))
	requireErrContains(t, body, code, 400, "count must be")
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, `"schema":"public","table":"nope_missing","mode":"simple","count":5`))
	requireErrContains(t, body, code, 400, "not found")

	// Missing mode defaults to simple; full success shape echoed back.
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (v INT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"count":7,"seed":3`, tbl)))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	requireKeys(t, "generate", out, "generated", "inserted", "seed", "duration_ms", "in_txn")
	requireDeep(t, body, "counts", []any{out["generated"], out["inserted"]}, []any{7, 7})
	requireDeep(t, body, "seed", out["seed"], 3)
	if _, ok := out["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms should be numeric: %s", body)
	}
	// Unseeded generate mints a seed.
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"count":2`, tbl)))
	requireStatus(t, body, code, 200)
	if _, ok := decodeObj(t, body)["seed"].(float64); !ok {
		t.Fatalf("unseeded generate should mint a seed: %s", body)
	}
}

func TestMockGenerateCaseInsensitive(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (v INT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"Advanced","count":10,`+
			`"fields":[{"column":"v","generator":"  Integer  ","params":{"min":1,"max":1}}]`, tbl)))
	requireStatus(t, body, code, 200)
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl+" WHERE v=1")[0][0]
	if fmt.Sprint(n) != "10" {
		t.Fatalf("case-insensitive generator failed: %v", n)
	}
}

// Advanced failures carry the raw PostgreSQL error with NO Simple hint.
func TestMockGenerateAdvancedErrorRaw(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id SERIAL PRIMARY KEY, email TEXT NOT NULL UNIQUE)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"email","generator":"constant","params":{"value":"dup@example.com"}}]`, tbl)))
	requireStatus(t, body, code, 400)
	if strings.Contains(body, "Use Advanced mode") {
		t.Fatalf("advanced errors must not carry the simple hint: %s", body)
	}
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("unique violation should roll back everything, left %v", n)
	}
}

func TestMockGenerateNullProbClamp(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (id SERIAL PRIMARY KEY, v TEXT)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	gen := func(np string, n int) {
		t.Helper()
		execSQL(t, h, sid, "DELETE FROM public."+tbl)
		code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
			postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":%d,`+
				`"fields":[{"column":"v","generator":"string","null_probability":%s}]`, tbl, n, np)))
		requireStatus(t, body, code, 200)
	}
	gen("5", 10) // clamped to 1 → all NULL
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl+" WHERE v IS NOT NULL")[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("np>1 should clamp to all-null, %v non-null", n)
	}
	gen("-2", 10) // clamped to 0 → none NULL
	n = queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl+" WHERE v IS NULL")[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("negative np should clamp to none-null, %v null", n)
	}
}

func TestMockGenerateSequenceValues(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (v BIGINT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"v","generator":"sequence","params":{"start":100,"step":10}}]`, tbl)))
	requireStatus(t, body, code, 200)
	got := queryRows(t, h, sid, "SELECT v::text FROM public."+tbl+" ORDER BY v")
	requireDeep(t, body, "sequence", normRows(got),
		[][]string{{"100"}, {"110"}, {"120"}, {"130"}, {"140"}})
}

func TestMockGenerateChoiceWeightsDB(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (s TEXT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":10,`+
			`"fields":[{"column":"s","generator":"choice","params":{"values":["only","never"],"weights":[1,0]}}]`, tbl)))
	requireStatus(t, body, code, 200)
	n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl+" WHERE s='only'")[0][0]
	if fmt.Sprint(n) != "10" {
		t.Fatalf("zero weight leaked through: %v/10 only", n)
	}
}

func TestMockGenerateFKSequentialDB(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY)", parent))
	for i := 0; i < 3; i++ {
		execSQL(t, h, sid, fmt.Sprintf("INSERT INTO public.%s DEFAULT VALUES", parent))
	}
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, pid INT NOT NULL REFERENCES public.%s(id))", child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	ids := queryRows(t, h, sid, "SELECT id::text FROM public."+parent+" ORDER BY id")
	if len(ids) != 3 {
		t.Fatalf("want 3 parents, got %v", ids)
	}
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,"seed":6,`+
			`"fields":[{"column":"pid","generator":"foreign_key","params":{"mode":"sequential"}}]`, child)))
	requireStatus(t, body, code, 200)
	got := queryRows(t, h, sid, "SELECT pid::text FROM public."+child+" ORDER BY id")
	want := [][]any{{ids[0][0]}, {ids[1][0]}, {ids[2][0]}, {ids[0][0]}, {ids[1][0]}}
	requireDeep(t, body, "round-robin", normRows(got), normRows(want))
}

func normRows(rows [][]any) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = make([]string, len(r))
		for j, v := range r {
			out[i][j] = fmt.Sprint(v)
		}
	}
	return out
}

func TestMockGenerateFKBadMode(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY)", parent))
	execSQL(t, h, sid, fmt.Sprintf("INSERT INTO public.%s DEFAULT VALUES", parent))
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, pid INT NOT NULL REFERENCES public.%s(id))", child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	pid := fmt.Sprint(queryRows(t, h, sid, "SELECT id FROM public."+parent)[0][0])
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,`+
			`"fields":[{"column":"pid","generator":"foreign_key","params":{"mode":"bogus"}}]`, child)))
	requireStatus(t, body, code, 200)
	n := queryRows(t, h, sid, fmt.Sprintf("SELECT count(*) FROM public.%s WHERE pid::text<>'%s'", child, pid))[0][0]
	if fmt.Sprint(n) != "0" {
		t.Fatalf("bad FK mode should fall back to uniform pool, stray=%v", n)
	}
}

// Full auto inference end-to-end with zero field config: enum + BETWEEN +
// existing-row FK all resolve without user input.
func TestMockGenerateReviewsAuto(t *testing.T) {
	h, sid := newHandler(t)
	maxID := queryRows(t, h, sid, "SELECT COALESCE(max(id),0) FROM public.reviews")[0][0]
	t.Cleanup(func() {
		execSQL(t, h, sid, fmt.Sprintf("DELETE FROM public.reviews WHERE id > %v", maxID))
	})
	bookIDs := map[string]bool{}
	for _, r := range queryRows(t, h, sid, "SELECT id::text FROM public.books") {
		bookIDs[fmt.Sprint(r[0])] = true
	}
	if len(bookIDs) == 0 {
		t.Fatal("seed books missing")
	}
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, `"schema":"public","table":"reviews","mode":"advanced","count":10,"seed":15`))
	requireStatus(t, body, code, 200)
	rows := queryRows(t, h, sid, fmt.Sprintf(
		"SELECT rating::text, feeling::text, book_id::text FROM public.reviews WHERE id > %v", maxID))
	if len(rows) != 10 {
		t.Fatalf("want 10 auto rows, got %d", len(rows))
	}
	for _, r := range rows {
		rating, feeling, book := fmt.Sprint(r[0]), fmt.Sprint(r[1]), fmt.Sprint(r[2])
		if rating < "1" || rating > "5" || len(rating) != 1 {
			t.Fatalf("rating outside inferred 1..5: %q", rating)
		}
		switch feeling {
		case "happy", "neutral", "sad":
		default:
			t.Fatalf("feeling outside enum: %q", feeling)
		}
		if !bookIDs[book] {
			t.Fatalf("book_id outside existing rows: %q", book)
		}
	}
}

// Partial field config: inferred age/status plus explicit relative datetime.
func TestMockGenerateCheckInferencePartial(t *testing.T) {
	h, sid := newHandler(t)
	execSQL(t, h, sid, "DELETE FROM public.mock_users")
	t.Cleanup(func() { execSQL(t, h, sid, "DELETE FROM public.mock_users") })
	body := postBody(sid, `"schema":"public","table":"mock_users","mode":"advanced","count":10,"seed":31,`+
		`"fields":[
			{"column":"created_at","generator":"datetime"},
			{"column":"updated_at","generator":"relative_datetime","params":{"source":"created_at"}}],`+
		`"constraints":[{"kind":"compare","left":{"field":"created_at"},"operator":"<=","right":{"field":"updated_at"}}]`)
	code, resp := callPOST(t, h.MockGenerate, "/api/mock-data/generate", body)
	requireStatus(t, resp, code, 200)
	bad := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users WHERE age NOT BETWEEN 18 AND 100 OR status NOT IN ('active','disabled') OR created_at > updated_at")[0][0]
	if fmt.Sprint(bad) != "0" {
		t.Fatalf("%v rows violate inferred rules", bad)
	}
	n := queryRows(t, h, sid, "SELECT count(*) FROM public.mock_users")[0][0]
	if fmt.Sprint(n) != "10" {
		t.Fatalf("want 10 rows, got %v", n)
	}
}

// FK pools observe the session's open transaction (uncommitted parents).
func TestMockGeneratePreviewTxnAwareness(t *testing.T) {
	h, sid := newHandler(t)
	parent := tempTable(t)
	child := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY)", parent))
	execSQL(t, h, sid, fmt.Sprintf("CREATE TABLE public.%s (id SERIAL PRIMARY KEY, pid INT NOT NULL REFERENCES public.%s(id))", child, parent))
	t.Cleanup(func() {
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+child)
		execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+parent)
	})
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	execSQL(t, h, sid, "INSERT INTO public."+parent+" DEFAULT VALUES")
	pid := fmt.Sprint(queryRows(t, h, sid, "SELECT id FROM public."+parent)[0][0])
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5,"seed":4`, child)))
	requireStatus(t, body, code, 200)
	cols := colsOf(t, body)
	pi := -1
	for i, c := range cols {
		if c == "pid" {
			pi = i
		}
	}
	if pi < 0 {
		t.Fatalf("preview missing pid column: %v", cols)
	}
	for _, r := range rowsOf(t, body) {
		if fmt.Sprint(r.([]any)[pi]) != pid {
			t.Fatalf("pool missed uncommitted parent: %v", r)
		}
	}
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	for _, tbl := range []string{parent, child} {
		n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]
		if fmt.Sprint(n) != "0" {
			t.Fatalf("%s not empty after rollback: %v", tbl, n)
		}
	}
}
