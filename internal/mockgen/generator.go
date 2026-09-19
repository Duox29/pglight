package mockgen

import (
	"fmt"
	"math/rand"
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

	p := &Plan{Meta: meta, Mode: mode, Seeded: req.HasSeed, SeedVal: req.Seed}
	if mode == "simple" {
		for _, c := range meta.Columns {
			if OmittedInSimple(c) {
				p.Omitted = append(p.Omitted, c.Name)
				continue
			}
			p.Fields = append(p.Fields, FieldSpec{Column: c.Name, Generator: "simple"})
		}
		if len(p.Fields) == 0 {
			return nil, fmt.Errorf("no writable columns (all are identity/generated/default)")
		}
		for _, c := range meta.Columns {
			if !contains(p.Omitted, c.Name) {
				p.InsertCols = append(p.InsertCols, c.Name)
			}
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
	// warn on the rest (PostgreSQL stays the final validator).
	hintRanges := map[string][2]any{}
	hintIN := map[string][]any{}
	for _, ch := range meta.Checks {
		h, kind, ok := ParseCheckHint(ch.Definition)
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
				if lo == nil || *h.Min > toFloat(lo) {
					lo = *h.Min
				}
			}
			if h.Max != nil {
				if hi == nil || *h.Max < toFloat(hi) {
					hi = *h.Max
				}
			}
			if lo == nil {
				lo = float64(-1000000)
			}
			if hi == nil {
				hi = float64(1000000)
			}
			hintRanges[h.Column] = [2]any{lo, hi}
		case "in":
			hintIN[h.Column] = h.Values
		}
		_ = kind
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
	// Validate constraint fields exist.
	for _, cc := range req.Constraints {
		for _, s := range []ConstraintSide{cc.Left, cc.Right} {
			if s.Field != "" && meta.ColumnByName(s.Field) == nil {
				return nil, fmt.Errorf("constraint references unknown column %q", s.Field)
			}
		}
	}
	p.Constraints = req.Constraints
	for _, name := range ordered {
		p.Fields = append(p.Fields, byCol[name])
	}
	p.InsertCols = ordered
	if len(p.Fields) == 0 {
		return nil, fmt.Errorf("no writable columns (all are database defaults)")
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
func (p *Plan) GenerateRows(seed int64, count int, fkValues map[string][]any) ([][]any, error) {
	r := rand.New(rand.NewSource(seed))
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
	seq := map[string]int64{}
	fkRR := map[string]int{}
	out := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		row, err := p.genRow(r, now, seq, fkRR, fkValues, seen, i)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func (p *Plan) genRow(r *rand.Rand, now time.Time, seq map[string]int64, fkRR map[string]int, fkValues map[string][]any, seen map[string]map[string]bool, rowIdx int) ([]any, error) {
	// Snapshot uniqueness state so a failed row attempt rolls back claims.
	for attempt := 0; attempt < MaxAttemptsPerRow; attempt++ {
		rowMap := map[string]any{}
		ctx := &RowContext{Row: rowMap, Seq: seq, FK: fkValues, FKRR: fkRR, Rand: r, Now: now}
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
			if p.Mode == "advanced" && c.Nullable && f.NullProb > 0 && v != nil {
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
		// Commit uniqueness claims.
		for _, f := range p.Fields {
			if uniq := seen[f.Column]; uniq != nil {
				if v, ok := rowMap[f.Column]; ok && v != nil {
					uniq[fmt.Sprintf("%v", v)] = true
				}
			}
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
