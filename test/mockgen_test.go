package test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"pglight/internal/mockgen"
)

func strp(s string) *string { return &s }

func simpleMeta() mockgen.TableMeta {
	return mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{
			{Name: "id", DataType: "bigint", Udt: "int8", Nullable: false, Default: strp("nextval('t_id_seq'::regclass)"), PrimaryKey: true, Unique: true},
			{Name: "code", DataType: "integer", Udt: "int4", Nullable: false},
			{Name: "price", DataType: "numeric", Udt: "numeric", Nullable: false},
			{Name: "ratio", DataType: "double precision", Udt: "float8", Nullable: true},
			{Name: "title", DataType: "text", Udt: "text", Nullable: false},
			{Name: "active", DataType: "boolean", Udt: "bool", Nullable: false},
			{Name: "day", DataType: "date", Udt: "date", Nullable: true},
			{Name: "ts", DataType: "timestamp with time zone", Udt: "timestamptz", Nullable: false},
			{Name: "uid", DataType: "uuid", Udt: "uuid", Nullable: false},
			{Name: "meta", DataType: "jsonb", Udt: "jsonb", Nullable: true},
			{Name: "blob", DataType: "bytea", Udt: "bytea", Nullable: true},
			{Name: "mood", DataType: "USER-DEFINED", Udt: "mood", Nullable: false, EnumValues: []string{"happy", "neutral", "sad"}},
			{Name: "tags", DataType: "ARRAY", Udt: "_text", Nullable: true},
			{Name: "created", DataType: "timestamp with time zone", Udt: "timestamptz", Nullable: false, Default: strp("now()")},
		},
	}
}

func genSimple(t *testing.T, meta mockgen.TableMeta, seed int64, n int) ([][]any, *mockgen.Plan) {
	t.Helper()
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "simple", Count: n, Seed: seed, HasSeed: true})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	rows, err := plan.GenerateRows(seed, n, nil)
	if err != nil {
		t.Fatalf("GenerateRows: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("got %d rows want %d", len(rows), n)
	}
	return rows, plan
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestMockgenSeedDeterministic(t *testing.T) {
	meta := simpleMeta()
	a, _ := genSimple(t, meta, 42, 20)
	b, _ := genSimple(t, meta, 42, 20)
	requireDeep(t, "seed", "rows", rowsToStr(a), rowsToStr(b))
	c, _ := genSimple(t, meta, 43, 20)
	if fmt.Sprint(rowsToStr(a)) == fmt.Sprint(rowsToStr(c)) {
		t.Fatal("different seeds produced identical output")
	}
}

func rowsToStr(rows [][]any) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = make([]string, len(r))
		for j, v := range r {
			out[i][j] = fmt.Sprintf("%v|%T", v, v)
		}
	}
	return out
}

func TestMockgenSimpleTypes(t *testing.T) {
	rows, plan := genSimple(t, simpleMeta(), 7, 30)
	idx := map[string]int{}
	for i, c := range plan.InsertCols {
		idx[c] = i
	}
	for _, r := range rows {
		if v := r[idx["code"]].(int64); v < 0 || v > 10000 {
			t.Fatalf("integer out of range: %d", v)
		}
		ps := fmt.Sprint(r[idx["price"]])
		f, err := strconv.ParseFloat(ps, 64)
		if err != nil || f < 0 || f > 10000 {
			t.Fatalf("decimal invalid: %v", r[idx["price"]])
		}
		if parts := strings.Split(ps, "."); len(parts) != 2 || len(parts[1]) != 2 {
			t.Fatalf("decimal scale != 2: %q", ps)
		}
		ts := fmt.Sprint(r[idx["title"]])
		if len(ts) < 8 || len(ts) > 24 {
			t.Fatalf("text length out of range: %q", ts)
		}
		if _, ok := r[idx["active"]].(bool); !ok {
			t.Fatalf("bool not bool: %T", r[idx["active"]])
		}
		if !uuidRe.MatchString(fmt.Sprint(r[idx["uid"]])) {
			t.Fatalf("uuid invalid: %v", r[idx["uid"]])
		}
		switch m := r[idx["mood"]].(string); m {
		case "happy", "neutral", "sad":
		default:
			t.Fatalf("enum invalid: %q", m)
		}
		if _, ok := r[idx["tags"]].([]string); !ok {
			t.Fatalf("array not []string: %T", r[idx["tags"]])
		}
		if _, ok := r[idx["meta"]].(string); !ok {
			t.Fatalf("jsonb not string: %T", r[idx["meta"]])
		}
		if _, ok := r[idx["blob"]].([]byte); !ok {
			t.Fatalf("bytea not []byte: %T", r[idx["blob"]])
		}
	}
}

func TestMockgenSimpleOmitsIdentityDefault(t *testing.T) {
	def := "nextval('x'::regclass)"
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "id", DataType: "bigint", Udt: "int8", Default: &def, Identity: true},
		{Name: "gen", DataType: "text", Udt: "text", Generated: true},
		{Name: "serial", DataType: "integer", Udt: "int4", Default: strp("nextval('s'::regclass)")},
		{Name: "ts", DataType: "timestamp with time zone", Udt: "timestamptz", Default: strp("now()")},
		{Name: "name", DataType: "text", Udt: "text"},
	}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "simple", Count: 5})
	if err != nil {
		t.Fatal(err)
	}
	requireDeep(t, "insert", "cols", plan.InsertCols, []string{"name"})
	requireDeep(t, "omitted", "cols", sortedStrs(plan.Omitted), sortedStrs([]string{"id", "gen", "serial", "ts"}))
	rows, err := plan.GenerateRows(1, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0]) != 1 {
		t.Fatalf("row width %d want 1", len(rows[0]))
	}
	// Preview renders omitted columns as the database-default placeholder.
	pv := plan.PreviewRow(rows[0])
	if pv[0] != "<database default>" || pv[4] == "<database default>" {
		t.Fatalf("preview placeholders wrong: %v", pv)
	}
}

func TestMockgenUnique(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "email", DataType: "text", Udt: "text", Nullable: false, Unique: true},
	}}
	req := mockgen.Request{Mode: "advanced", Count: 100, Seed: 3, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "email", Generator: "email", Unique: true}}}
	plan, err := mockgen.BuildPlan(meta, req)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(3, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		k := fmt.Sprint(r[0])
		if seen[k] {
			t.Fatalf("duplicate unique value %q", k)
		}
		seen[k] = true
	}
	// Impossible domain: 5 unique values out of 1.
	req2 := mockgen.Request{Mode: "advanced", Count: 5, Seed: 3, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "email", Generator: "choice",
			Params: map[string]any{"values": []any{"only"}}, Unique: true}}}
	plan2, err := mockgen.BuildPlan(meta, req2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan2.GenerateRows(3, 5, nil)
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("want uniqueness failure, got %v", err)
	}
}

func TestMockgenRelativeDatetime(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "created_at", DataType: "timestamp with time zone", Udt: "timestamptz", Nullable: false},
		{Name: "updated_at", DataType: "timestamp with time zone", Udt: "timestamptz", Nullable: false},
	}}
	req := mockgen.Request{Mode: "advanced", Count: 50, Seed: 9, HasSeed: true,
		Fields: []mockgen.FieldSpec{
			{Column: "created_at", Generator: "datetime"},
			{Column: "updated_at", Generator: "relative_datetime",
				Params: map[string]any{"source": "created_at", "min_offset_days": float64(0), "max_offset_days": float64(30)}},
		},
		Constraints: []mockgen.Constraint{{Kind: "compare",
			Left:     mockgen.ConstraintSide{Field: "created_at"},
			Operator: "<=", Right: mockgen.ConstraintSide{Field: "updated_at"}}}}
	plan, err := mockgen.BuildPlan(meta, req)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(9, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		m := map[string]any{"created_at": r[0], "updated_at": r[1]}
		if err := mockgen.ValidateRow(m, req.Constraints); err != nil {
			t.Fatalf("row %d violates created_at <= updated_at: %v", i, err)
		}
	}
}

func TestMockgenCycleDetected(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "a", DataType: "timestamp with time zone", Udt: "timestamptz"},
		{Name: "b", DataType: "timestamp with time zone", Udt: "timestamptz"},
	}}
	req := mockgen.Request{Mode: "advanced", Count: 5,
		Fields: []mockgen.FieldSpec{
			{Column: "a", Generator: "relative_datetime", Params: map[string]any{"source": "b"}},
			{Column: "b", Generator: "relative_datetime", Params: map[string]any{"source": "a"}},
		}}
	_, err := mockgen.BuildPlan(meta, req)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle error, got %v", err)
	}
}

func TestMockgenFK(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "company_id", DataType: "bigint", Udt: "int8", Nullable: false},
	}, ForeignKeys: []mockgen.ForeignKeyMeta{
		{Name: "fk_c", Column: "company_id", RefSchema: "public", RefTable: "companies", RefColumn: "id"},
	}}
	req := mockgen.Request{Mode: "advanced", Count: 10, Seed: 1, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "company_id", Generator: "foreign_key"}}}
	plan, err := mockgen.BuildPlan(meta, req)
	if err != nil {
		t.Fatal(err)
	}
	pool := map[string][]any{"company_id": {int64(7), int64(8)}}
	rows, err := plan.GenerateRows(1, 10, pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if v := r[0]; v != int64(7) && v != int64(8) {
			t.Fatalf("FK value outside pool: %v", v)
		}
	}
	// Empty pool on a NOT NULL FK fails instead of inventing values.
	if _, err := plan.GenerateRows(1, 3, map[string][]any{}); err == nil {
		t.Fatal("want empty-FK failure")
	}
}

func TestMockgenCheckHints(t *testing.T) {
	if _, k, ok := mockgen.ParseCheckHint("CHECK ((age >= 18))"); !ok || k != "comparison" {
		t.Fatalf("comparison not parsed: %v %v", k, ok)
	}
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((age BETWEEN 18 AND 100))"); !ok || k != "between" || *h.Min != 18 || *h.Max != 100 {
		t.Fatalf("between not parsed: %+v %v %v", h, k, ok)
	}
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((status = ANY (ARRAY['a', 'b'])))"); !ok || k != "in" || len(h.Values) != 2 {
		t.Fatalf("ANY not parsed: %+v %v %v", h, k, ok)
	}
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((status IN ('a', 'b')))"); !ok || k != "in" || len(h.Values) != 2 {
		t.Fatalf("IN not parsed: %+v %v %v", h, k, ok)
	}
	// PostgreSQL-normalized spellings.
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((age >= 18) AND (age <= 100))"); !ok || k != "between" || *h.Min != 18 || *h.Max != 100 {
		t.Fatalf("AND-range not parsed: %+v %v %v", h, k, ok)
	}
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text])))"); !ok || k != "in" || len(h.Values) != 2 {
		t.Fatalf("ANY-ARRAY not parsed: %+v %v %v", h, k, ok)
	}
	if _, k, ok := mockgen.ParseCheckHint("CHECK ((created_at <= updated_at))"); ok || k != "unsupported" {
		t.Fatalf("cross-column should stay unsupported: %v %v", k, ok)
	}
	if _, k, ok := mockgen.ParseCheckHint("CHECK (custom_function(a, b))"); ok || k != "unsupported" {
		t.Fatalf("unsupported should not parse: %v %v", k, ok)
	}
	for _, op := range []string{"=", "!=", "<", "<=", ">", ">="} {
		m := map[string]any{"a": int64(5), "b": int64(5)}
		c := []mockgen.Constraint{{Kind: "compare",
			Left: mockgen.ConstraintSide{Field: "a"}, Operator: op, Right: mockgen.ConstraintSide{Field: "b"}}}
		err := mockgen.ValidateRow(m, c)
		want := op == "=" || op == "<=" || op == ">="
		if (err == nil) != want {
			t.Fatalf("op %s on 5,5: err=%v want ok=%v", op, err, want)
		}
	}
}
