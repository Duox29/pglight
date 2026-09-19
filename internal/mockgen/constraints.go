package mockgen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Constraint is a structured cross-field rule. Only "compare" is supported
// in v1 — no arbitrary expressions are ever evaluated.
type Constraint struct {
	Kind     string         `json:"kind"`
	Left     ConstraintSide `json:"left"`
	Operator string         `json:"operator"`
	Right    ConstraintSide `json:"right"`
}

// ConstraintSide is either a column reference or a literal value.
type ConstraintSide struct {
	Field string `json:"field,omitempty"`
	Value any    `json:"value,omitempty"`
}

var validOps = map[string]bool{
	"=": true, "!=": true, "<>": true,
	"<": true, "<=": true, ">": true, ">=": true,
}

// ValidateRow checks every custom compare constraint against one row.
func ValidateRow(row map[string]any, constraints []Constraint) error {
	for _, c := range constraints {
		if c.Kind != "" && c.Kind != "compare" {
			return fmt.Errorf("unsupported constraint kind %q", c.Kind)
		}
		op := c.Operator
		if !validOps[op] {
			return fmt.Errorf("unsupported operator %q", op)
		}
		lv := sideValue(row, c.Left)
		rv := sideValue(row, c.Right)
		ok, err := compareOp(lv, rv, op)
		if err != nil {
			return err
		}
		if !ok {
			lName := c.Left.Field
			if lName == "" {
				lName = fmt.Sprint(c.Left.Value)
			}
			rName := c.Right.Field
			if rName == "" {
				rName = fmt.Sprint(c.Right.Value)
			}
			return &ConstraintError{Detail: fmt.Sprintf("%s %s %s", lName, op, rName)}
		}
	}
	return nil
}

// ConstraintError is a user-facing cross-field violation.
type ConstraintError struct{ Detail string }

func (e *ConstraintError) Error() string { return "constraint violated: " + e.Detail }

func sideValue(row map[string]any, s ConstraintSide) any {
	if s.Field != "" {
		return row[s.Field]
	}
	return s.Value
}

func compareOp(a, b any, op string) (bool, error) {
	if a == nil || b == nil {
		switch op {
		case "=":
			return a == nil && b == nil, nil
		case "!=", "<>":
			return !(a == nil && b == nil), nil
		default:
			return false, nil
		}
	}
	if at, aerr := toTimeLoose(a); aerr == nil {
		if bt, berr := toTimeLoose(b); berr == nil {
			return evalCmp(at.Compare(bt), op), nil
		}
	}
	if af, aok := toFloatLoose(a); aok {
		if bf, bok := toFloatLoose(b); bok {
			switch {
			case af < bf:
				return evalCmp(-1, op), nil
			case af > bf:
				return evalCmp(1, op), nil
			default:
				return evalCmp(0, op), nil
			}
		}
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	return evalCmp(strings.Compare(as, bs), op), nil
}

func evalCmp(cmp int, op string) bool {
	switch op {
	case "=":
		return cmp == 0
	case "!=", "<>":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	return false
}

func toTimeLoose(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	if s, ok := v.(string); ok {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02", "15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("not time")
}

func toFloatLoose(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// --- CHECK hint parsing ---

// CheckHint is a generator-usable reading of one CHECK definition.
type CheckHint struct {
	Column string
	Min    *float64
	Max    *float64
	Values []any
}

var (
	betweenRe = regexp.MustCompile(`(?i)(\w+)\s+BETWEEN\s+([-\d.']+)\s+AND\s+([-\d.']+)`)
	inRe      = regexp.MustCompile(`(?i)(\w+)\s+IN\s*\(([^)]+)\)`)
	anyArrRe  = regexp.MustCompile(`(?i)(\w+)\s*=\s*ANY\s*\(\s*ARRAY\s*\[([^\]]+)\]`)
	cmpRe     = regexp.MustCompile(`(?i)(\w+)\s*(>=|<=|<>|!=|=|>|<)\s*([-\d.']+)`)
)

// ParseCheckHint reads BETWEEN / comparison / IN from a CHECK definition.
// PostgreSQL normalizes some forms (BETWEEN → `x >= a AND x <= b`,
// `IN (...)` → `= ANY (ARRAY[...])`), so those spellings are accepted too.
// ok=false means unsupported — the caller must warn and let PostgreSQL
// validate during insertion.
func ParseCheckHint(def string) (hint CheckHint, kind string, ok bool) {
	if m := betweenRe.FindStringSubmatch(def); m != nil {
		lo, err1 := numLit(m[2])
		hi, err2 := numLit(m[3])
		if err1 == nil && err2 == nil {
			return CheckHint{Column: m[1], Min: &lo, Max: &hi}, "between", true
		}
	}
	if m := anyArrRe.FindStringSubmatch(def); m != nil {
		vals := splitList(m[2])
		if len(vals) > 0 {
			vs := make([]any, len(vals))
			for i, v := range vals {
				vs[i] = v
			}
			return CheckHint{Column: m[1], Values: vs}, "in", true
		}
	}
	if m := inRe.FindStringSubmatch(def); m != nil {
		vals := splitList(m[2])
		if len(vals) > 0 {
			vs := make([]any, len(vals))
			for i, v := range vals {
				vs[i] = v
			}
			return CheckHint{Column: m[1], Values: vs}, "in", true
		}
	}
	// Normalized ranges: `age >= 18 AND age <= 100`. Collect every
	// column-vs-literal comparison and fold lower+upper bounds per column.
	type bound struct {
		lower *float64
		upper *float64
	}
	bounds := map[string]*bound{}
	order := []string{}
	for _, m := range cmpRe.FindAllStringSubmatch(def, -1) {
		col, op := m[1], m[2]
		v, err := numLit(m[3])
		if err != nil {
			continue
		}
		b, seen := bounds[col]
		if !seen {
			b = &bound{}
			bounds[col] = b
			order = append(order, col)
		}
		switch op {
		case ">=":
			if b.lower == nil || v > *b.lower {
				b.lower = &v
			}
		case ">":
			vv := v + 1
			if v != float64(int64(v)) {
				vv = v + 1e-9
			}
			if b.lower == nil || vv > *b.lower {
				b.lower = &vv
			}
		case "<=":
			if b.upper == nil || v < *b.upper {
				b.upper = &v
			}
		case "<":
			vv := v - 1
			if v != float64(int64(v)) {
				vv = v - 1e-9
			}
			if b.upper == nil || vv < *b.upper {
				b.upper = &vv
			}
		case "=":
			b.lower, b.upper = &v, &v
		default:
			continue
		}
	}
	if len(order) == 0 {
		return CheckHint{}, "unsupported", false
	}
	col := order[0]
	b := bounds[col]
	h := CheckHint{Column: col, Min: b.lower, Max: b.upper}
	if b.lower != nil && b.upper != nil {
		return h, "between", true
	}
	return h, "comparison", true
}

func numLit(s string) (float64, error) {
	return strconv.ParseFloat(strings.Trim(strings.TrimSpace(s), "'\""), 64)
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := []string{}
	for _, p := range parts {
		t := strings.TrimSpace(p)
		// PostgreSQL decorates normalized ANY elements with casts
		// ('active'::text): strip the trailing ::type before unquoting.
		t = castSuffixRe.ReplaceAllString(t, "")
		t = strings.Trim(strings.TrimSpace(t), "'\"")
		t = strings.ReplaceAll(t, "''", "'")
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

var castSuffixRe = regexp.MustCompile(`::[\w\s()]+$`)

// checkHintInt / checkHintIN consult hints stashed in the RowContext by the
// engine (key "__hints"). They let Auto integer/choice generators respect
// supported CHECKs without re-parsing per row.
type intRange struct{ min, max int64 }

func checkHintInt(col string, ctx *RowContext) (intRange, bool) {
	if ctx == nil {
		return intRange{}, false
	}
	if h, ok := ctx.FK["__hint:"+col]; ok && len(h) == 2 {
		lo, a := toFloatLoose(h[0])
		hi, b := toFloatLoose(h[1])
		if a && b {
			return intRange{int64(lo), int64(hi)}, true
		}
	}
	return intRange{}, false
}

func checkHintIN(col string, ctx *RowContext) []any {
	if ctx == nil {
		return nil
	}
	if h, ok := ctx.FK["__hint_in:"+col]; ok {
		return h
	}
	return nil
}

// --- Dependency graph ---

// TopoOrder returns column names ordered so relative-datetime sources come
// before their dependents. It errors on dependency cycles.
func TopoOrder(cols []string, deps map[string]string) ([]string, error) {
	order := []string{}
	state := map[string]int{} // 0=unseen 1=in-stack 2=done
	var visit func(n string) error
	visit = func(n string) error {
		switch state[n] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("dependency cycle detected involving column %q", n)
		}
		state[n] = 1
		if src := deps[n]; src != "" {
			if err := visit(src); err != nil {
				return err
			}
		}
		state[n] = 2
		order = append(order, n)
		return nil
	}
	for _, c := range cols {
		if err := visit(c); err != nil {
			return nil, err
		}
	}
	return order, nil
}
