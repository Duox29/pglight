package test

// Regression coverage for the mock-data correctness fixes: conservative
// CHECK inference, server-side param validation, composite FK tuples,
// gapless sequence/FK counters on retry, decimal/IN hint consumption and
// default-only tables. Engine-only (no database needed).

import (
	"fmt"
	"strconv"
	"testing"

	"pglight/internal/mockgen"
)

func TestCheckHintRejectsOr(t *testing.T) {
	// CHECK (x < 0 OR x > 100) must NOT fold to a range: the old parser
	// derived min=101/max=-1 and generated mostly-violating rows.
	if _, _, ok := mockgen.ParseCheckHint("CHECK ((x < 0 OR x > 100))"); ok {
		t.Fatal("OR check must be unsupported")
	}
	if _, _, ok := mockgen.ParseCheckHint("CHECK ((NOT (x > 5)))"); ok {
		t.Fatal("NOT check must be unsupported")
	}
	if _, _, ok := mockgen.ParseCheckHint("CHECK ((CASE WHEN x > 0 THEN 1 ELSE 0 END = 1))"); ok {
		t.Fatal("CASE check must be unsupported")
	}
	if _, _, ok := mockgen.ParseCheckHint("CHECK ((a > 1 AND b < 2))"); ok {
		t.Fatal("multi-column comparison must be unsupported")
	}
	if _, _, ok := mockgen.ParseCheckHint("CHECK ((length(name) > 3))"); ok {
		t.Fatal("function check must be unsupported")
	}
	// Same-column AND ranges stay supported.
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((age >= 18 AND age <= 100))"); !ok || k != "between" || *h.Min != 18 || *h.Max != 100 {
		t.Fatalf("same-column range broke: %+v %q %v", h, k, ok)
	}
}

func TestCheckEqualityIntersectsExistingBounds(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "id", DataType: "integer", Udt: "int4"}}}
	for _, def := range []string{
		"CHECK (id >= 10 AND id = 5)",
		"CHECK (id = 5 AND id >= 10)",
		"CHECK (id <= 3 AND id = 5)",
		"CHECK (id = 5 AND id <= 3)",
	} {
		meta.Checks = []mockgen.CheckMeta{{Definition: def}}
		if _, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1}); err == nil {
			t.Fatalf("%s should be rejected as an inconsistent CHECK range", def)
		}
	}

	meta.Checks = []mockgen.CheckMeta{{Definition: "CHECK (id >= 1 AND id = 5 AND id <= 10)"}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 100, Seed: 1, HasSeed: true})
	if err != nil {
		t.Fatalf("valid equality intersection rejected: %v", err)
	}
	rows, err := plan.GenerateRows(1, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if got := row[0].(int64); got != 5 {
			t.Fatalf("CHECK equality generated %d, want 5", got)
		}
	}
}

func TestCheckHintForColumnsCrossColumn(t *testing.T) {
	cols := []string{"age", "enabled"}
	// Partially parseable: the regex sees age > 18 but must not silently
	// drop the enabled clause when column names are known.
	if _, _, ok := mockgen.ParseCheckHintForColumns("CHECK ((age > 18 AND enabled = true))", cols); ok {
		t.Fatal("cross-column check must be unsupported with column names")
	}
	// Single-column hints survive the stricter entry point.
	if h, _, ok := mockgen.ParseCheckHintForColumns("CHECK ((age >= 18))", cols); !ok || h.Column != "age" {
		t.Fatalf("single-column hint broke: %+v %v", h, ok)
	}
	// String literals mentioning other columns don't veto: 'enabled' the
	// value is not the enabled column.
	if _, _, ok := mockgen.ParseCheckHintForColumns("CHECK ((status IN ('enabled','off')))", []string{"status", "enabled"}); !ok {
		t.Fatal("quoted literal must not count as a column mention")
	}
}

func TestAdvancedParamValidation(t *testing.T) {
	arrayMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "a", DataType: "ARRAY", Udt: "_text"},
	}}
	textMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "v", DataType: "text", Udt: "text"},
	}}
	tsMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "v", DataType: "timestamp", Udt: "timestamp"},
		{Name: "w", DataType: "timestamp", Udt: "timestamp"},
	}}
	build := func(m mockgen.TableMeta, fields []mockgen.FieldSpec) error {
		_, err := mockgen.BuildPlan(m, mockgen.Request{Mode: "advanced", Count: 1, Fields: fields})
		return err
	}
	field := func(col, gen string, params map[string]any) []mockgen.FieldSpec {
		return []mockgen.FieldSpec{{Column: col, Generator: gen, Params: params}}
	}
	bigVals := make([]any, 1001)
	cases := []struct {
		name   string
		meta   mockgen.TableMeta
		fields []mockgen.FieldSpec
	}{
		{"neg min_items", arrayMeta, field("a", "array", map[string]any{"min_items": -1, "max_items": -1})},
		{"huge max_items", arrayMeta, field("a", "array", map[string]any{"max_items": 1000000000})},
		{"huge max_length", textMeta, field("v", "string", map[string]any{"max_length": 1000000000})},
		{"neg max_length", textMeta, field("v", "string", map[string]any{"max_length": -5})},
		{"min over max", textMeta, field("v", "string", map[string]any{"min_length": 9, "max_length": 3})},
		{"bad true_prob", textMeta, field("v", "boolean", map[string]any{"true_probability": 2})},
		{"bad integer min", textMeta, field("v", "integer", map[string]any{"min": "nope"})},
		{"huge offset", tsMeta, []mockgen.FieldSpec{
			{Column: "v", Generator: "datetime"},
			{Column: "w", Generator: "relative_datetime", Params: map[string]any{"source": "v", "max_offset_days": 1e12}},
		}},
		{"choice overflow", textMeta, field("v", "choice", map[string]any{"values": bigVals})},
		{"too many constraints", textMeta, nil},
	}
	for _, tc := range cases {
		var err error
		if tc.name == "too many constraints" {
			cons := make([]mockgen.Constraint, 65)
			for i := range cons {
				cons[i] = mockgen.Constraint{Kind: "compare",
					Left:     mockgen.ConstraintSide{Field: "v"},
					Operator: "=",
					Right:    mockgen.ConstraintSide{Value: "x"}}
			}
			_, err = mockgen.BuildPlan(tc.meta, mockgen.Request{Mode: "advanced", Count: 1, Constraints: cons})
		} else {
			err = build(tc.meta, tc.fields)
		}
		if err == nil {
			t.Fatalf("%s should fail validation", tc.name)
		}
	}
}

func TestCompositeFKTupleCoherence(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{
			{Name: "country", DataType: "text", Udt: "text"},
			{Name: "customer_id", DataType: "integer", Udt: "int4"},
		},
		ForeignKeys: []mockgen.ForeignKeyMeta{
			{Name: "fk_c", Column: "country", RefSchema: "public", RefTable: "parent", RefColumn: "country"},
			{Name: "fk_c", Column: "customer_id", RefSchema: "public", RefTable: "parent", RefColumn: "customer_id"},
		},
	}
	if groups := meta.FKGroups(); len(groups) != 1 || len(groups[0].Local) != 2 {
		t.Fatalf("want 1 composite group, got %+v", groups)
	}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 200, Seed: 9, HasSeed: true})
	if err != nil {
		t.Fatal(err)
	}
	// Parent tuples chosen so per-column mixing is observable: only
	// (US,1) and (JP,2) exist — (US,2)/(JP,1) must never appear.
	plan.FKTuples = map[string][][]any{"fk_c": {{"US", int64(1)}, {"JP", int64(2)}}}
	rows, err := plan.GenerateRows(9, 200, map[string][]any{})
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"US|1": true, "JP|2": true}
	for i, r := range rows {
		key := fmt.Sprint(r[0]) + "|" + fmt.Sprint(r[1])
		if !allowed[key] {
			t.Fatalf("row %d mixed composite tuple: %v", i, r)
		}
	}
}

func TestSequenceNoGapsOnRetry(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "s", DataType: "bigint", Udt: "int8"},
		{Name: "u", DataType: "text", Udt: "text"},
	}}
	fields := []mockgen.FieldSpec{
		{Column: "s", Generator: "sequence", Params: map[string]any{"start": 1, "step": 1}},
		{Column: "u", Generator: "choice", Params: map[string]any{"values": []any{"a", "b"}}, Unique: true},
	}
	// u has a 2-value UNIQUE domain over 2 rows, so about half the seeds
	// force row 2 through a failed attempt. Across many seeds at least one
	// retries — and s must be exactly 1,2 every time (no gaps consumed by
	// discarded attempts).
	for seed := int64(1); seed <= 50; seed++ {
		plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 2, Seed: seed, HasSeed: true, Fields: fields})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := plan.GenerateRows(seed, 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		si := colIdx(plan)["s"]
		if got := []string{fmt.Sprint(rows[0][si]), fmt.Sprint(rows[1][si])}; got[0] != "1" || got[1] != "2" {
			t.Fatalf("seed %d: sequence gap after retry: %v", seed, got)
		}
	}
}

func TestSequentialFKNoGapsOnRetry(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "f", DataType: "text", Udt: "text"},
		{Name: "u", DataType: "text", Udt: "text"},
	}}
	fields := []mockgen.FieldSpec{
		{Column: "f", Generator: "foreign_key", Params: map[string]any{"mode": "sequential"}},
		{Column: "u", Generator: "choice", Params: map[string]any{"values": []any{"a", "b", "c"}}, Unique: true},
	}
	fk := map[string][]any{"f": {"p1", "p2"}}
	for seed := int64(1); seed <= 50; seed++ {
		plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3, Seed: seed, HasSeed: true, Fields: fields})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := plan.GenerateRows(seed, 3, fk)
		if err != nil {
			t.Fatal(err)
		}
		fi := colIdx(plan)["f"]
		got := []string{fmt.Sprint(rows[0][fi]), fmt.Sprint(rows[1][fi]), fmt.Sprint(rows[2][fi])}
		if got[0] != "p1" || got[1] != "p2" || got[2] != "p1" {
			t.Fatalf("seed %d: sequential FK gap after retry: %v", seed, got)
		}
		// u must still be unique (retries, not silent dupes).
		seen := map[string]bool{}
		ui := colIdx(plan)["u"]
		for _, r := range rows {
			k := fmt.Sprint(r[ui])
			if seen[k] {
				t.Fatalf("seed %d: duplicate unique value %q", seed, k)
			}
			seen[k] = true
		}
	}
}

func TestDecimalCheckRangeHonored(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{{Name: "price", DataType: "numeric", Udt: "numeric"}},
		Checks:  []mockgen.CheckMeta{{Name: "c", Definition: "CHECK ((price >= 1 AND price <= 10))"}},
	}
	rows, _ := genAdvanced(t, meta, 13, 50, []mockgen.FieldSpec{{Column: "price", Generator: "auto"}}, nil, nil)
	for i, r := range rows {
		f, err := strconv.ParseFloat(fmt.Sprint(r[0]), 64)
		if err != nil || f < 1 || f > 10 {
			t.Fatalf("row %d decimal %v outside CHECK 1..10", i, r[0])
		}
	}
}

func TestIntegerINHintUsed(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{{Name: "n", DataType: "integer", Udt: "int4"}},
		Checks:  []mockgen.CheckMeta{{Name: "c", Definition: "CHECK ((n IN (4, 5, 6)))"}},
	}
	rows, _ := genAdvanced(t, meta, 14, 50, []mockgen.FieldSpec{{Column: "n", Generator: "auto"}}, nil, nil)
	for i, r := range rows {
		if s := fmt.Sprint(r[0]); s != "4" && s != "5" && s != "6" {
			t.Fatalf("row %d integer %v not in IN set", i, r[0])
		}
	}
}

func TestDefaultOnlyPlanRows(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "id", DataType: "bigint", Udt: "int8", Default: strp("nextval('x'::regclass)")},
		{Name: "ts", DataType: "timestamp", Udt: "timestamptz", Default: strp("now()")},
	}}
	for _, mode := range []string{"simple", "advanced"} {
		plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: mode, Count: 3})
		if err != nil {
			t.Fatalf("%s default-only should plan: %v", mode, err)
		}
		if len(plan.InsertCols) != 0 {
			t.Fatalf("%s default-only should insert no columns: %v", mode, plan.InsertCols)
		}
		rows, err := plan.GenerateRows(15, 3, nil)
		if err != nil || len(rows) != 3 {
			t.Fatalf("%s default-only rows: %v %v", mode, rows, err)
		}
		if len(plan.PreviewRow(rows[0])) != 2 {
			t.Fatalf("%s preview should span all columns", mode)
		}
	}
}
