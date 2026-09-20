package mockgen

import (
	"encoding/json"
	"fmt"
	"math/big"
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
	if ar, aok := numericRat(a); aok {
		if br, bok := numericRat(b); bok {
			return evalCmp(ar.Cmp(br), op), nil
		}
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	return evalCmp(strings.Compare(as, bs), op), nil
}

// numericRat keeps integer and decimal comparisons exact; in particular it
// avoids rounding json.Number/int64 values through float64.
func numericRat(v any) (*big.Rat, bool) {
	var s string
	switch n := v.(type) {
	case int:
		s = strconv.FormatInt(int64(n), 10)
	case int8:
		s = strconv.FormatInt(int64(n), 10)
	case int16:
		s = strconv.FormatInt(int64(n), 10)
	case int32:
		s = strconv.FormatInt(int64(n), 10)
	case int64:
		s = strconv.FormatInt(n, 10)
	case uint:
		s = strconv.FormatUint(uint64(n), 10)
	case uint8:
		s = strconv.FormatUint(uint64(n), 10)
	case uint16:
		s = strconv.FormatUint(uint64(n), 10)
	case uint32:
		s = strconv.FormatUint(uint64(n), 10)
	case uint64:
		s = strconv.FormatUint(n, 10)
	case json.Number:
		s = n.String()
	case float32:
		s = strconv.FormatFloat(float64(n), 'g', -1, 32)
	case float64:
		s = strconv.FormatFloat(n, 'g', -1, 64)
	case string:
		s = strings.TrimSpace(n)
	default:
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
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
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
		// Integer-looking literals that overflow float64 parsing keep
		// exactness via ParseInt (e.g. HTTP literal 10 vs generated 2 must
		// compare numerically, never lexicographically as "2" vs "10").
		if i, err := n.Int64(); err == nil {
			return float64(i), true
		}
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
	MinRaw *string
	MaxRaw *string
	Values []any
}

func compareNumericLiteral(a, b string) (int, bool) {
	ar, ok := new(big.Rat).SetString(cleanNum(a))
	if !ok {
		return 0, false
	}
	br, ok := new(big.Rat).SetString(cleanNum(b))
	if !ok {
		return 0, false
	}
	return ar.Cmp(br), true
}

var (
	betweenRe = regexp.MustCompile(`(?i)(\w+)\s+BETWEEN\s+([-\d.']+)\s+AND\s+([-\d.']+)`)
	inRe      = regexp.MustCompile(`(?i)(\w+)\s+IN\s*\(([^)]+)\)`)
	anyArrRe  = regexp.MustCompile(`(?i)(\w+)\s*=\s*ANY\s*\(\s*ARRAY\s*\[([^\]]+)\]`)
	cmpRe     = regexp.MustCompile(`(?i)(\w+)\s*(>=|<=|<>|!=|=|>|<)\s*([-\d.']+)`)
	// orRe/notRe/caseRe/funcRe reject expressions outside the supported
	// single-column grammar: inference must be conservative (false
	// negatives), because a false positive generates rows PostgreSQL
	// rejects — e.g. CHECK (x < 0 OR x > 100) must NOT fold to a range.
	orRe   = regexp.MustCompile(`(?i)\bOR\b`)
	notRe  = regexp.MustCompile(`(?i)\bNOT\b`)
	caseRe = regexp.MustCompile(`(?i)\bCASE\b`)
	// A function call is an identifier followed by '('. The IN/ANY/ARRAY
	// keywords are part of the supported spellings and don't count
	// (filtered in hasFuncCall — RE2 has no lookahead).
	funcCallRe = regexp.MustCompile(`(?i)\b([A-Za-z_]\w*)\s*\(`)
)

// hasFuncCall reports identifier-paren calls other than the CHECK wrapper,
// the AND joiner of normalized ranges, and the IN/ANY/ARRAY keywords of
// the supported spellings. (OR/NOT/CASE are rejected globally above.)
func hasFuncCall(s string) bool {
	for _, m := range funcCallRe.FindAllStringSubmatch(s, -1) {
		switch strings.ToUpper(m[1]) {
		case "CHECK", "AND", "IN", "ANY", "ARRAY":
			continue
		default:
			return true
		}
	}
	return false
}

// ParseCheckHint reads BETWEEN / comparison / IN from a CHECK definition.
// PostgreSQL normalizes some forms (BETWEEN → `x >= a AND x <= b`,
// `IN (...)` → `= ANY (ARRAY[...])`), so those spellings are accepted too.
// ok=false means unsupported — the caller must warn and let PostgreSQL
// validate during insertion.
func ParseCheckHint(def string) (hint CheckHint, kind string, ok bool) {
	return parseCheckHint(def, nil)
}

// ParseCheckHintForColumns is ParseCheckHint plus a cross-column guard: when
// the table's column names are known, any supported-looking hint that also
// mentions another column is rejected instead of partially inferred (e.g.
// CHECK (age > 18 AND enabled = true) must not silently drop `enabled`).
func ParseCheckHintForColumns(def string, columns []string) (hint CheckHint, kind string, ok bool) {
	return parseCheckHint(def, columns)
}

func parseCheckHint(def string, columns []string) (hint CheckHint, kind string, ok bool) {
	unsupported := func() (CheckHint, string, bool) { return CheckHint{}, "unsupported", false }
	// Global rejects: the whole expression must fit the supported grammar.
	// String literals are stripped first so literal text ('or', 'notorious')
	// can't veto an otherwise supported hint.
	nolit := stripStringLiterals(def)
	masked := maskStringLiterals(def)
	if orRe.MatchString(nolit) || notRe.MatchString(nolit) || caseRe.MatchString(nolit) {
		return unsupported()
	}
	if hasFuncCall(nolit) {
		return unsupported()
	}
	// Strict full-consumption grammar (v1): the ENTIRE expression must be a
	// conjunction of supported single-column predicates. Substring matching
	// that extracts one supported fragment while ignoring an unsupported
	// same-column predicate (x BETWEEN 1 AND 10 AND x <> 5, x IN (1,2) AND
	// x > 1, x >= 0 AND x % 2 = 0) generates rows PostgreSQL rejects.
	// Any unconsumed term — including <> / != / modulo — means unsupported.
	// Structure is validated on the literal-masked text (each '...' becomes
	// a dummy 0, so commas/keywords inside literals can't shape parsing);
	// IN/ANY values are extracted from the original definition afterwards.
	terms, terr := splitCheckTerms(masked)
	if terr != nil {
		return unsupported()
	}
	if len(terms) == 0 {
		return unsupported()
	}
	type termKind int
	const (
		tBetween termKind = iota
		tIn
		tAny
		tCmp
	)
	type term struct {
		kind   termKind
		col    string
		lo     float64
		hi     float64
		vals   []any
		op     string
		val    float64
		loRaw  string
		hiRaw  string
		valRaw string
	}
	betweenFullRe := regexp.MustCompile(`(?i)^\s*(\w+)\s+BETWEEN\s+([-\d.']+)\s+AND\s+([-\d.']+)\s*$`)
	inFullRe := regexp.MustCompile(`(?i)^\s*(\w+)\s+IN\s*\(([^)]+)\)\s*$`)
	anyFullRe := regexp.MustCompile(`(?i)^\s*(\w+)\s*=\s*ANY\s*\(\s*ARRAY\s*\[([^\]]+)\]\s*\)\s*$`)
	cmpFullRe := regexp.MustCompile(`(?i)^\s*(\w+)\s*(>=|<=|>|<|=)\s*([-\d.']+)\s*$`)
	parsed := make([]term, 0, len(terms))
	for _, t := range terms {
		tt := strings.TrimSpace(t)
		// Strip one layer of redundant parens per term.
		for len(tt) >= 2 && strings.HasPrefix(tt, "(") && strings.HasSuffix(tt, ")") {
			tt = strings.TrimSpace(tt[1 : len(tt)-1])
		}
		if tt == "" {
			return unsupported()
		}
		if m := betweenFullRe.FindStringSubmatch(tt); m != nil {
			lo, err1 := numLit(m[2])
			hi, err2 := numLit(m[3])
			if err1 != nil || err2 != nil {
				return unsupported()
			}
			parsed = append(parsed, term{kind: tBetween, col: m[1], lo: lo, hi: hi, loRaw: cleanNum(m[2]), hiRaw: cleanNum(m[3])})
			continue
		}
		if m := anyFullRe.FindStringSubmatch(tt); m != nil {
			vals := splitList(m[2])
			if len(vals) == 0 {
				return unsupported()
			}
			vs := make([]any, len(vals))
			for i, v := range vals {
				vs[i] = v
			}
			parsed = append(parsed, term{kind: tAny, col: m[1], vals: vs})
			continue
		}
		if m := inFullRe.FindStringSubmatch(tt); m != nil {
			vals := splitList(m[2])
			if len(vals) == 0 {
				return unsupported()
			}
			vs := make([]any, len(vals))
			for i, v := range vals {
				vs[i] = v
			}
			parsed = append(parsed, term{kind: tIn, col: m[1], vals: vs})
			continue
		}
		if m := cmpFullRe.FindStringSubmatch(tt); m != nil {
			v, err := numLit(m[3])
			if err != nil {
				return unsupported()
			}
			raw := cleanNum(m[3])
			if (m[2] == ">" || m[2] == "<") && strings.ContainsAny(raw, ".eE") {
				return unsupported()
			}
			parsed = append(parsed, term{kind: tCmp, col: m[1], op: m[2], val: v, valRaw: raw})
			continue
		}
		// Anything else on any column — including x <> 5, x != 5,
		// x % 2 = 0 — poisons the whole expression.
		return unsupported()
	}
	// All terms must reference a single column.
	col := parsed[0].col
	for _, pt := range parsed[1:] {
		if !strings.EqualFold(pt.col, col) {
			return unsupported()
		}
	}
	// IN / ANY / BETWEEN fully determine the domain: combining them with
	// any other predicate on the same column cannot be folded safely
	// (x IN (1,2) AND x > 1 would emit 1), so multi-term expressions
	// containing them are unsupported.
	for _, pt := range parsed {
		if pt.kind == tIn || pt.kind == tAny || pt.kind == tBetween {
			if len(parsed) != 1 {
				return unsupported()
			}
		}
	}
	mentionsOther := func(target string) bool {
		if len(columns) == 0 {
			return false
		}
		stripped := stripStringLiterals(def)
		for _, c := range columns {
			if c == "" || strings.EqualFold(c, target) {
				continue
			}
			if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(c) + `\b`).MatchString(stripped) {
				return true
			}
		}
		return false
	}
	if mentionsOther(col) {
		return unsupported()
	}
	// Single IN / ANY / BETWEEN term (values come from the original
	// definition so string literals survive masking).
	if len(parsed) == 1 {
		switch parsed[0].kind {
		case tBetween:
			lo, hi := parsed[0].lo, parsed[0].hi
			lr, hr := parsed[0].loRaw, parsed[0].hiRaw
			return CheckHint{Column: parsed[0].col, Min: &lo, Max: &hi, MinRaw: &lr, MaxRaw: &hr}, "between", true
		case tIn, tAny:
			if vals := checkListValues(def, parsed[0].col, parsed[0].kind == tAny); len(vals) > 0 {
				return CheckHint{Column: parsed[0].col, Values: vals}, "in", true
			}
			return CheckHint{Column: parsed[0].col, Values: parsed[0].vals}, "in", true
		}
	}
	// Pure comparison conjunction on one column: fold lower+upper bounds.
	type bound struct {
		lower    *float64
		upper    *float64
		lowerRaw *string
		upperRaw *string
	}
	b := &bound{}
	for _, pt := range parsed {
		if pt.kind != tCmp {
			return unsupported()
		}
		v := pt.val
		switch pt.op {
		case ">=":
			if b.lowerRaw == nil || exactBoundCompare(pt.valRaw, *b.lowerRaw) > 0 {
				vv := v
				b.lower = &vv
				raw := pt.valRaw
				b.lowerRaw = &raw
			}
		case ">":
			vv := v + 1
			raw, err := addIntLiteral(pt.valRaw, 1)
			if err != nil {
				return unsupported()
			}
			if b.lowerRaw == nil || exactBoundCompare(raw, *b.lowerRaw) > 0 {
				b.lower = &vv
				b.lowerRaw = &raw
			}
		case "<=":
			if b.upperRaw == nil || exactBoundCompare(pt.valRaw, *b.upperRaw) < 0 {
				vv := v
				b.upper = &vv
				raw := pt.valRaw
				b.upperRaw = &raw
			}
		case "<":
			vv := v - 1
			raw, err := addIntLiteral(pt.valRaw, -1)
			if err != nil {
				return unsupported()
			}
			if b.upperRaw == nil || exactBoundCompare(raw, *b.upperRaw) < 0 {
				b.upper = &vv
				b.upperRaw = &raw
			}
		case "=":
			raw := pt.valRaw
			// Equality is an exact lower and upper bound, but it must
			// intersect the bounds already collected. Replacing both sides
			// makes the inferred domain depend on predicate order and can
			// generate values that violate an earlier CHECK term.
			if b.lowerRaw == nil || exactBoundCompare(raw, *b.lowerRaw) > 0 {
				vv := v
				b.lower = &vv
				lowerRaw := raw
				b.lowerRaw = &lowerRaw
			}
			if b.upperRaw == nil || exactBoundCompare(raw, *b.upperRaw) < 0 {
				vv := v
				b.upper = &vv
				upperRaw := raw
				b.upperRaw = &upperRaw
			}
		default:
			return unsupported()
		}
	}
	h := CheckHint{Column: col, Min: b.lower, Max: b.upper, MinRaw: b.lowerRaw, MaxRaw: b.upperRaw}
	if b.lower != nil && b.upper != nil {
		return h, "between", true
	}
	return h, "comparison", true
}

func exactBoundCompare(a, b string) int {
	cmp, ok := compareNumericLiteral(a, b)
	if !ok {
		return strings.Compare(a, b)
	}
	return cmp
}

// maskStringLiterals replaces each single-quoted ('...', ”-escaped) literal
// with a dummy numeric 0 so structure checks and term splitting see a stable
// shape: keywords/commas inside literals can't veto or reshape parsing,
// while IN-list arity is preserved for grammar validation.
func maskStringLiterals(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '\'' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		// Consume the whole literal.
		i++
		for i < len(s) {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
		sb.WriteByte('0')
	}
	return sb.String()
}

// checkListValues extracts the real (unmasked) IN / ANY-ARRAY values for one
// already-validated single-term hint from the original definition.
func checkListValues(def, col string, isAny bool) []any {
	var m []string
	if isAny {
		m = anyArrRe.FindStringSubmatch(def)
	} else {
		m = inRe.FindStringSubmatch(def)
	}
	if m == nil || !strings.EqualFold(m[1], col) {
		return nil
	}
	vals := splitList(m[2])
	if len(vals) == 0 {
		return nil
	}
	vs := make([]any, len(vals))
	for i, v := range vals {
		vs[i] = v
	}
	return vs
}

// splitCheckTerms normalizes an outer CHECK (...) wrapper and splits the
// expression into top-level AND conjuncts. BETWEEN ... AND ... contains an
// AND that is not a separator, so BETWEEN spans are masked before splitting.
func splitCheckTerms(nolit string) ([]string, error) {
	s := strings.TrimSpace(nolit)
	// Strip a leading CHECK keyword plus its wrapper parens.
	if m := regexp.MustCompile(`(?i)^\s*CHECK\s*`).FindString(s); m != "" {
		s = s[len(m):]
	}
	s = strings.TrimSpace(s)
	for len(s) >= 2 && strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && parensBalanced(s[1:len(s)-1]) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	if s == "" {
		return nil, fmt.Errorf("empty check")
	}
	// Mask BETWEEN ... AND ... spans so the inner AND survives splitting.
	lower := strings.ToUpper(s)
	var masked strings.Builder
	masked.Grow(len(s))
	i := 0
	for i < len(s) {
		if isWordAt(lower, i, "BETWEEN") {
			// Copy BETWEEN lo AND hi verbatim but mask the inner AND.
			j := i + len("BETWEEN")
			masked.WriteString(s[i:j])
			i = j
			// Copy literal span up to the matching AND, then mask it.
			andPos, ok := findBetweenAnd(s, i)
			if !ok {
				return nil, fmt.Errorf("malformed between")
			}
			masked.WriteString(s[i:andPos])
			masked.WriteString("\x01")
			i = andPos + len("AND")
			continue
		}
		masked.WriteByte(s[i])
		i++
	}
	mstr := masked.String()
	andRe := regexp.MustCompile(`(?i)\s*\bAND\b\s*`)
	parts := andRe.Split(mstr, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ReplaceAll(p, "\x01", " AND ")
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty term")
		}
		out = append(out, p)
	}
	return out, nil
}

func parensBalanced(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func isWordAt(s string, pos int, word string) bool {
	if pos+len(word) > len(s) {
		return false
	}
	if s[pos:pos+len(word)] != word {
		return false
	}
	before := pos == 0 || !isWordChar(s[pos-1])
	after := pos+len(word) >= len(s) || !isWordChar(s[pos+len(word)])
	return before && after
}

func isWordChar(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// findBetweenAnd locates the AND terminating a BETWEEN low AND high span,
// scanning forward from pos (just after BETWEEN) at paren depth zero.
func findBetweenAnd(s string, pos int) (int, bool) {
	depth := 0
	up := strings.ToUpper(s)
	for i := pos; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && isWordAt(up, i, "AND") {
			return i, true
		}
	}
	return 0, false
}

// stripStringLiterals removes single-quoted (”-escaped) literals so global
// grammar checks don't mistake literal text for keywords or calls.
func stripStringLiterals(s string) string {
	var sb strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			if in && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			in = !in
			continue
		}
		if !in {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func numLit(s string) (float64, error) {
	return strconv.ParseFloat(strings.Trim(strings.TrimSpace(s), "'\""), 64)
}

func cleanNum(s string) string { return strings.Trim(strings.TrimSpace(s), "'\"") }

func addIntLiteral(s string, delta int64) (string, error) {
	n, ok := new(big.Int).SetString(cleanNum(s), 10)
	if !ok {
		return "", fmt.Errorf("not integer")
	}
	n.Add(n, big.NewInt(delta))
	return n.String(), nil
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
		lo, a := exactInt(h[0])
		hi, b := exactInt(h[1])
		if a == nil && b == nil {
			return intRange{lo, hi}, true
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

// checkHintFloat is the float counterpart of checkHintInt for the decimal
// generator, so NUMERIC CHECK ranges are honored instead of defaulting to
// 0..10000.
func checkHintFloat(col string, ctx *RowContext) (float64, float64, bool) {
	if ctx == nil {
		return 0, 0, false
	}
	if h, ok := ctx.FK["__hint:"+col]; ok && len(h) == 2 {
		lo, a := toFloatLoose(h[0])
		hi, b := toFloatLoose(h[1])
		if a && b {
			return lo, hi, true
		}
	}
	return 0, 0, false
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
