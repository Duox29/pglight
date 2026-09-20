package mockgen

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// MaxRowsPerRequest matches the /api/import safety limit.
const MaxRowsPerRequest = 20000

// MaxAttemptsPerRow bounds uniqueness/constraint retries; the engine never
// loops forever on an unsatisfiable domain.
const MaxAttemptsPerRow = 50

// Request is the normalized generation job shared by preview and generate.
type Request struct {
	Mode        string
	Count       int
	Seed        int64
	HasSeed     bool
	Fields      []FieldSpec
	Constraints []Constraint
	FKValues    map[string][]any
}

// Plan is a validated, dependency-ordered generation program.
type Plan struct {
	Meta        TableMeta
	Mode        string
	Fields      []FieldSpec // one per inserted column, in dependency order
	Omitted     []string    // identity/generated/default columns (DB fills them)
	InsertCols  []string    // columns present in INSERT, in table order
	Constraints []Constraint
	HintRanges  map[string][2]any
	HintIN      map[string][]any
	Warnings    []string
	// FKTuples holds one referenced tuple pool per composite FK group name,
	// loaded by the API layer after planning (fetchFKValues). GenerateRows
	// picks one tuple per row so members stay coherent.
	FKTuples map[string][][]any
	// Seeded pins the generation clock to a deterministic anchor so the
	// same seed always yields the same output. Unseeded runs use now.
	Seeded  bool
	SeedVal int64
}

// anchorTime derives a deterministic "now" from the seed: a fixed base
// plus a seed-dependent jitter under 30 days, so different seeds still
// produce different timestamps.
func anchorTime(seed int64) time.Time {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	j := seed % int64(30*24*time.Hour)
	return base.Add(time.Duration(j))
}

// EffectiveSeed resolves the RNG seed: explicit when provided, otherwise now.
func EffectiveSeed(hasSeed bool, seed int64) int64 {
	if hasSeed {
		return seed
	}
	return time.Now().UnixNano()
}

// BuildPlan normalizes the request against schema: picks inserted vs omitted
// columns, applies CHECK hints, orders dependents, and detects cycles.
func BuildPlan(meta TableMeta, req Request) (*Plan, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "simple"
	}
	if mode != "simple" && mode != "advanced" {
		return nil, fmt.Errorf("unknown mode %q (want simple|advanced)", req.Mode)
	}
	if req.Count < 1 {
		return nil, fmt.Errorf("count must be >= 1")
	}
	if req.Count > MaxRowsPerRequest {
		return nil, fmt.Errorf("too many rows (max %d per request)", MaxRowsPerRequest)
	}
	for _, c := range req.Constraints {
		if c.Kind != "" && c.Kind != "compare" {
			return nil, fmt.Errorf("unsupported constraint kind %q", c.Kind)
		}
		if !validOps[c.Operator] {
			return nil, fmt.Errorf("unsupported operator %q (want one of = != < <= > >=)", c.Operator)
		}
	}
	if len(req.Constraints) > MaxConstraintsPerRequest {
		return nil, fmt.Errorf("too many constraints (max %d)", MaxConstraintsPerRequest)
	}

	p := &Plan{Meta: meta, Mode: mode, Seeded: req.HasSeed, SeedVal: req.Seed}
	if mode == "simple" {
		for _, c := range meta.Columns {
			if OmittedInSimple(c) {
				p.Omitted = append(p.Omitted, c.Name)
				continue
			}
			p.Fields = append(p.Fields, FieldSpec{Column: c.Name, Generator: "simple"})
		}
		for _, c := range meta.Columns {
			if !contains(p.Omitted, c.Name) {
				p.InsertCols = append(p.InsertCols, c.Name)
			}
		}
		if len(p.Fields) == 0 {
			// Legitimate table (e.g. id BIGSERIAL + created_at DEFAULT
			// now()): generate DEFAULT-only rows, filled by PostgreSQL.
			p.Warnings = append(p.Warnings, "All columns are database defaults — inserting DEFAULT rows.")
		}
		return p, nil
	}

	// Advanced: index user field configs by column.
	byCol := map[string]FieldSpec{}
	for _, f := range req.Fields {
		if meta.ColumnByName(f.Column) == nil {
			return nil, fmt.Errorf("unknown column %q", f.Column)
		}
		nf := f
		nf.Generator = strings.ToLower(strings.TrimSpace(nf.Generator))
		if nf.Generator == "" {
			nf.Generator = "auto"
		}
		byCol[f.Column] = nf
	}
	// Default: identity/generated/serial/default columns get db_default,
	// everything else gets auto — the frontend sends explicit configs but
	// the backend must behave sanely when fields are omitted.
	for _, c := range meta.Columns {
		if _, ok := byCol[c.Name]; ok {
			continue
		}
		g := "auto"
		if c.Identity || c.Generated || IsSerialDefault(c.Default) || c.Default != nil {
			g = "db_default"
		}
		byCol[c.Name] = FieldSpec{Column: c.Name, Generator: g}
	}
	// Unique defaults from schema for non-PK unique columns.
	for _, c := range meta.Columns {
		if c.Unique && !c.PrimaryKey {
			f := byCol[c.Name]
			f.Unique = true
			byCol[c.Name] = f
		}
	}
	// CHECK hints: fold supported checks into implicit ranges/choices and
	// warn on the rest (PostgreSQL stays the final validator). Column names
	// are passed so partially-parseable multi-column CHECKs are rejected
	// instead of silently dropping clauses.
	colNames := make([]string, len(meta.Columns))
	for i, c := range meta.Columns {
		colNames[i] = c.Name
	}
	hintRanges := map[string][2]any{}
	hintIN := map[string][]any{}
	for _, ch := range meta.Checks {
		h, kind, ok := ParseCheckHintForColumns(ch.Definition, colNames)
		if !ok {
			p.Warnings = append(p.Warnings, fmt.Sprintf("Unsupported CHECK %s: %s — validated by PostgreSQL during insertion.", ch.Name, ch.Definition))
			continue
		}
		switch kind {
		case "between", "comparison":
			cur, has := hintRanges[h.Column]
			lo, hi := any(nil), any(nil)
			if has {
				lo, hi = cur[0], cur[1]
			}
			if h.Min != nil {
				candidate := any(*h.Min)
				if h.MinRaw != nil && isIntegerColumn(*meta.ColumnByName(h.Column)) {
					if v, err := strconv.ParseInt(*h.MinRaw, 10, 64); err == nil {
						candidate = v
					}
				}
				if lo == nil || compareAnyNumeric(candidate, lo) > 0 {
					lo = candidate
				}
			}
			if h.Max != nil {
				candidate := any(*h.Max)
				if h.MaxRaw != nil && isIntegerColumn(*meta.ColumnByName(h.Column)) {
					if v, err := strconv.ParseInt(*h.MaxRaw, 10, 64); err == nil {
						candidate = v
					}
				}
				if hi == nil || compareAnyNumeric(candidate, hi) < 0 {
					hi = candidate
				}
			}
			hintRanges[h.Column] = [2]any{lo, hi}
		case "in":
			hintIN[h.Column] = h.Values
		}
		_ = kind
	}
	// Materialize open bounds only after all CHECKs for a column have been
	// merged. Integer columns use their PostgreSQL width; this prevents a
	// one-sided CHECK such as id >= 2000000 from becoming an inverted range
	// against the old generic default max of 1000000.
	for col, r := range hintRanges {
		c := meta.ColumnByName(col)
		if c == nil {
			continue
		}
		lo, hi := r[0], r[1]
		if isIntegerColumn(*c) {
			minType, maxType := intTypeBounds(*c)
			if lo == nil {
				lo = minType
			}
			if hi == nil {
				hi = maxType
			}
		} else {
			if lo == nil {
				lo = float64(-1000000)
			}
			if hi == nil {
				hi = float64(1000000)
			}
			if compareAnyNumeric(lo, hi) > 0 {
				if r[0] != nil {
					hi = lo
				} else {
					lo = hi
				}
			}
		}
		if compareAnyNumeric(lo, hi) > 0 {
			return nil, fmt.Errorf("inconsistent CHECK range for column %q", col)
		}
		hintRanges[col] = [2]any{lo, hi}
	}
	_ = hintRanges
	_ = hintIN
	p.HintRanges = hintRanges
	p.HintIN = hintIN

	// Resolve db_default omissions and FK-missing errors.
	insertSet := []string{}
	deps := map[string]string{}
	for _, c := range meta.Columns {
		f := byCol[c.Name]
		if f.Generator == "auto" && autoGenerator(c) == "db_default" {
			// Explicit auto on an identity/generated/serial column means
			// "let PostgreSQL fill it", same as the implicit default.
			f.Generator = "db_default"
			byCol[c.Name] = f
		}
		if f.Generator == "db_default" {
			p.Omitted = append(p.Omitted, c.Name)
			continue
		}
		if f.Generator == "null" && !c.Nullable {
			return nil, fmt.Errorf("column %q is NOT NULL but generator is NULL", c.Name)
		}
		if (f.Generator == "auto" || f.Generator == "foreign_key") && isFKColumn(meta, c.Name) {
			if f.Generator == "auto" {
				f.Generator = "foreign_key"
				byCol[c.Name] = f
			}
		}
		if f.Generator == "relative_datetime" {
			src := ""
			if f.Params != nil {
				if s, ok := f.Params["source"].(string); ok {
					src = s
				}
			}
			if src == "" {
				return nil, fmt.Errorf("column %q: relative_datetime requires params.source", c.Name)
			}
			if meta.ColumnByName(src) == nil {
				return nil, fmt.Errorf("column %q: relative source %q does not exist", c.Name, src)
			}
			deps[c.Name] = src
		}
		insertSet = append(insertSet, c.Name)
	}
	ordered, err := TopoOrder(insertSet, deps)
	if err != nil {
		return nil, err
	}
	// Composite FK coherence as one unit: tuple generation is only safe
	// when EVERY member takes automatic FK values. If any member pins an
	// explicit ref_* source, uses Constant/another non-FK generator, or is
	// omitted/nulled, the remaining members would mix a random tuple with
	// the custom value and recreate the invalid-tuple problem — reject
	// with a clear message (group-level sources are future work).
	for _, g := range meta.FKGroups() {
		if len(g.Local) < 2 {
			continue
		}
		if g.MatchType == "f" || g.MatchType == "full" {
			var prob *float64
			for _, lc := range g.Local {
				f := byCol[lc]
				if prob == nil {
					v := f.NullProb
					prob = &v
				} else if *prob != f.NullProb {
					return nil, fmt.Errorf("composite MATCH FULL foreign key %q requires identical null_probability for all members", g.Name)
				}
			}
		}
		for _, lc := range g.Local {
			f, ok := byCol[lc]
			if !ok {
				continue
			}
			gen := strings.ToLower(strings.TrimSpace(f.Generator))
			if hasExplicitRefParams(f.Params) || hasAnyRefParam(f.Params) {
				return nil, fmt.Errorf("composite foreign key %q requires all members to use automatic FK generation: column %q pins an explicit source (configure group-level sources instead)", g.Name, lc)
			}
			if gen != "auto" && gen != "foreign_key" {
				return nil, fmt.Errorf("composite foreign key %q requires all members to use automatic FK generation: column %q uses %q", g.Name, lc, f.Generator)
			}
		}
	}
	// Validate constraint fields exist.
	for _, cc := range req.Constraints {
		for _, s := range []ConstraintSide{cc.Left, cc.Right} {
			if s.Field != "" && meta.ColumnByName(s.Field) == nil {
				return nil, fmt.Errorf("constraint references unknown column %q", s.Field)
			}
		}
	}
	// Server-side param validation: fail fast before any row is generated.
	// Weights are normalized once here (parsed + capped) so generation
	// never re-allocates a huge array per row.
	for _, name := range ordered {
		f := byCol[name]
		c := meta.ColumnByName(name)
		if c == nil {
			return nil, fmt.Errorf("unknown column %q", name)
		}
		if err := validateFieldParams(*c, f); err != nil {
			return nil, err
		}
		if f.Generator == "sequence" && isIntegerColumn(*c) {
			start, _ := fieldExactInt(f.Params, "start", 1)
			step, _ := fieldExactInt(f.Params, "step", 1)
			if step == 0 {
				step = 1
			}
			delta, ok := checkedMul(int64(req.Count-1), step)
			last, ok2 := checkedAdd(start, delta)
			if !ok || !ok2 {
				return nil, fmt.Errorf("sequence overflow for column %q", c.Name)
			}
			lo, hi := intTypeBounds(*c)
			minV, maxV := start, last
			if minV > maxV {
				minV, maxV = maxV, minV
			}
			if minV < lo || maxV > hi {
				return nil, fmt.Errorf("sequence range [%d,%d] exceeds %s range [%d,%d]", minV, maxV, colIntType(*c), lo, hi)
			}
		}
		f.NormWeights = normalizeWeights(f.Params)
		f.WeightsProcessed = true
		if vals, ok := f.Params["values"].([]any); ok {
			if len(f.NormWeights) != len(vals) {
				f.NormWeights = nil
			}
		}
		byCol[name] = f
	}
	// Uniqueness is batch-scoped: warn whenever per-column uniqueness is
	// requested so users know pre-existing table values are not consulted.
	for _, name := range ordered {
		if byCol[name].Unique {
			p.Warnings = append(p.Warnings, "Uniqueness is enforced within the generated batch only; values already in the table are not checked.")
			break
		}
	}
	// Composite/partial UNIQUEs are insert-validated, not pre-satisfied.
	for _, members := range meta.CompositeUniques {
		if len(members) > 1 {
			p.Warnings = append(p.Warnings, "Composite UNIQUE ("+strings.Join(members, ", ")+") is not pre-satisfied in Advanced mode — validated by PostgreSQL during insertion.")
		}
	}
	if meta.HasPartialUnique {
		p.Warnings = append(p.Warnings, "Partial/expression unique index present — uniqueness holds only within its predicate and is validated by PostgreSQL during insertion.")
	}
	p.Constraints = req.Constraints
	for _, name := range ordered {
		p.Fields = append(p.Fields, byCol[name])
	}
	p.InsertCols = ordered
	if len(p.Fields) == 0 {
		// All columns are database defaults — same DEFAULT-rows support as
		// Simple mode (the INSERT path emits DEFAULT VALUES per row).
		p.Warnings = append(p.Warnings, "All columns are database defaults — inserting DEFAULT rows.")
	}
	return p, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func toFloat(v any) float64 {
	f, _ := toFloatLoose(v)
	return f
}

func isIntegerColumn(c ColumnMeta) bool {
	switch strings.ToLower(c.Udt) {
	case "int2", "int4", "int8":
		return true
	}
	switch strings.ToLower(c.DataType) {
	case "smallint", "integer", "bigint":
		return true
	}
	return false
}

func compareAnyNumeric(a, b any) int {
	ar, aok := numericRat(a)
	br, bok := numericRat(b)
	if !aok || !bok {
		return 0
	}
	return ar.Cmp(br)
}

func isFKColumn(meta TableMeta, col string) bool {
	for _, fk := range meta.ForeignKeys {
		if fk.Column == col {
			return true
		}
	}
	return false
}

func fkOf(meta TableMeta, col string) ForeignKeyMeta {
	for _, fk := range meta.ForeignKeys {
		if fk.Column == col {
			return fk
		}
	}
	return ForeignKeyMeta{}
}

func fkRef(fk ForeignKeyMeta) string {
	if fk.RefTable == "" {
		return "?"
	}
	if fk.RefSchema != "" {
		return fk.RefSchema + "." + fk.RefTable
	}
	return fk.RefTable
}

// GenerateRows materializes count rows. Returned rows are column-ordered
// slices aligned with Plan.InsertCols; omitted columns are NOT included.
// A default-only plan yields one empty slice per row (DEFAULT VALUES).
func (p *Plan) GenerateRows(seed int64, count int, fkValues map[string][]any) ([][]any, error) {
	r := rand.New(rand.NewSource(seed))
	if fkValues == nil {
		fkValues = map[string][]any{}
	}
	now := time.Now().UTC()
	if p.Seeded {
		now = anchorTime(p.SeedVal)
	}
	seen := map[string]map[string]bool{}
	for _, f := range p.Fields {
		if f.Unique {
			seen[f.Column] = map[string]bool{}
		}
	}
	// Column → composite-group index (read-only, shared across attempts).
	fkGroups := p.Meta.FKGroups()
	fkColGroup := map[string]int{}
	for gi, g := range fkGroups {
		for _, lc := range g.Local {
			if _, dup := fkColGroup[lc]; !dup {
				fkColGroup[lc] = gi
			}
		}
	}
	byCol := map[string]FieldSpec{}
	for _, f := range p.Fields {
		byCol[f.Column] = f
	}
	seq := map[string]int64{}
	fkRR := map[string]int{}
	fkGRR := map[string]int{}
	out := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		row, err := p.genRow(r, now, seq, fkRR, fkGRR, fkValues, seen, fkGroups, fkColGroup, byCol, i)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func cloneInt64Map(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (p *Plan) genRow(r *rand.Rand, now time.Time, seq map[string]int64, fkRR, fkGRR map[string]int, fkValues map[string][]any, seen map[string]map[string]bool, fkGroups []FKGroup, fkColGroup map[string]int, byCol map[string]FieldSpec, rowIdx int) ([]any, error) {
	// Snapshot uniqueness AND counter state so a failed row attempt rolls
	// back every claim: uniqueness sets commit on success, while sequence
	// and FK round-robin counters (per-column and per-group) commit only
	// when the row passes all constraints — no gaps from retries.
	for attempt := 0; attempt < MaxAttemptsPerRow; attempt++ {
		seqTrial := cloneInt64Map(seq)
		fkTrial := cloneIntMap(fkRR)
		fkGTrial := cloneIntMap(fkGRR)
		// Pick one referenced tuple per composite group for this attempt
		// (sequential when any member asks for it, else uniform).
		tuplePick := map[string][]any{}
		fullNull := map[string]bool{}
		for _, g := range fkGroups {
			if len(g.Local) < 2 {
				continue
			}
			pool := p.FKTuples[g.Name]
			if len(pool) == 0 {
				continue
			}
			// Members with an explicit source override or a non-FK
			// generator don't participate, but the remaining members
			// still share one tuple.
			participates := false
			sequential := false
			for _, lc := range g.Local {
				f, ok := byCol[lc]
				if !ok || hasExplicitRefParams(f.Params) {
					continue
				}
				gen := strings.ToLower(strings.TrimSpace(f.Generator))
				if gen == "" || gen == "auto" || gen == "foreign_key" {
					participates = true
				}
				if paramString(f.Params, "mode", "random") == "sequential" {
					sequential = true
				}
			}
			if !participates {
				continue
			}
			if g.MatchType == "f" || g.MatchType == "full" {
				prob := 0.0
				if f, ok := byCol[g.Local[0]]; ok {
					prob = f.NullProb
				}
				fullNull[g.Name] = r.Float64() < prob
			}
			idx := 0
			if sequential {
				idx = fkGTrial[g.Name] % len(pool)
				fkGTrial[g.Name]++
			} else {
				idx = r.Intn(len(pool))
			}
			tuplePick[g.Name] = pool[idx]
		}
		rowMap := map[string]any{}
		ctx := &RowContext{Row: rowMap, Seq: seqTrial, FK: fkValues, FKRR: fkTrial, Rand: r, Now: now,
			FKGroups: fkGroups, FKColGroup: fkColGroup, FKTuples: p.FKTuples, FKTuplePick: tuplePick, FKGroupRR: fkGTrial}
		for k, v := range p.HintRanges {
			ctx.FK["__hint:"+k] = []any{v[0], v[1]}
		}
		for k, v := range p.HintIN {
			ctx.FK["__hint_in:"+k] = v
		}
		failed := ""
		for _, f := range p.Fields {
			c := p.Meta.ColumnByName(f.Column)
			if c == nil {
				return nil, fmt.Errorf("unknown column %q", f.Column)
			}
			var v any
			var err error
			if p.Mode == "simple" {
				v, err = simpleValueAt(r, now, *c)
			} else {
				v, err = generateField(ctx, *c, f)
				if err == errOmit {
					continue
				}
			}
			if err != nil {
				if err.Error() == "no_fk_values" {
					return nil, fmt.Errorf("cannot generate %s: referenced table contains no valid values", f.Column)
				}
				return nil, fmt.Errorf("column %s: %v", f.Column, err)
			}
			// NULL probability for nullable columns (advanced explicit).
			fullGroup, inComposite := fkColGroup[f.Column]
			fullNil := false
			isMatchFull := false
			if inComposite {
				isMatchFull = fkGroups[fullGroup].MatchType == "f" || fkGroups[fullGroup].MatchType == "full"
			}
			if isMatchFull {
				fullNil = fullNull[fkGroups[fullGroup].Name]
			}
			if fullNil {
				v = nil
			}
			if p.Mode == "advanced" && !isMatchFull && c.Nullable && f.NullProb > 0 && v != nil {
				if r.Float64() < f.NullProb {
					v = nil
				}
			}
			if v == nil && !c.Nullable && f.Generator != "null" {
				// A null slipped through on NOT NULL — regenerate.
				failed = f.Column
				break
			}
			// Unique tracking.
			if uniq := seen[f.Column]; uniq != nil && v != nil {
				key := fmt.Sprintf("%v", v)
				if uniq[key] {
					failed = f.Column
					break
				}
			}
			rowMap[f.Column] = v
		}
		if failed != "" {
			if attempt == MaxAttemptsPerRow-1 {
				return nil, uniquenessError(p, failed)
			}
			continue
		}
		if err := ValidateRow(rowMap, p.Constraints); err != nil {
			if attempt == MaxAttemptsPerRow-1 {
				return nil, fmt.Errorf("generation failed at row %d: %v", rowIdx+1, err)
			}
			continue
		}
		// Commit uniqueness claims AND counter state (sequences, FK
		// round-robins) only after the row passes every constraint.
		for _, f := range p.Fields {
			if uniq := seen[f.Column]; uniq != nil {
				if v, ok := rowMap[f.Column]; ok && v != nil {
					uniq[fmt.Sprintf("%v", v)] = true
				}
			}
		}
		for k, v := range seqTrial {
			seq[k] = v
		}
		for k, v := range fkTrial {
			fkRR[k] = v
		}
		for k, v := range fkGTrial {
			fkGRR[k] = v
		}
		row := make([]any, len(p.InsertCols))
		for i, col := range p.InsertCols {
			row[i] = rowMap[col]
		}
		return row, nil
	}
	return nil, fmt.Errorf("generation failed at row %d: unable to satisfy constraints", rowIdx+1)
}

func uniquenessError(p *Plan, col string) error {
	domain := "unknown"
	for _, f := range p.Fields {
		if f.Column != col {
			continue
		}
		if vals := paramValues(f.Params); len(vals) > 0 {
			domain = fmt.Sprintf("only %d values", len(vals))
		}
	}
	return fmt.Errorf("cannot generate unique values for %s: possible generator domain contains %s", col, domain)
}

// PreviewRow maps an inserted row to the full table column order, rendering
// omitted (DB-filled) columns as "<database default>".
func (p *Plan) PreviewRow(insertRow []any) []any {
	byCol := map[string]any{}
	for i, c := range p.InsertCols {
		if i < len(insertRow) {
			byCol[c] = insertRow[i]
		}
	}
	full := make([]any, len(p.Meta.Columns))
	for i, c := range p.Meta.Columns {
		if v, ok := byCol[c.Name]; ok {
			full[i] = PreviewCell(v)
			continue
		}
		full[i] = "<database default>"
	}
	return full
}

// PreviewCell renders one generated value JSON-safe for the preview table.
func PreviewCell(v any) any {
	if v == nil {
		return nil
	}
	if b, ok := v.([]byte); ok {
		const hexd = "0123456789abcdef"
		var sb strings.Builder
		sb.WriteString("\\x")
		for _, c := range b {
			sb.WriteByte(hexd[c>>4])
			sb.WriteByte(hexd[c&15])
		}
		return sb.String()
	}
	return v
}

// AllColumnNames returns every table column in order (preview header).
func (p *Plan) AllColumnNames() []string {
	out := make([]string, len(p.Meta.Columns))
	for i, c := range p.Meta.Columns {
		out[i] = c.Name
	}
	return out
}
