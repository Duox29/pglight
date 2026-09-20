package test

// Hardening regressions for the mock-data review round: adversarial CHECK
// fragments, exact integer handling, overflow-safe randoms/sequences,
// composite-FK unit rule, weight/caps normalization, json.Number literal
// comparison, and AddDate large offsets. Engine-only (no database).

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"pglight/internal/mockgen"
)

func TestCheckHintRejectsPartialPredicates(t *testing.T) {
	// Each expression pairs a supported fragment with an unsupported
	// same-column predicate. Substring matching used to return the fragment
	// and generate rows PostgreSQL rejects (e.g. x=5 against x<>5).
	for _, def := range []string{
		"CHECK ((x BETWEEN 1 AND 10 AND x <> 5))",
		"CHECK ((x IN (1, 2) AND x > 1))",
		"CHECK ((x >= 0 AND x % 2 = 0))",
		"CHECK ((x >= 0 AND x != 5))",
		"CHECK ((x BETWEEN 1 AND 10 AND x != 5))",
	} {
		if _, _, ok := mockgen.ParseCheckHint(def); ok {
			t.Fatalf("%s must be unsupported (partial predicate)", def)
		}
		if _, _, ok := mockgen.ParseCheckHintForColumns(def, []string{"x"}); ok {
			t.Fatalf("%s must be unsupported with column names", def)
		}
	}
	// Supported single-predicate forms still parse.
	if h, k, ok := mockgen.ParseCheckHint("CHECK ((x BETWEEN 1 AND 10))"); !ok || k != "between" || *h.Min != 1 || *h.Max != 10 {
		t.Fatalf("plain BETWEEN broke: %+v %q %v", h, k, ok)
	}
	if _, k, ok := mockgen.ParseCheckHint("CHECK ((x IN (1, 2)))"); !ok || k != "in" {
		t.Fatalf("plain IN broke: %q %v", k, ok)
	}
	if _, k, ok := mockgen.ParseCheckHint("CHECK ((x >= 0 AND x <= 10))"); !ok || k != "between" {
		t.Fatalf("pure comparison conjunction broke: %q %v", k, ok)
	}
}

func TestExactIntParams(t *testing.T) {
	intMeta := func() mockgen.TableMeta {
		return mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
			{Name: "n", DataType: "integer", Udt: "int4"},
		}}
	}
	build := func(fields []mockgen.FieldSpec) error {
		_, err := mockgen.BuildPlan(intMeta(), mockgen.Request{Mode: "advanced", Count: 1, Fields: fields})
		return err
	}
	// 9007199254740993 (2^53+1) arrives as json.Number via UseNumber and
	// must stay exact — float64 would collapse it to ...992.
	if err := build([]mockgen.FieldSpec{{Column: "n", Generator: "integer",
		Params: map[string]any{"min": json.Number("9007199254740993"), "max": json.Number("9007199254740993")}}}); err == nil {
		t.Fatal("int4 out-of-range exact int should fail validation")
	} else if !strings.Contains(err.Error(), "int4") {
		t.Fatalf("want int4 range message, got %v", err)
	}
	// int8 accepts the exact value (proves no float64 rounding in between).
	bigMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "n", DataType: "bigint", Udt: "int8"}}}
	plan, err := mockgen.BuildPlan(bigMeta, mockgen.Request{Mode: "advanced", Count: 1,
		Fields: []mockgen.FieldSpec{{Column: "n", Generator: "integer",
			Params: map[string]any{"min": json.Number("9007199254740993"), "max": json.Number("9007199254740993")}}}})
	if err != nil {
		t.Fatalf("int8 exact int should validate: %v", err)
	}
	rows, err := plan.GenerateRows(1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rows[0][0]) != "9007199254740993" {
		t.Fatalf("exact int lost precision: %v", rows[0][0])
	}
	// Fractional values are rejected, not truncated via int64(...).
	for _, v := range []any{1.9, "1.9", json.Number("1.9")} {
		if err := build([]mockgen.FieldSpec{{Column: "n", Generator: "integer",
			Params: map[string]any{"min": v, "max": 10}}}); err == nil {
			t.Fatalf("fractional min %v (%T) should fail", v, v)
		}
	}
	// int2/int4 bounds enforced against the column type.
	smallMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "n", DataType: "smallint", Udt: "int2"}}}
	_, err = mockgen.BuildPlan(smallMeta, mockgen.Request{Mode: "advanced", Count: 1,
		Fields: []mockgen.FieldSpec{{Column: "n", Generator: "integer", Params: map[string]any{"max": 32768}}}})
	if err == nil {
		t.Fatal("int2 max 32768 should fail")
	}
	// min <= max for integer, decimal, and relative datetime.
	decMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "d", DataType: "numeric", Udt: "numeric"}}}
	_, err = mockgen.BuildPlan(decMeta, mockgen.Request{Mode: "advanced", Count: 1,
		Fields: []mockgen.FieldSpec{{Column: "d", Generator: "decimal", Params: map[string]any{"min": 9, "max": 1}}}})
	if err == nil {
		t.Fatal("decimal max<min should fail")
	}
	tsMeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "v", DataType: "timestamp", Udt: "timestamp"},
		{Name: "w", DataType: "timestamp", Udt: "timestamp"},
	}}
	_, err = mockgen.BuildPlan(tsMeta, mockgen.Request{Mode: "advanced", Count: 1, Fields: []mockgen.FieldSpec{
		{Column: "v", Generator: "datetime"},
		{Column: "w", Generator: "relative_datetime", Params: map[string]any{"source": "v", "min_offset_days": 10, "max_offset_days": 1}},
	}})
	if err == nil {
		t.Fatal("relative max_offset<min_offset should fail")
	}
}

func TestRandIntHugeRange(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "n", DataType: "bigint", Udt: "int8"}}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 200, Seed: 5, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "n", Generator: "integer",
			Params: map[string]any{"min": int64(-9000000000000000000), "max": int64(9000000000000000000)}}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(5, 200, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[fmt.Sprint(r[0])] = true
	}
	// The old overflow path returned min for every row (span <= 0).
	if len(seen) < 10 {
		t.Fatalf("huge range collapsed to %d distinct values", len(seen))
	}
}

func TestSequenceOverflowFailsAtPlanTime(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "s", DataType: "bigint", Udt: "int8"}}}
	_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3, Seed: 1, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "s", Generator: "sequence",
			Params: map[string]any{"start": int64(math.MaxInt64 - 1), "step": 2}}}})
	if err == nil || !strings.Contains(err.Error(), "sequence overflow") {
		t.Fatalf("expected sequence overflow at plan time, got %v", err)
	}
}

func TestBigIntCheckHintAndConstraintRemainExact(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "id", DataType: "bigint", Udt: "int8"}}, Checks: []mockgen.CheckMeta{{
		Name: "id_range", Definition: "CHECK ((id >= 9007199254740993 AND id <= 9007199254740995))",
	}}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 100, Seed: 1, HasSeed: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(1, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		v := row[0].(int64)
		if v < 9007199254740993 || v > 9007199254740995 {
			t.Fatalf("out of exact range: %d", v)
		}
	}
	for _, tc := range []struct {
		op    string
		right json.Number
		valid bool
	}{
		{">", json.Number("9007199254740992"), true}, {"=", json.Number("9007199254740992"), false},
		{"!=", json.Number("9007199254740992"), true}, {"=", json.Number("9007199254740993"), true},
	} {
		err := mockgen.ValidateRow(map[string]any{"id": int64(9007199254740993)}, []mockgen.Constraint{{
			Kind: "compare", Operator: tc.op, Left: mockgen.ConstraintSide{Field: "id"}, Right: mockgen.ConstraintSide{Value: tc.right},
		}})
		if tc.valid && err != nil {
			t.Fatalf("%d %s %s: %v", 9007199254740993, tc.op, tc.right, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("comparison should fail: %s %s", tc.op, tc.right)
		}
	}
}

func TestDecimalStrictCheckIsNotInferred(t *testing.T) {
	for _, def := range []string{"CHECK ((price > 0.1))", "CHECK ((price < 1.25))"} {
		if _, _, ok := mockgen.ParseCheckHint(def); ok {
			t.Fatalf("strict decimal check must be unsupported: %s", def)
		}
	}
}

func TestBigIntCheckChoosesExactTighterBounds(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "id", DataType: "bigint", Udt: "int8"}}, Checks: []mockgen.CheckMeta{{
		Name: "ck", Definition: "CHECK (id >= 9007199254740992 AND id >= 9007199254740993 AND id <= 9007199254740995)",
	}}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1000})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(1, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		v := row[0].(int64)
		if v < 9007199254740993 || v > 9007199254740995 {
			t.Fatalf("invalid bigint: %d", v)
		}
	}
	meta.Checks[0].Definition = "CHECK (id >= 9007199254740992 AND id > 9007199254740992)"
	plan, err = mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 100})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = plan.GenerateRows(1, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row[0].(int64) < 9007199254740993 {
			t.Fatalf("strict bigint lower bound lost: %v", row[0])
		}
	}
}

func TestOneSidedChecksUseSafeOpenBounds(t *testing.T) {
	for _, tc := range []struct {
		def   string
		valid func(int64) bool
	}{
		{"CHECK (id >= 2000000)", func(v int64) bool { return v >= 2000000 }},
		{"CHECK (id <= -2000000)", func(v int64) bool { return v <= -2000000 }},
	} {
		meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "id", DataType: "bigint", Udt: "int8"}}, Checks: []mockgen.CheckMeta{{Definition: tc.def}}}
		plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1000})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := plan.GenerateRows(1, 1000, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if !tc.valid(row[0].(int64)) {
				t.Fatalf("%s generated invalid value %v", tc.def, row[0])
			}
		}
	}
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "id", DataType: "bigint", Udt: "int8"}}, Checks: []mockgen.CheckMeta{{Definition: "CHECK (id >= 2000000)"}, {Definition: "CHECK (id <= 3000000)"}}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1000})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(1, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		v := row[0].(int64)
		if v < 2000000 || v > 3000000 {
			t.Fatalf("separate checks not merged: %d", v)
		}
	}
}

func TestSequenceRespectsTargetIntegerBounds(t *testing.T) {
	for _, tc := range []struct {
		udt         string
		start, step int64
		count       int
		fail        bool
	}{
		{"int2", 32760, 1, 8, false}, {"int2", 32760, 1, 9, true}, {"int2", 32760, 10, 2, true},
		{"int2", -32760, -1, 8, false}, {"int2", -32760, -10, 2, true},
		{"int4", math.MaxInt32 - 2, 1, 3, false}, {"int4", math.MaxInt32 - 2, 1, 4, true},
	} {
		meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "s", DataType: "integer", Udt: tc.udt}}}
		_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: tc.count, Fields: []mockgen.FieldSpec{{Column: "s", Generator: "sequence", Params: map[string]any{"start": tc.start, "step": tc.step}}}})
		if tc.fail && err == nil {
			t.Fatalf("%s sequence should fail", tc.udt)
		}
		if !tc.fail && err != nil {
			t.Fatalf("%s sequence: %v", tc.udt, err)
		}
	}
}

func TestChoiceMismatchedWeightsAreNormalizedOnce(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text"}}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 100, Fields: []mockgen.FieldSpec{{Column: "v", Generator: "choice", Params: map[string]any{"values": []any{"a", "b", "c"}, "weights": []any{1, 2}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Fields[0].WeightsProcessed || plan.Fields[0].NormWeights != nil {
		t.Fatalf("mismatched weights retained normalized values: %+v", plan.Fields[0])
	}
	if _, err := plan.GenerateRows(1, 100, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMatchFullRejectsDifferentNullProbabilities(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns:     []mockgen.ColumnMeta{{Name: "a", DataType: "integer", Udt: "int4", Nullable: true}, {Name: "b", DataType: "integer", Udt: "int4", Nullable: true}},
		ForeignKeys: []mockgen.ForeignKeyMeta{{Name: "fk", Column: "a", RefSchema: "public", RefTable: "p", RefColumn: "a", MatchType: "f"}, {Name: "fk", Column: "b", RefSchema: "public", RefTable: "p", RefColumn: "b", MatchType: "f"}},
	}
	_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1, Fields: []mockgen.FieldSpec{{Column: "a", Generator: "foreign_key", NullProb: 0.2}, {Column: "b", Generator: "foreign_key", NullProb: 0.7}}})
	if err == nil || !strings.Contains(err.Error(), "MATCH FULL") {
		t.Fatalf("want MATCH FULL validation error, got %v", err)
	}
}

func TestMatchFullNeverProducesPartialNull(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns:     []mockgen.ColumnMeta{{Name: "a", DataType: "integer", Udt: "int4", Nullable: true}, {Name: "b", DataType: "integer", Udt: "int4", Nullable: true}},
		ForeignKeys: []mockgen.ForeignKeyMeta{{Name: "fk", Column: "a", RefSchema: "public", RefTable: "parent", RefColumn: "a", MatchType: "f"}, {Name: "fk", Column: "b", RefSchema: "public", RefTable: "parent", RefColumn: "b", MatchType: "f"}},
	}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 5000, Fields: []mockgen.FieldSpec{{Column: "a", Generator: "foreign_key", NullProb: 0.5}, {Column: "b", Generator: "foreign_key", NullProb: 0.5}}})
	if err != nil {
		t.Fatal(err)
	}
	plan.FKTuples = map[string][][]any{"fk": {{int64(1), int64(10)}, {int64(2), int64(20)}}}
	rows, err := plan.GenerateRows(123, 5000, map[string][]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if (row[0] == nil) != (row[1] == nil) {
			t.Fatalf("MATCH FULL partial null: %v", row)
		}
	}
}

func TestCompositeFKCustomMemberRejected(t *testing.T) {
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
	// One member customized via Constant: the remaining FK member would
	// otherwise take a random tuple and mix invalid combinations.
	_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 5, Fields: []mockgen.FieldSpec{
		{Column: "country", Generator: "constant", Params: map[string]any{"value": "US"}},
		{Column: "customer_id", Generator: "foreign_key"},
	}})
	if err == nil || !strings.Contains(err.Error(), "composite foreign key") {
		t.Fatalf("want composite-FK unit error, got %v", err)
	}
	// Explicit ref_* override on one member is the same violation.
	_, err = mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 5, Fields: []mockgen.FieldSpec{
		{Column: "country", Generator: "foreign_key",
			Params: map[string]any{"ref_schema": "public", "ref_table": "other", "ref_column": "country"}},
		{Column: "customer_id", Generator: "foreign_key"},
	}})
	if err == nil || !strings.Contains(err.Error(), "composite foreign key") {
		t.Fatalf("want composite-FK unit error for ref override, got %v", err)
	}
}

func TestWeightsCappedAndNormalized(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text"}}}
	// Hundreds of thousands of weights: rejected once at plan time instead
	// of re-allocated per row across 20k rows.
	huge := make([]any, 200000)
	for i := range huge {
		huge[i] = 1
	}
	_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1,
		Fields: []mockgen.FieldSpec{{Column: "v", Generator: "choice",
			Params: map[string]any{"values": []any{"a"}, "weights": huge}}}})
	if err == nil || !strings.Contains(err.Error(), "weights") {
		t.Fatalf("want weights cap error, got %v", err)
	}
	// Oversized constant payloads are bounded (amplification per row).
	big := strings.Repeat("x", 5000)
	_, err = mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 1,
		Fields: []mockgen.FieldSpec{{Column: "v", Generator: "constant", Params: map[string]any{"value": big}}}})
	if err == nil {
		t.Fatal("oversized constant should fail")
	}
	// Normalized weights are reused: plan carries them, generation honors
	// them (single value with matching weight still picks it).
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3, Seed: 2, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "v", Generator: "choice",
			Params: map[string]any{"values": []any{"only"}, "weights": []any{5}}}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(2, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if fmt.Sprint(r[0]) != "only" {
			t.Fatalf("weighted single value broke: %v", r[0])
		}
	}
}

func TestCompareLiteralJSONNumber(t *testing.T) {
	// HTTP bodies decode with UseNumber, so literal 10 arrives as
	// json.Number — not int64 as in engine-only tests. Generated 2 vs
	// literal 10 must compare numerically (2 < 10), never as "2" vs "10".
	row := map[string]any{"n": int64(2)}
	cons := []mockgen.Constraint{{
		Kind: "compare", Operator: "<",
		Left:  mockgen.ConstraintSide{Field: "n"},
		Right: mockgen.ConstraintSide{Value: json.Number("10")},
	}}
	if err := mockgen.ValidateRow(row, cons); err != nil {
		t.Fatalf("2 < json.Number(10) should hold: %v", err)
	}
	cons[0].Operator = ">"
	if err := mockgen.ValidateRow(row, cons); err == nil {
		t.Fatal("2 > json.Number(10) should fail")
	}
}

func TestLargeOffsetNoOverflow(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "v", DataType: "timestamp", Udt: "timestamp"},
		{Name: "w", DataType: "timestamp", Udt: "timestamp"},
	}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 5, Seed: 3, HasSeed: true,
		Fields: []mockgen.FieldSpec{
			{Column: "v", Generator: "datetime"},
			{Column: "w", Generator: "relative_datetime",
				Params: map[string]any{"source": "v", "min_offset_days": 364000, "max_offset_days": 365000}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(3, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	idx := map[string]int{}
	for i, c := range plan.InsertCols {
		idx[c] = i
	}
	for i, r := range rows {
		base, ok1 := r[idx["v"]].(time.Time)
		got, ok2 := r[idx["w"]].(time.Time)
		if !ok1 || !ok2 {
			t.Fatalf("row %d not datetimes: %v", i, r)
		}
		// NB: (got-base) as time.Duration overflows past ~292 years, so
		// compare via AddDate bounds instead of Sub().Hours()/24.
		lo, hi := base.AddDate(0, 0, 364000), base.AddDate(0, 0, 365000)
		if got.Before(lo) || got.After(hi) {
			t.Fatalf("row %d offset outside 364000..365000 days: base=%v got=%v", i, base, got)
		}
		if got.Year() < 2000 || got.Year() > 4000 {
			t.Fatalf("row %d overflowed datetime: %v", i, got)
		}
	}
}
