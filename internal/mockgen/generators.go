package mockgen

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

const alphanum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Server-side bounds for advanced generator params. The React dialog
// validates too, but the API must never trust it: unbounded lengths and
// array sizes are OOM vectors, negative sizes panic (make with negative
// capacity), and garbage numerics produce silently wrong data.
const (
	MaxStringLength = 4096
	MaxArrayItems   = 1000
	MaxChoiceValues = 1000
	// MaxOffsetDays bounds relative-datetime offsets (~1000 years).
	MaxOffsetDays = 365000
	// MaxConstraintsPerRequest bounds cross-field compare rules per plan.
	MaxConstraintsPerRequest = 64
	// MaxWeights caps per-request weight arrays (same budget as values —
	// weights are re-parsed per row unless normalized, so an unbounded
	// array is a CPU/memory amplification vector).
	MaxWeights = 1000
	// MaxConstantBytes caps one constant/choice string/JSON payload. The
	// body limit bounds input size, not generated work: a large value
	// emitted thousands of times multiplies bytes per row.
	MaxConstantBytes = 4096
)

// RowContext carries the current row's already-generated values for
// dependent generators (Relative DateTime) plus per-column sequence state.
type RowContext struct {
	Row  map[string]any
	Seq  map[string]int64
	FK   map[string][]any
	FKRR map[string]int
	Rand *rand.Rand
	// Now is the generation clock: wall time for unseeded runs, the
	// deterministic seed anchor for seeded ones.
	Now time.Time
	// FKGroups/FKColGroup describe composite FOREIGN KEY tuples (shared,
	// read-only across attempts). FKTuples holds one referenced tuple pool
	// per group name; FKTuplePick holds this attempt's chosen tuple per
	// group; FKGroupRR holds per-group sequential counters (cloned and
	// committed per attempt like Seq/FKRR, so failed rows leave no gaps).
	FKGroups    []FKGroup
	FKColGroup  map[string]int
	FKTuples    map[string][][]any
	FKTuplePick map[string][]any
	FKGroupRR   map[string]int
}

// FieldSpec is one normalized advanced field configuration.
type FieldSpec struct {
	Column    string
	Generator string
	Params    map[string]any
	Unique    bool
	NullProb  float64
	// NormWeights is the BuildPlan-normalized weight vector for choice
	// generators (parsed and capped once, reused for every row instead of
	// re-allocating per row). Nil means uniform choice.
	NormWeights      []float64
	WeightsProcessed bool
}

func randString(r *rand.Rand, minLen, maxLen int) string {
	if maxLen < minLen {
		maxLen = minLen
	}
	n := minLen
	if maxLen > minLen {
		n = minLen + r.Intn(maxLen-minLen+1)
	}
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteByte(alphanum[r.Intn(len(alphanum))])
	}
	return sb.String()
}

func randInt(r *rand.Rand, min, max int64) int64 {
	if max < min {
		min, max = max, min
	}
	// span as uint64 so the full int64 range (-9e18..9e18 and beyond)
	// cannot overflow to <= 0 and collapse to a constant min.
	span := uint64(max) - uint64(min) + 1
	if span == 0 {
		// Full 2^64 wrap: every int64 is equally likely.
		return int64(r.Uint64())
	}
	if span == 1 {
		return min
	}
	// Modulo bias is negligible for mock data and keeps this O(1).
	return min + int64(r.Uint64()%span)
}

// uuidV4 renders a deterministic RFC-4122 v4 UUID from the seeded RNG.
func uuidV4(r *rand.Rand) string {
	var b [16]byte
	for i := range b {
		b[i] = byte(r.Intn(256))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

var firstNames = []string{"Ava", "Liam", "Mia", "Noah", "Emma", "Lucas", "Sofia", "Mateo", "Yuki", "Omar", "Lena", "Diego", "Priya", "Tomas", "Nina", "Kofi"}
var lastNames = []string{"Nguyen", "Tran", "Garcia", "Smith", "Khan", "Muller", "Rossi", "Dubois", "Tanaka", "Ali", "Novak", "Silva", "Haddad", "Berg", "Costa", "Park"}
var domains = []string{"example.com", "mail.test", "sample.org"}

func pick(r *rand.Rand, xs []string) string { return xs[r.Intn(len(xs))] }

// simpleValue generates a zero-config datatype-only value. It MUST NOT use
// column names, CHECK/UNIQUE/FK constraints, or defaults — PostgreSQL is the
// final validator in Simple mode.
func simpleValue(r *rand.Rand, c ColumnMeta) (any, error) {
	return simpleValueAt(r, time.Now().UTC(), c)
}

func simpleValueAt(r *rand.Rand, now time.Time, c ColumnMeta) (any, error) {
	if len(c.EnumValues) > 0 {
		return c.EnumValues[r.Intn(len(c.EnumValues))], nil
	}
	udt := strings.ToLower(c.Udt)
	dt := strings.ToLower(c.DataType)
	isArray := dt == "array" || strings.HasPrefix(udt, "_")
	elem := strings.TrimPrefix(udt, "_")
	if isArray {
		n := r.Intn(6)
		return simpleArrayAt(r, now, elem, n)
	}
	return simpleScalarAt(r, now, c)
}

// simpleArray builds a typed slice pgx can encode into the array column:
// int arrays get []int64, floats []float64, bools []bool, else []string.
func simpleArray(r *rand.Rand, elem string, n int) (any, error) {
	return simpleArrayAt(r, time.Now().UTC(), elem, n)
}

func simpleArrayAt(r *rand.Rand, now time.Time, elem string, n int) (any, error) {
	base := ColumnMeta{Udt: elem, DataType: elemDataType(elem)}
	switch elem {
	case "int2", "int4", "int8":
		out := make([]int64, 0, n)
		for i := 0; i < n; i++ {
			v, err := simpleScalarAt(r, now, base)
			if err != nil {
				return nil, err
			}
			out = append(out, toInt64(v))
		}
		return out, nil
	case "float4", "float8", "numeric", "decimal":
		out := make([]float64, 0, n)
		for i := 0; i < n; i++ {
			v, err := simpleScalarAt(r, now, base)
			if err != nil {
				return nil, err
			}
			out = append(out, toFloat64(v))
		}
		return out, nil
	case "bool":
		out := make([]bool, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, r.Intn(2) == 0)
		}
		return out, nil
	default:
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			v, err := simpleScalarAt(r, now, base)
			if err != nil {
				return nil, err
			}
			out = append(out, fmt.Sprint(v))
		}
		return out, nil
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case string:
		var f float64
		_, _ = fmt.Sscan(n, &f)
		return f
	default:
		return 0
	}
}

func elemDataType(elem string) string {
	switch elem {
	case "int2":
		return "smallint"
	case "int4":
		return "integer"
	case "int8":
		return "bigint"
	case "float4":
		return "real"
	case "float8":
		return "double precision"
	case "bool":
		return "boolean"
	case "text":
		return "text"
	case "varchar":
		return "character varying"
	case "uuid":
		return "uuid"
	default:
		return elem
	}
}

func simpleScalar(r *rand.Rand, c ColumnMeta) (any, error) {
	return simpleScalarAt(r, time.Now().UTC(), c)
}

func simpleScalarAt(r *rand.Rand, now time.Time, c ColumnMeta) (any, error) {
	udt := strings.ToLower(c.Udt)
	fiveYears := 5 * 365 * 24 * time.Hour
	switch udt {
	case "int2", "int4", "int8":
		return randInt(r, 0, 10000), nil
	case "numeric", "decimal":
		return fmt.Sprintf("%.2f", float64(randInt(r, 0, 1000000))/100.0), nil
	case "float4", "float8":
		return float64(randInt(r, 0, 1000000)) / 100.0, nil
	case "text", "varchar", "bpchar", "char", "name":
		return randString(r, 8, 24), nil
	case "bool":
		return r.Intn(2) == 0, nil
	case "date":
		d := now.Add(-time.Duration(r.Int63n(int64(fiveYears))))
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), nil
	case "timestamp", "timestamptz":
		return now.Add(-time.Duration(r.Int63n(int64(fiveYears)))).Truncate(time.Microsecond), nil
	case "time", "timetz":
		return fmt.Sprintf("%02d:%02d:%02d", r.Intn(24), r.Intn(60), r.Intn(60)), nil
	case "uuid":
		return uuidV4(r), nil
	case "json", "jsonb":
		return fmt.Sprintf(`{"key":%q}`, randString(r, 8, 16)), nil
	case "bytea":
		n := 4 + r.Intn(12)
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(r.Intn(256))
		}
		return b, nil
	default:
		if strings.Contains(udt, "int") {
			return randInt(r, 0, 10000), nil
		}
		if strings.Contains(udt, "char") || strings.Contains(udt, "text") {
			return randString(r, 8, 24), nil
		}
		if strings.Contains(udt, "time") {
			return now.Add(-time.Duration(r.Int63n(int64(fiveYears)))).Truncate(time.Microsecond), nil
		}
		return randString(r, 8, 24), nil
	}
}

// --- Advanced generator dispatch ---

func paramFloat(p map[string]any, key string, def float64) float64 {
	if p == nil {
		return def
	}
	switch v := p[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
		return def
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
		return def
	default:
		return def
	}
}

func paramInt(p map[string]any, key string, def int) int {
	return int(paramFloat(p, key, float64(def)))
}

func paramString(p map[string]any, key string, def string) string {
	if p == nil {
		return def
	}
	if s, ok := p[key].(string); ok {
		return s
	}
	return def
}

// toFloatParam converts one raw JSON param to float64, reporting whether it
// was present and finite. Strings are parsed (so exact integer strings keep
// working); NaN/Inf are rejected.
func toFloatParam(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, true
		}
		return 0, false
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// exactInt converts one raw JSON param to int64 without float64 rounding:
// json.Number and strings go through ParseInt (exact, so 9007199254740993
// stays 9007199254740993 instead of collapsing to ...992); Go ints pass
// through; float64 must already be integral and in range.
func exactInt(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, nil
		}
		return 0, fmt.Errorf("must be an integer")
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			return 0, fmt.Errorf("must be an integer")
		}
		if n < -9.223372036854776e18 || n > 9.223372036854776e18 {
			return 0, fmt.Errorf("out of int64 range")
		}
		return int64(n), nil
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return 0, fmt.Errorf("must be an integer")
		}
		return int64(f), nil
	case string:
		t := strings.TrimSpace(n)
		if t == "" {
			return 0, fmt.Errorf("must be an integer")
		}
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i, nil
		}
		return 0, fmt.Errorf("must be an integer")
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

// intTypeBounds returns the valid range for an integer column type
// (int2/int4/int8 by UDT); non-integer columns accept the full int64 range.
func intTypeBounds(c ColumnMeta) (int64, int64) {
	switch strings.ToLower(strings.TrimSpace(c.Udt)) {
	case "int2":
		return -32768, 32767
	case "int4":
		return -2147483648, 2147483647
	default:
		return math.MinInt64, math.MaxInt64
	}
}

// exactIntForKey parses an integral param exactly (no float64 round-trip).
func exactIntForKey(p map[string]any, key string) (int64, bool, error) {
	v, present := rawParam(p, key)
	if !present {
		return 0, false, nil
	}
	i, err := exactInt(v)
	if err != nil {
		return 0, true, fmt.Errorf("param %q %v", key, err)
	}
	return i, true, nil
}

// intParam validates an integral param: present, integral (exact, no
// float64 precision loss), in [min, max].
func intParam(p map[string]any, key string, min, max int64) (int64, bool, error) {
	v, present := rawParam(p, key)
	if !present {
		return 0, false, nil
	}
	i, err := exactInt(v)
	if err != nil {
		return 0, true, fmt.Errorf("param %q %v", key, err)
	}
	if i < min || i > max {
		return 0, true, fmt.Errorf("param %q out of range [%d, %d]", key, min, max)
	}
	return i, true, nil
}

// colIntType names the integer width for validation messages.
func colIntType(c ColumnMeta) string {
	switch strings.ToLower(strings.TrimSpace(c.Udt)) {
	case "int2":
		return "int2"
	case "int4":
		return "int4"
	default:
		return "int64"
	}
}

// checkValueSizes caps individual constant/choice payloads: one huge string
// emitted per row multiplies input bytes into output bytes (amplification).
func checkValueSizes(vals []any) error {
	for _, v := range vals {
		var n int
		switch t := v.(type) {
		case string:
			n = len(t)
		case json.Number:
			n = len(string(t))
		default:
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			n = len(b)
		}
		if n > MaxConstantBytes {
			return fmt.Errorf("value exceeds %d bytes (max %d)", n, MaxConstantBytes)
		}
	}
	return nil
}

func rawParam(p map[string]any, key string) (any, bool) {
	if p == nil {
		return nil, false
	}
	v, ok := p[key]
	return v, ok
}

// validateFieldParams rejects dangerous or nonsensical advanced params at
// plan time (fail fast as 400), instead of panicking or OOMing per row.
// Benign legacy tolerances (scale clamping, weight fallback) are preserved.
func validateFieldParams(c ColumnMeta, f FieldSpec) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("column %q: %s", c.Name, fmt.Sprintf(format, args...))
	}
	if f.NullProb < 0 || f.NullProb > 1 || math.IsNaN(f.NullProb) {
		return fail("null_probability must be between 0 and 1")
	}
	gen := strings.ToLower(strings.TrimSpace(f.Generator))
	floatOK := func(key string) (float64, bool, error) {
		v, present := rawParam(f.Params, key)
		if !present {
			return 0, false, nil
		}
		fv, ok := toFloatParam(v)
		if !ok {
			return 0, true, fail("param %q must be a finite number", key)
		}
		return fv, true, nil
	}
	switch gen {
	case "integer":
		lo, hi := intTypeBounds(c)
		minV, hasMin, err := exactIntForKey(f.Params, "min")
		if err != nil {
			return fail("%v", err)
		}
		maxV, hasMax, err := exactIntForKey(f.Params, "max")
		if err != nil {
			return fail("%v", err)
		}
		if hasMin && (minV < lo || minV > hi) {
			return fail("param \"min\" out of %s range [%d, %d]", colIntType(c), lo, hi)
		}
		if hasMax && (maxV < lo || maxV > hi) {
			return fail("param \"max\" out of %s range [%d, %d]", colIntType(c), lo, hi)
		}
		if hasMin && hasMax && maxV < minV {
			return fail("param \"max\" must not be less than \"min\"")
		}
	case "decimal":
		minV, hasMin, err := floatOK("min")
		if err != nil {
			return err
		} else if hasMin && (math.IsNaN(minV) || math.IsInf(minV, 0)) {
			return fail("param \"min\" must be a finite number")
		}
		maxV, hasMax, err := floatOK("max")
		if err != nil {
			return err
		} else if hasMax && (math.IsNaN(maxV) || math.IsInf(maxV, 0)) {
			return fail("param \"max\" must be a finite number")
		}
		if hasMin && hasMax && maxV < minV {
			return fail("param \"max\" must not be less than \"min\"")
		}
		// Scale keeps its legacy clamp (generation clamps to 0..10);
		// validation only requires an exact integer.
		if _, _, err := exactIntForKey(f.Params, "scale"); err != nil {
			return fail("%v", err)
		}
	case "boolean":
		if v, present, err := floatOK("true_probability"); err != nil {
			return err
		} else if present && (v < 0 || v > 1) {
			return fail("true_probability must be between 0 and 1")
		}
	case "string":
		minL, _, err := intParam(f.Params, "min_length", 0, MaxStringLength)
		if err != nil {
			return fail("%v", err)
		}
		maxL, maxSet, err := intParam(f.Params, "max_length", 0, MaxStringLength)
		if err != nil {
			return fail("%v", err)
		}
		if _, minSet := rawParam(f.Params, "min_length"); minSet && maxSet && maxL < minL {
			return fail("min_length must not exceed max_length")
		}
	case "array":
		minI, _, err := intParam(f.Params, "min_items", 0, MaxArrayItems)
		if err != nil {
			return fail("%v", err)
		}
		maxI, maxSet, err := intParam(f.Params, "max_items", 0, MaxArrayItems)
		if err != nil {
			return fail("%v", err)
		}
		if _, minSet := rawParam(f.Params, "min_items"); minSet && maxSet && maxI < minI {
			return fail("min_items must not exceed max_items")
		}
	case "choice":
		vals := paramValues(f.Params)
		if vals != nil && len(vals) > MaxChoiceValues {
			return fail("too many choice values (max %d)", MaxChoiceValues)
		}
		if err := checkValueSizes(vals); err != nil {
			return fail("%v", err)
		}
		if raw, ok := f.Params["weights"].([]any); ok && len(raw) > MaxWeights {
			return fail("too many weights (max %d)", MaxWeights)
		}
		if w, present := rawParam(f.Params, "weights"); present {
			if _, ok := w.([]any); !ok {
				return fail("param \"weights\" must be an array of numbers")
			}
		}
	case "constant":
		if v, ok := rawParam(f.Params, "value"); ok {
			if err := checkValueSizes([]any{v}); err != nil {
				return fail("%v", err)
			}
		}
	case "sequence":
		if _, _, err := exactIntForKey(f.Params, "start"); err != nil {
			return fail("%v", err)
		}
		// step 0 keeps its legacy tolerance (generation treats it as 1).
		if _, _, err := exactIntForKey(f.Params, "step"); err != nil {
			return fail("%v", err)
		}
	case "relative_datetime":
		minO, hasMin, err := floatOK("min_offset_days")
		if err != nil {
			return err
		}
		if hasMin && math.Abs(minO) > MaxOffsetDays {
			return fail("param %q magnitude exceeds %d days", "min_offset_days", MaxOffsetDays)
		}
		maxO, hasMax, err := floatOK("max_offset_days")
		if err != nil {
			return err
		}
		if hasMax && math.Abs(maxO) > MaxOffsetDays {
			return fail("param %q magnitude exceeds %d days", "max_offset_days", MaxOffsetDays)
		}
		if hasMin && hasMax && maxO < minO {
			return fail("param \"max_offset_days\" must not be less than \"min_offset_days\"")
		}
	}
	return nil
}

// generateField produces one value for an advanced field. FK pools come from
// ctx.FK (fetched by the API layer); relative generators read ctx.Row.
func generateField(ctx *RowContext, c ColumnMeta, f FieldSpec) (any, error) {
	r := ctx.Rand
	gen := strings.ToLower(strings.TrimSpace(f.Generator))
	if gen == "" || gen == "auto" {
		gen = autoGenerator(c)
	}
	switch gen {
	case "db_default":
		return nil, errOmit
	case "null":
		return nil, nil
	case "constant":
		if f.Params != nil {
			if v, ok := f.Params["value"]; ok {
				return v, nil
			}
		}
		return nil, nil
	case "integer":
		min, max := fieldIntRange(f.Params, c, 0, 10000)
		_, hasMin := f.Params["min"]
		_, hasMax := f.Params["max"]
		// An inferred IN set is more specific than a range: pick from it
		// when the user gave no explicit bounds.
		if !hasMin && !hasMax {
			if inVals := checkHintIN(c.Name, ctx); len(inVals) > 0 {
				if ints := parseIntSet(inVals); len(ints) > 0 {
					return ints[r.Intn(len(ints))], nil
				}
			}
		}
		if v, ok := checkHintInt(c.Name, ctx); ok {
			if !hasMin {
				min = v.min
			}
			if !hasMax {
				max = v.max
			}
		}
		return randInt(r, min, max), nil
	case "decimal":
		min := paramFloat(f.Params, "min", 0)
		max := paramFloat(f.Params, "max", 10000)
		// Honor inferred CHECK ranges (e.g. price BETWEEN 1 AND 10) when
		// the user gave no explicit bounds — previously ignored.
		if _, has := f.Params["min"]; !has {
			if lo, _, ok := checkHintFloat(c.Name, ctx); ok {
				min = lo
			}
		}
		if _, has := f.Params["max"]; !has {
			if _, hi, ok := checkHintFloat(c.Name, ctx); ok {
				max = hi
			}
		}
		scale := paramInt(f.Params, "scale", 2)
		if scale < 0 {
			scale = 0
		}
		if scale > 10 {
			scale = 10
		}
		v := min
		if max > min {
			v = min + r.Float64()*(max-min)
		}
		return fmt.Sprintf("%.*f", scale, v), nil
	case "boolean":
		tp := paramFloat(f.Params, "true_probability", 0.5)
		return r.Float64() < tp, nil
	case "string":
		// A supported IN check narrows free text to the allowed values
		// (e.g. Auto on a status column with CHECK IN (...)).
		if hintVals := checkHintIN(c.Name, ctx); len(hintVals) > 0 {
			return weightedPick(r, hintVals, fieldWeights(f, hintVals)), nil
		}
		minL := paramInt(f.Params, "min_length", 8)
		maxL := paramInt(f.Params, "max_length", 24)
		return randString(r, minL, maxL), nil
	case "uuid":
		return uuidV4(r), nil
	case "date":
		min, max := paramDateRange(f.Params, "min", "max", 5*365*24*time.Hour, ctx.Now)
		return randTime(r, min, max), nil
	case "datetime":
		min, max := paramDateRange(f.Params, "min", "max", 5*365*24*time.Hour, ctx.Now)
		return randTime(r, min, max), nil
	case "time":
		return fmt.Sprintf("%02d:%02d:%02d", r.Intn(24), r.Intn(60), r.Intn(60)), nil
	case "choice":
		vals := paramValues(f.Params)
		if len(vals) == 0 {
			vals = checkHintIN(c.Name, ctx)
		}
		if len(vals) == 0 && len(c.EnumValues) > 0 {
			vals = make([]any, len(c.EnumValues))
			for i, v := range c.EnumValues {
				vals[i] = v
			}
		}
		if len(vals) == 0 {
			vals = []any{"a", "b", "c"}
		}
		return weightedPick(r, vals, fieldWeights(f, vals)), nil
	case "sequence":
		start, _ := fieldExactInt(f.Params, "start", 1)
		step, _ := fieldExactInt(f.Params, "step", 1)
		if step == 0 {
			step = 1
		}
		// Checked arithmetic: start + counter*step must not silently wrap.
		prod, ok := checkedMul(ctx.Seq[c.Name], step)
		if !ok {
			return nil, fmt.Errorf("sequence overflow for column %q", c.Name)
		}
		cur, ok := checkedAdd(start, prod)
		if !ok {
			return nil, fmt.Errorf("sequence overflow for column %q", c.Name)
		}
		ctx.Seq[c.Name]++
		return cur, nil
	case "json":
		return fmt.Sprintf(`{"key":%q}`, randString(r, 8, 16)), nil
	case "array":
		minI := paramInt(f.Params, "min_items", 0)
		maxI := paramInt(f.Params, "max_items", 5)
		n := minI
		if maxI > minI {
			n = minI + r.Intn(maxI-minI+1)
		}
		elem := strings.TrimPrefix(strings.ToLower(c.Udt), "_")
		if strings.ToLower(c.DataType) != "array" && !strings.HasPrefix(strings.ToLower(c.Udt), "_") {
			elem = "text"
		}
		return simpleArrayAt(r, ctx.Now, elem, n)
	case "email":
		return fmt.Sprintf("%s%s@%s", strings.ToLower(pick(r, firstNames)), randString(r, 2, 5), pick(r, domains)), nil
	case "first_name":
		return pick(r, firstNames), nil
	case "last_name":
		return pick(r, lastNames), nil
	case "full_name":
		return pick(r, firstNames) + " " + pick(r, lastNames), nil
	case "username":
		return strings.ToLower(pick(r, firstNames)) + "_" + randString(r, 3, 6), nil
	case "phone":
		return fmt.Sprintf("+1-%03d-%03d-%04d", r.Intn(800)+200, r.Intn(800)+200, r.Intn(10000)), nil
	case "url":
		return fmt.Sprintf("https://%s/%s", pick(r, domains), randString(r, 4, 12)), nil
	case "foreign_key":
		// Composite keys read one tuple position per row so members can
		// never mix into combinations absent from the parent. An explicit
		// ref_* source override opts a column back out to the single pool.
		if ctx != nil && len(ctx.FKGroups) > 0 && !hasExplicitRefParams(f.Params) {
			if gi, ok := ctx.FKColGroup[c.Name]; ok && gi >= 0 && gi < len(ctx.FKGroups) {
				g := ctx.FKGroups[gi]
				if len(g.Local) > 1 {
					if pick, ok := ctx.FKTuplePick[g.Name]; ok {
						for i, lc := range g.Local {
							if lc == c.Name && i < len(pick) {
								return pick[i], nil
							}
						}
					}
					return nil, fmt.Errorf("no_fk_values")
				}
			}
		}
		pool := ctx.FK[c.Name]
		if len(pool) == 0 {
			return nil, fmt.Errorf("no_fk_values")
		}
		if paramString(f.Params, "mode", "random") == "sequential" {
			i := ctx.FKRR[c.Name] % len(pool)
			ctx.FKRR[c.Name]++
			return pool[i], nil
		}
		return pool[r.Intn(len(pool))], nil
	case "relative_datetime":
		src := paramString(f.Params, "source", "")
		base, ok := ctx.Row[src]
		if !ok || base == nil {
			return nil, fmt.Errorf("relative source %q not generated yet", src)
		}
		bt, err := toTime(base)
		if err != nil {
			return nil, fmt.Errorf("relative source %q: %v", src, err)
		}
		minOff := paramFloat(f.Params, "min_offset_days", 0)
		maxOff := paramFloat(f.Params, "max_offset_days", 30)
		off := minOff
		if maxOff > minOff {
			off = minOff + r.Float64()*(maxOff-minOff)
		}
		return addOffsetDays(bt, off).Truncate(time.Microsecond), nil
	default:
		return simpleScalarAt(r, ctx.Now, c)
	}
}

type errOmitType struct{}

func (errOmitType) Error() string { return "omit column (database default)" }

var errOmit error = errOmitType{}

// autoGenerator resolves Advanced "auto" without touching user config.
func autoGenerator(c ColumnMeta) string {
	if c.Identity || c.Generated || IsSerialDefault(c.Default) {
		return "db_default"
	}
	if len(c.EnumValues) > 0 {
		return "choice"
	}
	switch SemanticHintFor(c.Name) {
	case "email":
		return "email"
	case "first_name":
		return "first_name"
	case "last_name":
		return "last_name"
	case "full_name":
		return "full_name"
	case "username":
		return "username"
	case "phone":
		return "phone"
	case "url":
		return "url"
	case "decimal":
		return "decimal"
	case "datetime":
		return "datetime"
	}
	udt := strings.ToLower(c.Udt)
	switch udt {
	case "int2", "int4", "int8":
		return "integer"
	case "numeric", "decimal", "float4", "float8":
		return "decimal"
	case "bool":
		return "boolean"
	case "uuid":
		return "uuid"
	case "date":
		return "date"
	case "timestamp", "timestamptz":
		return "datetime"
	case "time", "timetz":
		return "time"
	case "json", "jsonb":
		return "json"
	case "bytea":
		return "string"
	default:
		return "string"
	}
}

func paramValues(p map[string]any) []any {
	if p == nil {
		return nil
	}
	if v, ok := p["values"].([]any); ok {
		return v
	}
	if v, ok := p["values"].([]string); ok {
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	}
	return nil
}

func paramWeights(p map[string]any) []float64 {
	if p == nil {
		return nil
	}
	raw, ok := p["weights"].([]any)
	if !ok {
		return nil
	}
	// Cap before allocating: an unbounded weights array with mismatched
	// values would otherwise allocate per row and fall back to uniform
	// anyway (amplification without effect).
	if len(raw) > MaxWeights {
		return nil
	}
	out := make([]float64, len(raw))
	for i, v := range raw {
		switch n := v.(type) {
		case float64:
			out[i] = clampWeight(n)
		case float32:
			out[i] = clampWeight(float64(n))
		case json.Number:
			if f, err := n.Float64(); err == nil {
				out[i] = clampWeight(f)
			} else {
				out[i] = 1
			}
		case int:
			out[i] = clampWeight(float64(n))
		case int64:
			out[i] = clampWeight(float64(n))
		default:
			out[i] = 1
		}
	}
	return out
}

// clampWeight keeps weighted choice total-based: non-finite becomes 1,
// negatives become 0 (a zero total falls back to uniform in weightedPick).
func clampWeight(w float64) float64 {
	if math.IsNaN(w) || math.IsInf(w, 0) {
		return 1
	}
	if w < 0 {
		return 0
	}
	return w
}

// fieldWeights prefers the BuildPlan-normalized vector (parsed once per
// request) and falls back to per-row parsing for hand-built FieldSpecs in
// tests. Either way a length mismatch means uniform choice.
func fieldWeights(f FieldSpec, vals []any) []float64 {
	if f.WeightsProcessed {
		if len(f.NormWeights) == len(vals) {
			return f.NormWeights
		}
		return nil
	}
	return paramWeights(f.Params)
}

// normalizeWeights parses and caps the weights array once at plan time.
func normalizeWeights(p map[string]any) []float64 {
	w := paramWeights(p)
	if len(w) > MaxWeights {
		return nil
	}
	return w
}

// fieldExactInt reads an integral generator param exactly (no float64
// truncation of 1.9 → 1); def applies when absent or unparseable at
// generation time (plan validation already rejected bad input).
func fieldExactInt(p map[string]any, key string, def int64) (int64, bool) {
	v, ok := rawParam(p, key)
	if !ok {
		return def, false
	}
	if i, err := exactInt(v); err == nil {
		return i, true
	}
	return def, false
}

// fieldIntRange resolves integer min/max exactly against column bounds.
func fieldIntRange(p map[string]any, c ColumnMeta, defMin, defMax int64) (int64, int64) {
	min, _ := fieldExactInt(p, "min", defMin)
	max, _ := fieldExactInt(p, "max", defMax)
	return min, max
}

// checkedMul/checkedAdd perform overflow-checked int64 arithmetic for
// sequence generation (start + counter*step).
func checkedMul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	r := a * b
	if r/b != a {
		return 0, false
	}
	// Multiplication of MinInt64 by -1 wraps without the division check
	// catching it on some platforms — guard explicitly.
	if a == math.MinInt64 && b == -1 || b == math.MinInt64 && a == -1 {
		return 0, false
	}
	return r, true
}

func checkedAdd(a, b int64) (int64, bool) {
	r := a + b
	if (b > 0 && r < a) || (b < 0 && r > a) {
		return 0, false
	}
	return r, true
}

// addOffsetDays adds a (possibly fractional, possibly huge) day offset
// without overflowing time.Duration (~292 years max): whole days go through
// AddDate (calendar arithmetic, no Duration involved) and only the
// sub-day remainder uses time.Duration.
func addOffsetDays(base time.Time, off float64) time.Time {
	if math.IsNaN(off) || math.IsInf(off, 0) {
		return base
	}
	whole := math.Trunc(off)
	frac := off - whole
	out := base
	if whole != 0 {
		d := int(whole)
		out = out.AddDate(0, 0, d)
	}
	if frac != 0 {
		out = out.Add(time.Duration(frac * float64(24*time.Hour)))
	}
	return out
}

// hasAnyRefParam reports any ref_* pinning (even partial), for the
// composite-FK unit rule: a half-pinned override is still an override.
func hasAnyRefParam(p map[string]any) bool {
	if p == nil {
		return false
	}
	for _, k := range []string{"ref_schema", "ref_table", "ref_column"} {
		if s, _ := p[k].(string); strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// hasExplicitRefParams reports a pinned FK source (the dialog's FK source
// picker), which opts a composite member out of tuple generation.
func hasExplicitRefParams(p map[string]any) bool {
	if p == nil {
		return false
	}
	for _, k := range []string{"ref_schema", "ref_table", "ref_column"} {
		s, _ := p[k].(string)
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}

// parseIntSet converts inferred IN values to int64s, skipping non-integral
// entries. Empty means "no usable set" — the caller falls back to ranges.
func parseIntSet(vals []any) []int64 {
	out := []int64{}
	for _, v := range vals {
		s := strings.TrimSpace(fmt.Sprint(v))
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func weightedPick(r *rand.Rand, vals []any, weights []float64) any {
	if len(weights) != len(vals) {
		return vals[r.Intn(len(vals))]
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return vals[r.Intn(len(vals))]
	}
	x := r.Float64() * total
	for i, w := range weights {
		x -= w
		if x <= 0 {
			return vals[i]
		}
	}
	return vals[len(vals)-1]
}

func paramDateRange(p map[string]any, minKey, maxKey string, span time.Duration, now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	defMin := now.Add(-span)
	defMax := now
	min := parseTimeOr(paramString(p, minKey, ""), defMin)
	max := parseTimeOr(paramString(p, maxKey, ""), defMax)
	if max.Before(min) {
		min, max = max, min
	}
	return min, max
}

func parseTimeOr(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return def
}

func randTime(r *rand.Rand, min, max time.Time) time.Time {
	if !max.After(min) {
		return min.Truncate(time.Microsecond)
	}
	span := max.Sub(min)
	return min.Add(time.Duration(r.Int63n(int64(span)))).Truncate(time.Microsecond)
}

func toTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02", "15:04:05"} {
			if tm, err := time.Parse(layout, t); err == nil {
				return tm, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse time %q", t)
	default:
		return time.Time{}, fmt.Errorf("not a datetime")
	}
}
