package mockgen

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

const alphanum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

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
}

// FieldSpec is one normalized advanced field configuration.
type FieldSpec struct {
	Column    string
	Generator string
	Params    map[string]any
	Unique    bool
	NullProb  float64
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
	span := max - min + 1
	if span <= 0 {
		return min
	}
	return min + r.Int63n(span)
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
		min := int64(paramFloat(f.Params, "min", 0))
		max := int64(paramFloat(f.Params, "max", 10000))
		if v, ok := checkHintInt(c.Name, ctx); ok {
			if _, has := f.Params["min"]; !has {
				min = v.min
			}
			if _, has := f.Params["max"]; !has {
				max = v.max
			}
		}
		return randInt(r, min, max), nil
	case "decimal":
		min := paramFloat(f.Params, "min", 0)
		max := paramFloat(f.Params, "max", 10000)
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
			return weightedPick(r, hintVals, paramWeights(f.Params)), nil
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
		return weightedPick(r, vals, paramWeights(f.Params)), nil
	case "sequence":
		start := int64(paramFloat(f.Params, "start", 1))
		step := int64(paramFloat(f.Params, "step", 1))
		if step == 0 {
			step = 1
		}
		cur := start + ctx.Seq[c.Name]*step
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
		return bt.Add(time.Duration(off * float64(24*time.Hour))).Truncate(time.Microsecond), nil
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
	out := make([]float64, len(raw))
	for i, v := range raw {
		switch n := v.(type) {
		case float64:
			out[i] = n
		case json.Number:
			if f, err := n.Float64(); err == nil {
				out[i] = f
			} else {
				out[i] = 1
			}
		case int:
			out[i] = float64(n)
		case int64:
			out[i] = float64(n)
		default:
			out[i] = 1
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
