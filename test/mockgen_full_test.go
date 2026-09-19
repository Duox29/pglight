package test

// Full engine coverage for internal/mockgen: every generator's contract,
// param coercion (incl. encoding/json numbers from HTTP bodies), semantic
// auto-resolution, CHECK-hint folding, constraint validation, dependency
// ordering and request validation. No database needed.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"pglight/internal/mockgen"
)

func tcol(name, dataType, udt string) mockgen.ColumnMeta {
	return mockgen.ColumnMeta{Name: name, DataType: dataType, Udt: udt, Nullable: true}
}

func genAdvanced(t *testing.T, meta mockgen.TableMeta, seed int64, n int, fields []mockgen.FieldSpec, cons []mockgen.Constraint, fk map[string][]any) ([][]any, *mockgen.Plan) {
	t.Helper()
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{
		Mode: "advanced", Count: n, Seed: seed, HasSeed: true,
		Fields: fields, Constraints: cons, FKValues: fk,
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	rows, err := plan.GenerateRows(seed, n, fk)
	if err != nil {
		t.Fatalf("GenerateRows: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("got %d rows want %d", len(rows), n)
	}
	return rows, plan
}

func colIdx(plan *mockgen.Plan) map[string]int {
	idx := map[string]int{}
	for i, c := range plan.InsertCols {
		idx[c] = i
	}
	return idx
}

var timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d:[0-5]\d$`)
var alnumRe = regexp.MustCompile(`^[A-Za-z0-9]{8,24}$`)

func TestMockgenSimpleArrayTypes(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("ia", "ARRAY", "_int4"), tcol("fa", "ARRAY", "_float8"),
		tcol("ba", "ARRAY", "_bool"), tcol("ta", "ARRAY", "_text"),
	}}
	rows, _ := genSimple(t, meta, 5, 200)
	seenLen := map[int]bool{}
	for _, r := range rows {
		if len(r) != 4 {
			t.Fatalf("row width %d want 4", len(r))
		}
		ia, ok := r[0].([]int64)
		if !ok {
			t.Fatalf("int array type %T", r[0])
		}
		fa, ok := r[1].([]float64)
		if !ok {
			t.Fatalf("float array type %T", r[1])
		}
		ba, ok := r[2].([]bool)
		if !ok {
			t.Fatalf("bool array type %T", r[2])
		}
		ta, ok := r[3].([]string)
		if !ok {
			t.Fatalf("text array type %T", r[3])
		}
		for _, l := range []int{len(ia), len(fa), len(ba), len(ta)} {
			if l < 0 || l > 5 {
				t.Fatalf("array length %d out of 0..5", l)
			}
			seenLen[l] = true
		}
		for _, v := range ia {
			if v < 0 || v > 10000 {
				t.Fatalf("int element %d out of range", v)
			}
		}
	}
	if len(seenLen) < 3 {
		t.Fatalf("array lengths suspiciously static: %v", seenLen)
	}
}

func TestMockgenSimpleTimeJsonBytea(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("tm", "time without time zone", "time"),
		tcol("js", "jsonb", "jsonb"),
		tcol("by", "bytea", "bytea"),
	}}
	rows, _ := genSimple(t, meta, 11, 50)
	for _, r := range rows {
		if !timeRe.MatchString(fmt.Sprint(r[0])) {
			t.Fatalf("time invalid: %v", r[0])
		}
		var js map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(r[1])), &js); err != nil {
			t.Fatalf("json invalid: %v (%v)", r[1], err)
		}
		if _, ok := js["key"]; !ok {
			t.Fatalf("json missing key: %v", r[1])
		}
		b, ok := r[2].([]byte)
		if !ok || len(b) < 4 || len(b) > 15 {
			t.Fatalf("bytea length invalid: %v", r[2])
		}
	}
}

func TestMockgenSimpleDateBounds(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("d", "date", "date"), tcol("ts", "timestamp with time zone", "timestamptz"),
	}}
	rows, _ := genSimple(t, meta, 42, 100)
	for _, r := range rows {
		d, ok := r[0].(time.Time)
		if !ok {
			t.Fatalf("date not time.Time: %T", r[0])
		}
		if d.Hour() != 0 || d.Minute() != 0 || d.Second() != 0 || d.Location() != time.UTC {
			t.Fatalf("date not midnight UTC: %v", d)
		}
		if d.Year() < 2021 || d.Year() > 2026 {
			t.Fatalf("date out of 5-year window: %v", d)
		}
		ts, ok := r[1].(time.Time)
		if !ok {
			t.Fatalf("timestamp not time.Time: %T", r[1])
		}
		if ts.Year() < 2021 || ts.After(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("timestamp out of window: %v", ts)
		}
		if ts.Nanosecond()%1000 != 0 {
			t.Fatalf("timestamp not microsecond-truncated: %v", ts)
		}
	}
}

// Simple mode must not infer business semantics from column names.
func TestMockgenSimpleIgnoresSemantics(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "email", DataType: "text", Udt: "text"},
		{Name: "first_name", DataType: "text", Udt: "text"},
		{Name: "phone", DataType: "text", Udt: "text"},
	}}
	rows, _ := genSimple(t, meta, 3, 50)
	for _, r := range rows {
		for _, v := range r {
			if !alnumRe.MatchString(fmt.Sprint(v)) {
				t.Fatalf("simple value looks semantic: %q", v)
			}
		}
	}
}

func TestMockgenAdvancedInteger(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("n", "integer", "int4")}}
	mk := func(params map[string]any) [][]any {
		rows, _ := genAdvanced(t, meta, 8, 50,
			[]mockgen.FieldSpec{{Column: "n", Generator: "integer", Params: params}}, nil, nil)
		return rows
	}
	for _, r := range mk(map[string]any{"min": 5, "max": 5}) {
		if r[0] != int64(5) {
			t.Fatalf("min==max should pin 5, got %v", r[0])
		}
	}
	for _, r := range mk(map[string]any{"min": 10, "max": 5}) {
		if v := r[0].(int64); v < 5 || v > 10 {
			t.Fatalf("swapped bounds not normalized: %d", v)
		}
	}
	for _, r := range mk(nil) {
		if v := r[0].(int64); v < 0 || v > 10000 {
			t.Fatalf("default integer out of range: %d", v)
		}
	}
}

func TestMockgenAdvancedDecimal(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("d", "numeric", "numeric")}}
	mk := func(params map[string]any, n int) [][]any {
		rows, _ := genAdvanced(t, meta, 8, n,
			[]mockgen.FieldSpec{{Column: "d", Generator: "decimal", Params: params}}, nil, nil)
		return rows
	}
	for _, r := range mk(map[string]any{"min": 0, "max": 0, "scale": 2}, 5) {
		if r[0] != "0.00" {
			t.Fatalf("min==max should pin, got %v", r[0])
		}
	}
	for _, r := range mk(map[string]any{"min": 0, "max": 9, "scale": 0}, 20) {
		if !mockIntRe.MatchString(fmt.Sprint(r[0])) {
			t.Fatalf("scale 0 should be integral: %v", r[0])
		}
	}
	for _, r := range mk(map[string]any{"min": 1, "max": 2, "scale": 99}, 5) {
		if m := regexp.MustCompile(`^-?\d+\.\d{10}$`).MatchString(fmt.Sprint(r[0])); !m {
			t.Fatalf("scale clamped to 10: %v", r[0])
		}
	}
	for _, r := range mk(map[string]any{"min": 0, "max": 100, "scale": -3}, 5) {
		if !mockIntRe.MatchString(fmt.Sprint(r[0])) {
			t.Fatalf("negative scale should be integral: %v", r[0])
		}
	}
}

func TestMockgenAdvancedBoolean(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("b", "boolean", "bool")}}
	mk := func(params map[string]any, n int) [][]any {
		rows, _ := genAdvanced(t, meta, 8, n,
			[]mockgen.FieldSpec{{Column: "b", Generator: "boolean", Params: params}}, nil, nil)
		return rows
	}
	for _, r := range mk(map[string]any{"true_probability": 1}, 10) {
		if r[0] != true {
			t.Fatalf("p=1 should always be true: %v", r[0])
		}
	}
	for _, r := range mk(map[string]any{"true_probability": 0}, 10) {
		if r[0] != false {
			t.Fatalf("p=0 should always be false: %v", r[0])
		}
	}
	seen := map[bool]bool{}
	for _, r := range mk(nil, 200) {
		seen[r[0].(bool)] = true
	}
	if len(seen) != 2 {
		t.Fatal("default boolean never varied")
	}
}

func TestMockgenAdvancedString(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("s", "text", "text")}}
	rows, _ := genAdvanced(t, meta, 8, 20,
		[]mockgen.FieldSpec{{Column: "s", Generator: "string", Params: map[string]any{"min_length": 5, "max_length": 5}}}, nil, nil)
	for _, r := range rows {
		if len(fmt.Sprint(r[0])) != 5 {
			t.Fatalf("exact length not honored: %q", r[0])
		}
	}
}

func TestMockgenAdvancedUuid(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("u", "uuid", "uuid")}}
	rows, _ := genAdvanced(t, meta, 8, 200,
		[]mockgen.FieldSpec{{Column: "u", Generator: "uuid"}}, nil, nil)
	seen := map[string]bool{}
	for _, r := range rows {
		s := fmt.Sprint(r[0])
		if !uuidRe.MatchString(s) {
			t.Fatalf("uuid v4 invalid: %q", s)
		}
		seen[s] = true
	}
	if len(seen) != 200 {
		t.Fatalf("uuid collision in 200 draws: %d unique", len(seen))
	}
}

func TestMockgenAdvancedDateRange(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("d", "date", "date")}}
	mk := func(params map[string]any) [][]any {
		rows, _ := genAdvanced(t, meta, 8, 30,
			[]mockgen.FieldSpec{{Column: "d", Generator: "date", Params: params}}, nil, nil)
		return rows
	}
	lo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	hi := time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, r := range mk(map[string]any{"min": "2020-01-01", "max": "2020-02-01"}) {
		v := r[0].(time.Time)
		if v.Before(lo) || v.After(hi) {
			t.Fatalf("date out of explicit range: %v", v)
		}
	}
	// Swapped bounds normalize instead of erroring.
	for _, r := range mk(map[string]any{"min": "2021-06-01", "max": "2020-01-01"}) {
		v := r[0].(time.Time)
		if v.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) || v.After(time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("swapped range not normalized: %v", v)
		}
	}
	// Garbage bounds fall back to the 5-year default window.
	for _, r := range mk(map[string]any{"min": "not-a-date"}) {
		if _, ok := r[0].(time.Time); !ok {
			t.Fatalf("bad bound should fall back, got %T", r[0])
		}
	}
}

func TestMockgenAdvancedTime(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("tm", "time without time zone", "time")}}
	rows, _ := genAdvanced(t, meta, 8, 50,
		[]mockgen.FieldSpec{{Column: "tm", Generator: "time"}}, nil, nil)
	for _, r := range rows {
		if !timeRe.MatchString(fmt.Sprint(r[0])) {
			t.Fatalf("time invalid: %v", r[0])
		}
	}
}

func TestMockgenAdvancedSequence(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("a", "bigint", "int8"), tcol("b", "bigint", "int8"),
	}}
	rows, _ := genAdvanced(t, meta, 8, 6, []mockgen.FieldSpec{
		{Column: "a", Generator: "sequence", Params: map[string]any{"start": 10, "step": 5}},
		{Column: "b", Generator: "sequence", Params: map[string]any{"start": 3, "step": 0}},
	}, nil, nil)
	for i, r := range rows {
		if r[0] != int64(10+i*5) {
			t.Fatalf("row %d a=%v want %d", i, r[0], 10+i*5)
		}
		if r[1] != int64(3+i) {
			t.Fatalf("row %d b=%v want step-1 fallback %d", i, r[1], 3+i)
		}
	}
	// Negative steps count down.
	rows, _ = genAdvanced(t, meta, 8, 3, []mockgen.FieldSpec{
		{Column: "a", Generator: "sequence", Params: map[string]any{"start": 0, "step": -2}},
		{Column: "b", Generator: "sequence"},
	}, nil, nil)
	for i, r := range rows {
		if r[0] != int64(-2*i) {
			t.Fatalf("row %d negative step: %v", i, r[0])
		}
	}
}

func TestMockgenAdvancedJsonConstantNull(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("j", "jsonb", "jsonb"),
		tcol("c", "text", "text"),
		tcol("n", "text", "text"),
	}}
	rows, _ := genAdvanced(t, meta, 8, 10, []mockgen.FieldSpec{
		{Column: "j", Generator: "json"},
		{Column: "c", Generator: "constant", Params: map[string]any{"value": "FIXED"}},
		{Column: "n", Generator: "null"},
	}, nil, nil)
	for _, r := range rows {
		var js map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(r[0])), &js); err != nil {
			t.Fatalf("json invalid: %v", r[0])
		}
		if r[1] != "FIXED" {
			t.Fatalf("constant not passed through: %v", r[1])
		}
		if r[2] != nil {
			t.Fatalf("null generator should be nil: %v", r[2])
		}
	}
	// Constant without a value is nil — illegal on NOT NULL.
	nn := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "c", DataType: "text", Udt: "text", Nullable: false},
	}}
	plan, err := mockgen.BuildPlan(nn, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{{Column: "c", Generator: "constant"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.GenerateRows(1, 3, nil); err == nil {
		t.Fatal("valueless constant on NOT NULL should fail")
	}
}

func TestMockgenChoiceWeights(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("s", "text", "text")}}
	mk := func(params map[string]any, seed int64, n int) [][]any {
		rows, _ := genAdvanced(t, meta, seed, n,
			[]mockgen.FieldSpec{{Column: "s", Generator: "choice", Params: params}}, nil, nil)
		return rows
	}
	vals := []any{"a", "b", "c"}
	for _, r := range mk(map[string]any{"values": vals, "weights": []any{100, 0, 0}}, 8, 20) {
		if r[0] != "a" {
			t.Fatalf("zero weights should never pick: %v", r[0])
		}
	}
	// Mismatched weights fall back to uniform: every value appears.
	seen := map[string]bool{}
	for _, r := range mk(map[string]any{"values": vals, "weights": []any{1}}, 8, 300) {
		seen[fmt.Sprint(r[0])] = true
	}
	if len(seen) != 3 {
		t.Fatalf("bad weights should fall back to uniform: %v", seen)
	}
	seen = map[string]bool{}
	for _, r := range mk(map[string]any{"values": vals, "weights": []any{0, 0, 0}}, 9, 300) {
		seen[fmt.Sprint(r[0])] = true
	}
	if len(seen) != 3 {
		t.Fatalf("zero-total weights should fall back to uniform: %v", seen)
	}
	// No values and no hint: built-in a/b/c default.
	for _, r := range mk(nil, 8, 20) {
		switch fmt.Sprint(r[0]) {
		case "a", "b", "c":
		default:
			t.Fatalf("default choice out of domain: %v", r[0])
		}
	}
}

func TestMockgenNullProbability(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		tcol("maybe", "text", "text"),
		{Name: "req", DataType: "text", Udt: "text", Nullable: false},
	}}
	mk := func(np float64, n int) [][]any {
		rows, _ := genAdvanced(t, meta, 8, n, []mockgen.FieldSpec{
			{Column: "maybe", Generator: "string", NullProb: np},
			{Column: "req", Generator: "string", NullProb: 1},
		}, nil, nil)
		return rows
	}
	for _, r := range mk(1, 10) {
		if r[0] != nil {
			t.Fatalf("p=1 should always be null: %v", r[0])
		}
		if r[1] == nil {
			t.Fatal("NOT NULL must never be nulled")
		}
	}
	for _, r := range mk(0, 10) {
		if r[0] == nil {
			t.Fatal("p=0 should never be null")
		}
	}
	// NULLs bypass uniqueness tracking (matches PostgreSQL semantics).
	umeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{tcol("u", "text", "text")}}
	rows, _ := genAdvanced(t, umeta, 8, 10,
		[]mockgen.FieldSpec{{Column: "u", Generator: "string", Unique: true, NullProb: 1}}, nil, nil)
	for _, r := range rows {
		if r[0] != nil {
			t.Fatalf("expected nil, got %v", r[0])
		}
	}
}

func TestMockgenUniqueFromSchema(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "code", DataType: "text", Udt: "text", Nullable: false, Unique: true},
	}}
	// No explicit Unique flag: schema uniqueness is promoted by BuildPlan.
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 50, Seed: 4, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "code", Generator: "string"}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plan.GenerateRows(4, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		k := fmt.Sprint(r[0])
		if seen[k] {
			t.Fatalf("schema-unique violated: %q", k)
		}
		seen[k] = true
	}
	// Tiny domain still fails even when uniqueness comes from schema.
	plan2, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 4, Seed: 4, HasSeed: true,
		Fields: []mockgen.FieldSpec{{Column: "code", Generator: "choice", Params: map[string]any{"values": []any{"x", "y"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan2.GenerateRows(4, 4, nil); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("want uniqueness failure, got %v", err)
	}
}

func TestMockgenAutoResolution(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "i", DataType: "integer", Udt: "int4"},
		{Name: "n", DataType: "numeric", Udt: "numeric"},
		{Name: "f", DataType: "double precision", Udt: "float8"},
		{Name: "b", DataType: "boolean", Udt: "bool"},
		{Name: "u", DataType: "uuid", Udt: "uuid"},
		{Name: "d", DataType: "date", Udt: "date"},
		{Name: "ts", DataType: "timestamp with time zone", Udt: "timestamptz"},
		{Name: "tm", DataType: "time without time zone", Udt: "time"},
		{Name: "j", DataType: "jsonb", Udt: "jsonb"},
		{Name: "by", DataType: "bytea", Udt: "bytea"},
		{Name: "t", DataType: "text", Udt: "text"},
		{Name: "ip", DataType: "inet", Udt: "inet"},
		{Name: "email", DataType: "text", Udt: "text"},
		{Name: "price", DataType: "numeric", Udt: "numeric"},
		{Name: "created_at", DataType: "timestamp with time zone", Udt: "timestamptz"},
		{Name: "mood", DataType: "USER-DEFINED", Udt: "mood", EnumValues: []string{"x", "y"}},
	}}
	auto := []mockgen.FieldSpec{}
	for _, c := range meta.Columns {
		auto = append(auto, mockgen.FieldSpec{Column: c.Name, Generator: "auto"})
	}
	rows, _ := genAdvanced(t, meta, 21, 30, auto, nil, nil)
	for _, r := range rows {
		if _, ok := r[0].(int64); !ok {
			t.Fatalf("auto int: %T", r[0])
		}
		if parts := strings.Split(fmt.Sprint(r[1]), "."); len(parts) != 2 || len(parts[1]) != 2 {
			t.Fatalf("auto decimal: %v", r[1])
		}
		if _, ok := r[2].(float64); !ok {
			// NOTE: auto maps float udts to the decimal generator, which
			// emits exact decimal strings (never binary floats).
			if parts := strings.Split(fmt.Sprint(r[2]), "."); len(parts) != 2 {
				t.Fatalf("auto float should be decimal text: %v (%T)", r[2], r[2])
			}
		}
		if _, ok := r[3].(bool); !ok {
			t.Fatalf("auto bool: %T", r[3])
		}
		if !uuidRe.MatchString(fmt.Sprint(r[4])) {
			t.Fatalf("auto uuid: %v", r[4])
		}
		if _, ok := r[5].(time.Time); !ok {
			t.Fatalf("auto date: %T", r[5])
		}
		if _, ok := r[6].(time.Time); !ok {
			t.Fatalf("auto datetime: %T", r[6])
		}
		if !timeRe.MatchString(fmt.Sprint(r[7])) {
			t.Fatalf("auto time: %v", r[7])
		}
		var js map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(r[8])), &js); err != nil {
			t.Fatalf("auto json: %v", r[8])
		}
		if _, ok := r[9].(string); !ok {
			t.Fatalf("auto bytea falls back to string: %T", r[9])
		}
		if _, ok := r[10].(string); !ok {
			t.Fatalf("auto text: %T", r[10])
		}
		if _, ok := r[11].(string); !ok {
			t.Fatalf("auto unknown udt falls back to string: %T", r[11])
		}
		if !strings.Contains(fmt.Sprint(r[12]), "@") {
			t.Fatalf("auto email should look like email: %v", r[12])
		}
		if parts := strings.Split(fmt.Sprint(r[13]), "."); len(parts) != 2 {
			t.Fatalf("auto price should be decimal: %v", r[13])
		}
		if _, ok := r[14].(time.Time); !ok {
			t.Fatalf("auto created_at should be datetime: %T", r[14])
		}
		switch fmt.Sprint(r[15]) {
		case "x", "y":
		default:
			t.Fatalf("auto enum out of domain: %v", r[15])
		}
	}
	// Identity/serial columns resolve to db_default and leave the INSERT.
	idmeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "id", DataType: "bigint", Udt: "int8", Identity: true},
		{Name: "s", DataType: "integer", Udt: "int4", Default: strp("nextval('s'::regclass)")},
		{Name: "v", DataType: "text", Udt: "text"},
	}}
	plan, err := mockgen.BuildPlan(idmeta, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{
			{Column: "id", Generator: "auto"},
			{Column: "s", Generator: "auto"},
			{Column: "v", Generator: "auto"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	requireDeep(t, "insert", "cols", plan.InsertCols, []string{"v"})
}

func TestMockgenCheckHintApply(t *testing.T) {
	meta := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{
			{Name: "age", DataType: "integer", Udt: "int4"},
			{Name: "status", DataType: "text", Udt: "text"},
		},
		Checks: []mockgen.CheckMeta{
			{Name: "c_age", Definition: "CHECK ((age >= 18 AND age <= 100))"},
			{Name: "c_status", Definition: "CHECK ((status = ANY (ARRAY['a'::text, 'b'::text])))"},
		},
	}
	rows, plan := genAdvanced(t, meta, 31, 50,
		[]mockgen.FieldSpec{
			{Column: "age", Generator: "auto"},
			{Column: "status", Generator: "auto"},
		}, nil, nil)
	if len(plan.Warnings) != 0 {
		t.Fatalf("supported checks should not warn: %v", plan.Warnings)
	}
	idx := colIdx(plan)
	for _, r := range rows {
		if v := r[idx["age"]].(int64); v < 18 || v > 100 {
			t.Fatalf("hint range ignored: %d", v)
		}
		switch fmt.Sprint(r[idx["status"]]) {
		case "a", "b":
		default:
			t.Fatalf("hint IN ignored: %v", r[idx["status"]])
		}
	}
	// Multiple checks on one column merge to the tightest bound; an explicit
	// param wins over the hint on its own side only.
	m2 := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{{Name: "n", DataType: "integer", Udt: "int4"}},
		Checks: []mockgen.CheckMeta{
			{Name: "c1", Definition: "CHECK ((n >= 10))"},
			{Name: "c2", Definition: "CHECK ((n <= 20))"},
		},
	}
	rows, _ = genAdvanced(t, m2, 31, 30,
		[]mockgen.FieldSpec{{Column: "n", Generator: "integer"}}, nil, nil)
	for _, r := range rows {
		if v := r[0].(int64); v < 10 || v > 20 {
			t.Fatalf("merged hint ignored: %d", v)
		}
	}
	rows, _ = genAdvanced(t, m2, 31, 30,
		[]mockgen.FieldSpec{{Column: "n", Generator: "integer", Params: map[string]any{"min": 5}}}, nil, nil)
	for _, r := range rows {
		if v := r[0].(int64); v < 5 || v > 20 {
			t.Fatalf("explicit min + hint max ignored: %d", v)
		}
	}
	// Cast-decorated ANY elements unquote to plain values.
	h, k, ok := mockgen.ParseCheckHint("CHECK ((s = ANY (ARRAY['x'::text, 'O''Brien'::character varying])))")
	if !ok || k != "in" || len(h.Values) != 2 || h.Values[0] != "x" || h.Values[1] != "O'Brien" {
		t.Fatalf("cast elements not unquoted: %+v", h)
	}
	// Unsupported checks warn and never block the plan.
	m3 := mockgen.TableMeta{
		Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text"}},
		Checks: []mockgen.CheckMeta{
			{Name: "c_weird", Definition: "CHECK (my_func(v) > 0)"},
		},
	}
	_, plan3 := genAdvanced(t, m3, 31, 5,
		[]mockgen.FieldSpec{{Column: "v", Generator: "auto"}}, nil, nil)
	if len(plan3.Warnings) != 1 || !strings.Contains(plan3.Warnings[0], "c_weird") {
		t.Fatalf("want named warning, got %v", plan3.Warnings)
	}
}

func TestMockgenValidateRowFull(t *testing.T) {
	cc := func(l, op, r string, lv any) mockgen.Constraint {
		return mockgen.Constraint{Kind: "compare",
			Left: mockgen.ConstraintSide{Field: l}, Operator: op,
			Right: func() mockgen.ConstraintSide {
				if r != "" {
					return mockgen.ConstraintSide{Field: r}
				}
				return mockgen.ConstraintSide{Value: lv}
			}()}
	}
	row := map[string]any{"a": int64(3), "b": int64(5), "s": "x", "t1": "2024-01-02T00:00:00Z", "t2": "2024-01-01T00:00:00Z", "f": "4.5"}
	cases := []struct {
		c    mockgen.Constraint
		want bool
	}{
		{cc("a", "<", "b", nil), true}, {cc("b", ">", "a", nil), true},
		{cc("a", "=", "b", nil), false}, {cc("a", "!=", "b", nil), true},
		{cc("a", "<>", "b", nil), true}, {cc("a", "<=", "a", nil), true},
		{cc("a", ">=", "a", nil), true},
		{cc("s", ">", "", "w"), true},                  // literal side, string compare
		{cc("a", "=", "", int64(3)), true},             // int vs int literal
		{cc("f", "=", "", "4.5"), true},                // numeric string coerces
		{cc("t1", ">", "t2", nil), true},               // RFC3339 time compare
		{cc("a", "=", "missing", nil), false},          // missing field is nil ≠ 3
		{cc("missing", "=", "alsomissing", nil), true}, // both nil are equal
		{cc("missing", "!=", "a", nil), true},
		{cc("missing", "<", "a", nil), false}, // ordered ops on nil are false
	}
	for i, tc := range cases {
		err := mockgen.ValidateRow(row, []mockgen.Constraint{tc.c})
		if (err == nil) != tc.want {
			t.Fatalf("case %d (%v %s): err=%v want ok=%v", i, tc.c.Left, tc.c.Operator, err, tc.want)
		}
	}
	if err := mockgen.ValidateRow(row, nil); err != nil {
		t.Fatalf("empty constraints should pass: %v", err)
	}
	if err := mockgen.ValidateRow(row, []mockgen.Constraint{{Kind: "regex", Left: mockgen.ConstraintSide{Field: "a"}, Operator: "=", Right: mockgen.ConstraintSide{Field: "b"}}}); err == nil {
		t.Fatal("bad kind should error")
	}
	err := mockgen.ValidateRow(row, []mockgen.Constraint{{Left: mockgen.ConstraintSide{Field: "a"}, Operator: "<=>", Right: mockgen.ConstraintSide{Field: "b"}}})
	if err == nil || !strings.Contains(err.Error(), "<=>") {
		t.Fatalf("bad operator should error, got %v", err)
	}
	// Violation message names the constraint.
	err = mockgen.ValidateRow(row, []mockgen.Constraint{{Kind: "compare",
		Left: mockgen.ConstraintSide{Field: "a"}, Operator: ">", Right: mockgen.ConstraintSide{Field: "b"}}})
	if err == nil || !strings.Contains(err.Error(), "a > b") {
		t.Fatalf("violation should name sides, got %v", err)
	}
	var cerr *mockgen.ConstraintError
	_ = cerr
}

func TestMockgenTopoOrder(t *testing.T) {
	// Chain a→b→c: sources first regardless of input order.
	got, err := mockgen.TopoOrder([]string{"c", "b", "a"}, map[string]string{"c": "b", "b": "a"})
	if err != nil {
		t.Fatal(err)
	}
	requireDeep(t, "chain", "order", got, []string{"a", "b", "c"})
	// Diamond: partial order respected (shared source first, dependent last).
	got, err = mockgen.TopoOrder([]string{"d", "c", "b", "a"},
		map[string]string{"d": "b", "b": "a", "c": "a"})
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, c := range got {
		pos[c] = i
	}
	if !(pos["a"] < pos["b"] && pos["a"] < pos["c"] && pos["b"] < pos["d"]) {
		t.Fatalf("diamond order wrong: %v", got)
	}
	// No deps: input order preserved.
	got, err = mockgen.TopoOrder([]string{"x", "y"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireDeep(t, "plain", "order", got, []string{"x", "y"})
	// Self and 3-cycles rejected.
	if _, err := mockgen.TopoOrder([]string{"a"}, map[string]string{"a": "a"}); err == nil {
		t.Fatal("self-cycle should fail")
	}
	if _, err := mockgen.TopoOrder([]string{"a", "b", "c"},
		map[string]string{"a": "b", "b": "c", "c": "a"}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("3-cycle should fail, got %v", err)
	}
}

func TestMockgenRelativeEdges(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "c", DataType: "timestamp with time zone", Udt: "timestamptz"},
		{Name: "u", DataType: "timestamp with time zone", Udt: "timestamptz"},
	}}
	// Missing source param rejected at plan time.
	_, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{
			{Column: "c", Generator: "datetime"},
			{Column: "u", Generator: "relative_datetime"},
		}})
	if err == nil || !strings.Contains(err.Error(), "params.source") {
		t.Fatalf("want source-required error, got %v", err)
	}
	// Unknown source column rejected at plan time.
	_, err = mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{
			{Column: "c", Generator: "datetime"},
			{Column: "u", Generator: "relative_datetime", Params: map[string]any{"source": "nope"}},
		}})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want unknown-source error, got %v", err)
	}
	// Fixed offset: identical shift for every row.
	rows, _ := genAdvanced(t, meta, 44, 10, []mockgen.FieldSpec{
		{Column: "c", Generator: "datetime"},
		{Column: "u", Generator: "relative_datetime",
			Params: map[string]any{"source": "c", "min_offset_days": 1, "max_offset_days": 1}},
	}, nil, nil)
	for _, r := range rows {
		if r[1].(time.Time).Sub(r[0].(time.Time)) != 24*time.Hour {
			t.Fatalf("fixed offset not exact: %v -> %v", r[0], r[1])
		}
	}
	// Negative window flips the inequality.
	rows, _ = genAdvanced(t, meta, 44, 10, []mockgen.FieldSpec{
		{Column: "c", Generator: "datetime"},
		{Column: "u", Generator: "relative_datetime",
			Params: map[string]any{"source": "c", "min_offset_days": -2, "max_offset_days": -1}},
	}, nil, nil)
	for _, r := range rows {
		if !r[1].(time.Time).Before(r[0].(time.Time)) {
			t.Fatalf("negative offset should precede: %v -> %v", r[0], r[1])
		}
	}
	// Source omitted (database default) is not yet generated at row time.
	dmeta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "c", DataType: "timestamp with time zone", Udt: "timestamptz", Default: strp("now()")},
		{Name: "u", DataType: "timestamp with time zone", Udt: "timestamptz"},
	}}
	plan, err := mockgen.BuildPlan(dmeta, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{
			{Column: "c", Generator: "db_default"},
			{Column: "u", Generator: "relative_datetime", Params: map[string]any{"source": "c"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.GenerateRows(1, 3, nil); err == nil || !strings.Contains(err.Error(), "not generated yet") {
		t.Fatalf("want not-generated-yet error, got %v", err)
	}
}

func TestMockgenFKModes(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "pid", DataType: "bigint", Udt: "int8"},
	}}
	pool := map[string][]any{"pid": {int64(7), int64(8), int64(9)}}
	// Sequential round-robins the pool exactly.
	rows, _ := genAdvanced(t, meta, 55, 5,
		[]mockgen.FieldSpec{{Column: "pid", Generator: "foreign_key", Params: map[string]any{"mode": "sequential"}}}, nil, pool)
	for i, r := range rows {
		if want := int64(7 + i%3); r[0] != want {
			t.Fatalf("row %d: got %v want %d", i, r[0], want)
		}
	}
	// Unknown mode falls back to uniform random within the pool.
	rows, _ = genAdvanced(t, meta, 55, 100,
		[]mockgen.FieldSpec{{Column: "pid", Generator: "foreign_key", Params: map[string]any{"mode": "weird"}}}, nil, pool)
	for _, r := range rows {
		if v := r[0]; v != int64(7) && v != int64(8) && v != int64(9) {
			t.Fatalf("out of pool: %v", v)
		}
	}
}

func TestMockgenUnknownGenerator(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "t", DataType: "text", Udt: "text"},
		{Name: "n", DataType: "integer", Udt: "int4"},
	}}
	rows, _ := genAdvanced(t, meta, 66, 10, []mockgen.FieldSpec{
		{Column: "t", Generator: "frobnicate"},
		{Column: "n", Generator: "FROBNICATE"},
	}, nil, nil)
	for _, r := range rows {
		if _, ok := r[0].(string); !ok {
			t.Fatalf("unknown generator on text should fall back to string: %T", r[0])
		}
		if _, ok := r[1].(int64); !ok {
			t.Fatalf("unknown generator on int should fall back to int: %T", r[1])
		}
	}
}

func TestMockgenJsonNumberParams(t *testing.T) {
	// HTTP bodies decode numbers as json.Number; params must honor them.
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "n", DataType: "integer", Udt: "int4"},
		{Name: "b", DataType: "boolean", Udt: "bool"},
	}}
	rows, _ := genAdvanced(t, meta, 77, 30, []mockgen.FieldSpec{
		{Column: "n", Generator: "integer", Params: map[string]any{"min": json.Number("18"), "max": json.Number("20")}},
		{Column: "b", Generator: "boolean", Params: map[string]any{"true_probability": json.Number("1")}},
	}, nil, nil)
	for _, r := range rows {
		if v := r[0].(int64); v < 18 || v > 20 {
			t.Fatalf("json.Number bounds ignored: %d", v)
		}
		if r[1] != true {
			t.Fatalf("json.Number probability ignored: %v", r[1])
		}
	}
}

func TestMockgenRequestValidation(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text"}}}
	ok := func(req mockgen.Request) *mockgen.Plan {
		t.Helper()
		req.Count = 3
		p, err := mockgen.BuildPlan(meta, req)
		if err != nil {
			t.Fatalf("BuildPlan(%+v): %v", req, err)
		}
		return p
	}
	ok(mockgen.Request{Mode: ""})         // empty defaults to simple
	ok(mockgen.Request{Mode: "SIMPLE"})   // case-insensitive
	ok(mockgen.Request{Mode: "Advanced"}) // case-insensitive
	bad := func(req mockgen.Request, frag string) {
		t.Helper()
		req.Count = 3
		if req.Count == 3 && frag == "count must be" {
			req.Count = 0
		}
		if frag == "too many" {
			req.Count = 20001
		}
		_, err := mockgen.BuildPlan(meta, req)
		if err == nil || !strings.Contains(err.Error(), frag) {
			t.Fatalf("want %q, got %v", frag, err)
		}
	}
	bad(mockgen.Request{Mode: "weird"}, "unknown mode")
	bad(mockgen.Request{Mode: "simple", Count: -1}, "count must be")
	bad(mockgen.Request{}, "count must be")
	bad(mockgen.Request{Mode: "simple"}, "too many")
	bad(mockgen.Request{Mode: "advanced",
		Fields: []mockgen.FieldSpec{{Column: "nope", Generator: "integer"}}}, "unknown column")
	// NULL generator on NOT NULL is rejected at plan time.
	nn := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text", Nullable: false}}}
	_, err := mockgen.BuildPlan(nn, mockgen.Request{Mode: "advanced", Count: 3,
		Fields: []mockgen.FieldSpec{{Column: "v", Generator: "null"}}})
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("want NOT NULL rejection, got %v", err)
	}
	// Nothing writable: explicit error instead of an empty INSERT.
	dd := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{{Name: "v", DataType: "text", Udt: "text", Default: strp("now()")}}}
	if _, err := mockgen.BuildPlan(dd, mockgen.Request{Mode: "simple", Count: 3}); err == nil {
		t.Fatal("simple with no writable columns should fail")
	}
	if _, err := mockgen.BuildPlan(dd, mockgen.Request{Mode: "advanced", Count: 3}); err == nil {
		t.Fatal("advanced with no writable columns should fail")
	}
	// Constraint referencing a ghost column is rejected at plan time.
	_, err = mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3,
		Constraints: []mockgen.Constraint{{Kind: "compare",
			Left: mockgen.ConstraintSide{Field: "v"}, Operator: "=",
			Right: mockgen.ConstraintSide{Field: "ghost"}}}})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want unknown-column rejection, got %v", err)
	}
}

func TestMockgenImpossibleConstraint(t *testing.T) {
	meta := mockgen.TableMeta{Columns: []mockgen.ColumnMeta{
		{Name: "a", DataType: "integer", Udt: "int4"},
		{Name: "b", DataType: "integer", Udt: "int4"},
	}}
	plan, err := mockgen.BuildPlan(meta, mockgen.Request{Mode: "advanced", Count: 3, Seed: 1, HasSeed: true,
		Fields: []mockgen.FieldSpec{
			{Column: "a", Generator: "constant", Params: map[string]any{"value": 1}},
			{Column: "b", Generator: "constant", Params: map[string]any{"value": 2}},
		},
		Constraints: []mockgen.Constraint{{Kind: "compare",
			Left: mockgen.ConstraintSide{Field: "a"}, Operator: ">",
			Right: mockgen.ConstraintSide{Field: "b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan.GenerateRows(1, 3, nil)
	if err == nil || !strings.Contains(err.Error(), "row 1") || !strings.Contains(err.Error(), "a > b") {
		t.Fatalf("want row-1 constraint error, got %v", err)
	}
}

func TestMockgenPreviewCell(t *testing.T) {
	if got := mockgen.PreviewCell([]byte{0xab, 0x01}); got != `\xab01` {
		t.Fatalf("bytea preview: %v", got)
	}
	if mockgen.PreviewCell(nil) != nil {
		t.Fatal("nil preview should stay nil")
	}
	if mockgen.PreviewCell("x") != "x" {
		t.Fatal("string preview should pass through")
	}
	if mockgen.PreviewCell(int64(7)) != int64(7) {
		t.Fatal("int preview should pass through")
	}
}

func TestMockgenSemanticHints(t *testing.T) {
	cases := map[string]string{
		" Email ": "email", "USER_EMAIL": "email",
		"firstname": "first_name", "First_Name": "first_name",
		"lastname": "last_name", "LAST_NAME": "last_name",
		"name": "full_name", "FULLNAME": "full_name", "full_name": "full_name",
		"username": "username", "user_name": "username", "login": "username",
		"mobile": "phone", "TEL": "phone", "phone_number": "phone",
		"homepage": "url", "avatar_url": "url", "website": "url", "link": "url",
		"total": "decimal", "AMOUNT": "decimal", "price": "decimal", "balance": "decimal",
		"created_at": "datetime", "updated_at": "datetime",
		"description": "", "id": "", "count": "", "rate": "",
	}
	for name, want := range cases {
		if got := mockgen.SemanticHintFor(name); got != want {
			t.Fatalf("hint(%q) = %q want %q", name, got, want)
		}
	}
}

func TestMockgenSerialAndOmit(t *testing.T) {
	if mockgen.IsSerialDefault(nil) {
		t.Fatal("nil default is not serial")
	}
	for _, d := range []string{"nextval('x'::regclass)", " NEXTVAL('x')", "nextval ('x')"} {
		dd := d
		if !mockgen.IsSerialDefault(&dd) {
			t.Fatalf("%q should be serial", d)
		}
	}
	nd := "now()"
	if mockgen.IsSerialDefault(&nd) {
		t.Fatal("now() is not serial")
	}
	e := ""
	if mockgen.IsSerialDefault(&e) {
		t.Fatal("empty default is not serial")
	}
	plain := mockgen.ColumnMeta{Name: "v", DataType: "text", Udt: "text"}
	if mockgen.OmittedInSimple(plain) {
		t.Fatal("plain column must not be omitted")
	}
	if mockgen.SemanticHintFor("") != "" {
		t.Fatal("empty name should have no hint")
	}
}

func TestMockgenEffectiveSeed(t *testing.T) {
	if mockgen.EffectiveSeed(true, 123) != 123 {
		t.Fatal("explicit seed must pass through")
	}
	// Unseeded path must at least return without drama.
	_ = mockgen.EffectiveSeed(false, 0)
}
