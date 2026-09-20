package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/mockgen"

	"github.com/jackc/pgx/v5"
)

// Mock generation endpoints (Table workspace → Generate):
//
//	GET  /api/mock-data/meta      schema introspection (React never parses DDL)
//	POST /api/mock-data/preview   generate without touching the database
//	POST /api/mock-data/generate  atomic bulk generation + insert
//
// Simple mode is datatype-only and zero-config; business-constraint
// intelligence (semantic generators, UNIQUE/FK/CHECK inference, custom
// cross-field rules, relative datetimes) lives in Advanced mode only.

// MockMeta serves normalized table metadata for the generator dialog.
func (h *Handler) MockMeta(w http.ResponseWriter, r *http.Request) {
	qq, _, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	if table == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	if schema == "" {
		schema = "public"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	meta, err := loadMockMeta(ctx, qq, schema, table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, meta)
}

// loadMockMeta introspects one table via catalogs/information_schema.
func loadMockMeta(ctx context.Context, q db.Querier, schema, table string) (mockgen.TableMeta, error) {
	var meta mockgen.TableMeta
	// Base columns + identity/generated flags from pg_attribute.
	_, crows, err := queryJSON(q, ctx, `
		SELECT c.column_name, c.data_type, c.udt_name, c.is_nullable, c.column_default,
			COALESCE(a.attidentity IN ('a','d'), false),
			COALESCE(a.attgenerated <> '', false)
		FROM information_schema.columns c
		LEFT JOIN pg_attribute a ON a.attrelid = to_regclass(format('%I.%I', c.table_schema, c.table_name))
			AND a.attname = c.column_name
		WHERE c.table_schema=$1 AND c.table_name=$2
		ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return meta, err
	}
	if len(crows) == 0 {
		return meta, fmt.Errorf("table %s.%s not found", schema, table)
	}
	// Single-column PKs, UNIQUEs, FKs, CHECKs from pg_constraint (OID joins).
	pkCols := map[string]bool{}
	_, pkrows, err := queryJSON(q, ctx, `
		SELECT a.attname FROM pg_constraint con
		JOIN LATERAL unnest(con.conkey) AS k(attnum) ON true
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		WHERE con.contype='p' AND con.conrelid=to_regclass(format('%I.%I', $1::text, $2::text))`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range pkrows {
		pkCols[fmt.Sprint(r[0])] = true
	}
	uniqCols := map[string]bool{}
	_, urows, err := queryJSON(q, ctx, `
		SELECT a.attname FROM pg_constraint con
		JOIN LATERAL unnest(con.conkey) AS k(attnum) ON true
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		WHERE con.contype='u' AND con.conrelid=to_regclass(format('%I.%I', $1::text, $2::text))
		AND cardinality(con.conkey) = 1
		GROUP BY a.attname HAVING count(*)>=1`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range urows {
		uniqCols[fmt.Sprint(r[0])] = true
	}
	// Composite UNIQUE constraints (cardinality > 1): not treated as
	// per-column unique, but recorded so the planner can warn that
	// Advanced mode does not pre-satisfy them.
	_, curows, err := queryJSON(q, ctx, `
		SELECT con.conname, a.attname FROM pg_constraint con
		JOIN LATERAL unnest(con.conkey) AS k(attnum) ON true
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		WHERE con.contype='u' AND con.conrelid=to_regclass(format('%I.%I', $1::text, $2::text))
		AND cardinality(con.conkey) > 1
		ORDER BY con.conname, k.attnum`, schema, table)
	if err != nil {
		return meta, err
	}
	byCon := map[string][]string{}
	conOrder := []string{}
	for _, r := range curows {
		if len(r) < 2 {
			continue
		}
		name := fmt.Sprint(r[0])
		if _, ok := byCon[name]; !ok {
			conOrder = append(conOrder, name)
		}
		byCon[name] = append(byCon[name], fmt.Sprint(r[1]))
	}
	for _, name := range conOrder {
		meta.CompositeUniques = append(meta.CompositeUniques, byCon[name])
	}
	// Unique indexes also enforce uniqueness (e.g. UNIQUE(email) via CREATE
	// UNIQUE INDEX): treat single-key unique-index members as unique.
	// indnkeyatts (not array_length(indkey)) counts key columns excluding
	// INCLUDE payloads; partial (indpred) and expression (indexprs)
	// indexes only hold within their predicate, never globally.
	_, irows, err := queryJSON(q, ctx, `
		SELECT a.attname FROM pg_index ix
		JOIN pg_class t ON t.oid=ix.indrelid
		JOIN pg_namespace n ON n.oid=t.relnamespace
		JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=ix.indkey[0]
		WHERE n.nspname=$1 AND t.relname=$2 AND ix.indisunique AND ix.indisvalid
		AND ix.indnkeyatts=1 AND ix.indpred IS NULL AND ix.indexprs IS NULL`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range irows {
		uniqCols[fmt.Sprint(r[0])] = true
	}
	// Partial/expression unique indexes hold only within their predicate —
	// record the presence so the planner can warn instead of claiming
	// global uniqueness.
	_, prow, err := queryJSON(q, ctx, `
		SELECT 1 FROM pg_index ix
		JOIN pg_class t ON t.oid=ix.indrelid
		JOIN pg_namespace n ON n.oid=t.relnamespace
		WHERE n.nspname=$1 AND t.relname=$2 AND ix.indisunique AND ix.indisvalid
		AND (ix.indpred IS NOT NULL OR ix.indexprs IS NOT NULL) LIMIT 1`, schema, table)
	if err != nil {
		return meta, err
	}
	meta.HasPartialUnique = len(prow) > 0
	// Enum labels per column.
	enumVals := map[string][]string{}
	_, erows, err := queryJSON(q, ctx, `
		SELECT c.column_name, e.enumlabel
		FROM information_schema.columns c
		JOIN pg_type t ON t.typname=c.udt_name
		JOIN pg_namespace n ON n.oid=t.typnamespace AND n.nspname=c.udt_schema
		JOIN pg_enum e ON e.enumtypid=t.oid
		WHERE c.table_schema=$1 AND c.table_name=$2
		ORDER BY c.ordinal_position, e.enumsortorder`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range erows {
		if len(r) >= 2 {
			col := fmt.Sprint(r[0])
			enumVals[col] = append(enumVals[col], fmt.Sprint(r[1]))
		}
	}
	for _, r := range crows {
		if len(r) < 7 {
			continue
		}
		name := fmt.Sprint(r[0])
		nullable := strings.EqualFold(fmt.Sprint(r[3]), "YES")
		var def *string
		if r[4] != nil {
			s := fmt.Sprint(r[4])
			def = &s
		}
		cols := mockgen.ColumnMeta{
			Name:         name,
			DataType:     fmt.Sprint(r[1]),
			Udt:          fmt.Sprint(r[2]),
			Nullable:     nullable,
			Default:      def,
			Identity:     r[5] == true || fmt.Sprint(r[5]) == "t",
			Generated:    r[6] == true || fmt.Sprint(r[6]) == "t",
			PrimaryKey:   pkCols[name],
			Unique:       uniqCols[name] || pkCols[name],
			EnumValues:   enumVals[name],
			SemanticHint: mockgen.SemanticHintFor(name),
		}
		if cols.EnumValues == nil {
			cols.EnumValues = []string{}
		}
		meta.Columns = append(meta.Columns, cols)
	}
	// Foreign keys (OID joins — constraint names repeat across tables).
	_, frows, err := queryJSON(q, ctx, `
		SELECT con.conname, src_att.attname, dst_ns.nspname, dst.relname, dst_att.attname, con.confmatchtype::text
		FROM pg_constraint con
		JOIN pg_class src ON src.oid=con.conrelid
		JOIN pg_namespace src_ns ON src_ns.oid=src.relnamespace
		JOIN pg_class dst ON dst.oid=con.confrelid
		JOIN pg_namespace dst_ns ON dst_ns.oid=dst.relnamespace
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS sk(attnum,ord) ON true
		JOIN pg_attribute src_att ON src_att.attrelid=src.oid AND src_att.attnum=sk.attnum
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS tk(attnum,ord) ON tk.ord=sk.ord
		JOIN pg_attribute dst_att ON dst_att.attrelid=dst.oid AND dst_att.attnum=tk.attnum
		WHERE con.contype='f' AND src_ns.nspname=$1 AND src.relname=$2
		ORDER BY con.conname, sk.ord`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range frows {
		if len(r) < 6 {
			continue
		}
		meta.ForeignKeys = append(meta.ForeignKeys, mockgen.ForeignKeyMeta{
			Name:      fmt.Sprint(r[0]),
			Column:    fmt.Sprint(r[1]),
			RefSchema: fmt.Sprint(r[2]),
			RefTable:  fmt.Sprint(r[3]),
			RefColumn: fmt.Sprint(r[4]),
		})
		meta.ForeignKeys[len(meta.ForeignKeys)-1].MatchType = strings.ToLower(fmt.Sprint(r[5]))
	}
	if meta.ForeignKeys == nil {
		meta.ForeignKeys = []mockgen.ForeignKeyMeta{}
	}
	// CHECK definitions with best-effort structural kinds.
	_, chkrows, err := queryJSON(q, ctx, `
		SELECT conname, pg_get_constraintdef(oid, true) FROM pg_constraint
		WHERE conrelid=to_regclass(format('%I.%I', $1::text, $2::text)) AND contype='c' ORDER BY 1`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range chkrows {
		if len(r) < 2 {
			continue
		}
		name, def := fmt.Sprint(r[0]), fmt.Sprint(r[1])
		// Column names are known here, so partially-parseable
		// multi-column CHECKs are labeled unsupported (with a warning)
		// instead of silently dropping clauses.
		colNames := make([]string, len(meta.Columns))
		for i, c := range meta.Columns {
			colNames[i] = c.Name
		}
		_, kind, ok := mockgen.ParseCheckHintForColumns(def, colNames)
		if !ok {
			kind = "unsupported"
		}
		chk := mockgen.CheckMeta{Name: name, Definition: def, Kind: kind}
		if !ok {
			chk.Warning = "Unsupported CHECK — validated by PostgreSQL during insertion."
		}
		meta.Checks = append(meta.Checks, chk)
	}
	if meta.Checks == nil {
		meta.Checks = []mockgen.CheckMeta{}
	}
	return meta, nil
}

// --- shared request decoding ---

type mockFieldReq struct {
	Column          string         `json:"column"`
	Generator       string         `json:"generator"`
	Params          map[string]any `json:"params"`
	Unique          bool           `json:"unique"`
	NullProbability *float64       `json:"null_probability"`
}

type mockReq struct {
	Session     string               `json:"session_id"`
	Schema      string               `json:"schema"`
	Table       string               `json:"table"`
	Mode        string               `json:"mode"`
	Count       int                  `json:"count"`
	Seed        *int64               `json:"seed"`
	Fields      []mockFieldReq       `json:"fields"`
	Constraints []mockgen.Constraint `json:"constraints"`
}

func decodeMockReq(r *http.Request) (mockReq, string, error) {
	var req mockReq
	if err := decodeBody(r, &req); err != nil {
		return req, "", fmt.Errorf("invalid json")
	}
	id := sessionFromBody(req.Session, r)
	if id == "" {
		return req, "", fmt.Errorf("not connected")
	}
	if req.Schema == "" {
		req.Schema = "public"
	}
	if req.Mode == "" {
		req.Mode = "simple"
	}
	return req, id, nil
}

func toMockgenRequest(req mockReq) mockgen.Request {
	fields := make([]mockgen.FieldSpec, 0, len(req.Fields))
	for _, f := range req.Fields {
		np := 0.0
		if f.NullProbability != nil {
			np = *f.NullProbability
			if np < 0 {
				np = 0
			}
			if np > 1 {
				np = 1
			}
		}
		fields = append(fields, mockgen.FieldSpec{
			Column:    f.Column,
			Generator: f.Generator,
			Params:    f.Params,
			Unique:    f.Unique,
			NullProb:  np,
		})
	}
	seed := int64(0)
	hasSeed := req.Seed != nil
	if hasSeed {
		seed = *req.Seed
	}
	return mockgen.Request{
		Mode:        req.Mode,
		Count:       req.Count,
		Seed:        seed,
		HasSeed:     hasSeed,
		Fields:      fields,
		Constraints: req.Constraints,
		FKValues:    map[string][]any{},
	}
}

// fetchFKPools loads referenced value pools for FK generation
// (uniform-source for both random and sequential modes). A field may pin an
// explicit source via params ref_schema/ref_table/ref_column (the dialog's
// FK source picker); otherwise the column's own FK default is used.
// Composite FOREIGN KEYs additionally get one tuple pool per constraint:
// members share a single referenced tuple per generated row, so the engine
// can never mix values into combinations absent from the parent.
func fetchFKPools(ctx context.Context, q db.Querier, meta mockgen.TableMeta, plan *mockgen.Plan) (singles map[string][]any, tuples map[string][][]any, err error) {
	singles = map[string][]any{}
	tuples = map[string][][]any{}
	for _, f := range plan.Fields {
		if f.Generator != "foreign_key" {
			continue
		}
		rs, rt, rc, err := fkSourceFor(meta, f)
		if err != nil {
			return nil, nil, err
		}
		qt := pgx.Identifier{rs, rt}.Sanitize()
		qc := pgx.Identifier{rc}.Sanitize()
		// ORDER BY makes the pool deterministic for a fixed seed (same
		// rows feed the same RNG sequence across runs).
		_, data, err := queryJSON(q, ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL ORDER BY %s LIMIT 5000", qc, qt, qc, qc))
		if err != nil {
			return nil, nil, fmt.Errorf("foreign key %s (source %s.%s.%s): %v", f.Column, rs, rt, rc, err)
		}
		vals := make([]any, 0, len(data))
		for _, row := range data {
			if len(row) > 0 && row[0] != nil {
				vals = append(vals, row[0])
			}
		}
		singles[f.Column] = vals
	}
	for _, g := range meta.FKGroups() {
		if len(g.Local) < 2 {
			continue
		}
		cols := make([]string, len(g.Remote))
		for i, rc := range g.Remote {
			cols[i] = pgx.Identifier{rc}.Sanitize()
		}
		qt := pgx.Identifier{g.RefSchema, g.RefTable}.Sanitize()
		conds := make([]string, len(cols))
		for i, c := range cols {
			conds[i] = c + " IS NOT NULL"
		}
		sel := strings.Join(cols, ", ")
		_, data, err := queryJSON(q, ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT 5000",
			sel, qt, strings.Join(conds, " AND "), sel))
		if err != nil {
			return nil, nil, fmt.Errorf("foreign key %s (source %s.%s): %v", g.Name, g.RefSchema, g.RefTable, err)
		}
		pool := make([][]any, 0, len(data))
		for _, row := range data {
			if len(row) != len(cols) {
				continue
			}
			tup := make([]any, len(row))
			copy(tup, row)
			pool = append(pool, tup)
		}
		tuples[g.Name] = pool
	}
	return singles, tuples, nil
}

// fkPoolEmpty reports whether a foreign_key field has no usable values: a
// composite member consults its group tuple pool (unless it pins an
// explicit source), every other field its single-column pool.
func fkPoolEmpty(meta mockgen.TableMeta, f mockgen.FieldSpec, singles map[string][]any, tuples map[string][][]any) bool {
	if f.Generator != "foreign_key" {
		return false
	}
	if hasRefParams(f.Params) {
		return len(singles[f.Column]) == 0
	}
	for _, g := range meta.FKGroups() {
		if len(g.Local) < 2 {
			continue
		}
		for _, lc := range g.Local {
			if lc == f.Column {
				return len(tuples[g.Name]) == 0
			}
		}
	}
	return len(singles[f.Column]) == 0
}

func hasRefParams(p map[string]any) bool {
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

// fkSourceFor resolves the value pool for one FK field: explicit
// ref_schema/ref_table/ref_column params win, else the column's first FK.
func fkSourceFor(meta mockgen.TableMeta, f mockgen.FieldSpec) (string, string, string, error) {
	paramStr := func(key string) string {
		if f.Params == nil {
			return ""
		}
		s, _ := f.Params[key].(string)
		return strings.TrimSpace(s)
	}
	if rs, rt, rc := paramStr("ref_schema"), paramStr("ref_table"), paramStr("ref_column"); rs != "" && rt != "" && rc != "" {
		return rs, rt, rc, nil
	}
	for _, fk := range meta.ForeignKeys {
		if fk.Column == f.Column {
			return fk.RefSchema, fk.RefTable, fk.RefColumn, nil
		}
	}
	return "", "", "", fmt.Errorf("column %s has no foreign key and no source selected", f.Column)
}

// MockPreview generates rows without touching the database.
func (h *Handler) MockPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	req, id, err := decodeMockReq(r)
	if err != nil {
		code := 400
		if err.Error() == "not connected" {
			code = 401
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if req.Count < 1 {
		req.Count = 20
	}
	if req.Count > 100 {
		writeJSON(w, 400, map[string]string{"error": "preview supports at most 100 rows"})
		return
	}
	qqRaw, release, inTxn, _, ok := h.Mgr.AcquireLease(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	// One operation lease for the whole preview: the same txn (raw pgx.Tx
	// under e.mu) serves metadata, FK pools and generation, and Commit
	// blocks until release — so a concurrent Commit can't wed metadata
	// reads to a different backend than the write path observes.
	qq := logging.Wrap(qqRaw, h.Log, id)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	meta, err := loadMockMeta(ctx, qq, req.Schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	mr := toMockgenRequest(req)
	plan, err := mockgen.BuildPlan(meta, mr)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	fkVals, fkTuples, err := fetchFKPools(ctx, qq, meta, plan)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	mr.FKValues = fkVals
	plan.FKTuples = fkTuples
	// Re-plan is unnecessary: FK pools only feed generation, not planning,
	// except empty-pool NULL fallback already handled with a warning pass.
	// Re-resolve generators that flipped to null for empty pools.
	for i, f := range plan.Fields {
		if f.Generator == "foreign_key" && fkPoolEmpty(meta, f, fkVals, fkTuples) {
			if col := meta.ColumnByName(f.Column); col != nil && col.Nullable {
				plan.Fields[i].Generator = "null"
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("Referenced table for %s is empty — generating NULL (column is nullable).", f.Column))
			} else {
				writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("cannot generate %s: referenced table contains no valid values", f.Column)})
				return
			}
		}
	}
	seed := mockgen.EffectiveSeed(mr.HasSeed, mr.Seed)
	rows, err := plan.GenerateRows(seed, req.Count, fkVals)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": previewErr(req.Mode, err)})
		return
	}
	preview := make([][]any, 0, len(rows))
	for _, row := range rows {
		preview = append(preview, plan.PreviewRow(row))
	}
	warns := plan.Warnings
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, 200, map[string]any{
		"columns":  plan.AllColumnNames(),
		"rows":     preview,
		"warnings": warns,
		"seed":     seed,
		"in_txn":   inTxn,
	})
}

// MockGenerate generates rows and inserts them atomically: inside the
// session's explicit transaction when one is open (left uncommitted),
// otherwise in a private transaction committed only on full success.
func (h *Handler) MockGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	req, id, err := decodeMockReq(r)
	if err != nil {
		code := 400
		if err.Error() == "not connected" {
			code = 401
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if req.Count < 1 {
		writeJSON(w, 400, map[string]string{"error": "count must be >= 1"})
		return
	}
	if req.Count > mockgen.MaxRowsPerRequest {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("too many rows (max %d per request)", mockgen.MaxRowsPerRequest)})
		return
	}
	qqRaw, release, inTxn, pool, ok := h.Mgr.AcquireLease(id)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	// One operation lease (see MockPreview): the raw pgx.Tx under e.mu
	// serves metadata, FK pools, generation AND all INSERT batches, so a
	// concurrent Commit blocks until release instead of committing batch 1
	// and failing batch 2 (partial commit with an error to the user).
	qq := logging.Wrap(qqRaw, h.Log, id)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	meta, err := loadMockMeta(ctx, qq, req.Schema, req.Table)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	mr := toMockgenRequest(req)
	plan, err := mockgen.BuildPlan(meta, mr)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	fkVals, fkTuples, err := fetchFKPools(ctx, qq, meta, plan)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	for i, f := range plan.Fields {
		if f.Generator == "foreign_key" && fkPoolEmpty(meta, f, fkVals, fkTuples) {
			if col := meta.ColumnByName(f.Column); col != nil && col.Nullable {
				plan.Fields[i].Generator = "null"
			} else {
				writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("cannot generate %s: referenced table contains no valid values", f.Column)})
				return
			}
		}
	}
	seed := mockgen.EffectiveSeed(mr.HasSeed, mr.Seed)
	start := time.Now()
	plan.FKTuples = fkTuples
	mr.FKValues = fkVals
	rows, err := plan.GenerateRows(seed, req.Count, fkVals)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": previewErr(req.Mode, err)})
		return
	}
	var inserted int64
	if inTxn {
		n, ierr := insertRowsBatched(ctx, logging.Wrap(qqRaw, h.Log, id), req.Schema, req.Table, plan.InsertCols, rows, "")
		if ierr != nil {
			writeJSON(w, 400, map[string]string{"error": generateErr(req.Mode, ierr)})
			return
		}
		inserted = n
		writeJSON(w, 200, map[string]any{
			"generated": int64(len(rows)), "inserted": inserted,
			"seed": seed, "duration_ms": time.Since(start).Milliseconds(), "in_txn": true,
		})
		return
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	n, ierr := insertRowsBatched(ctx, logging.Wrap(tx, h.Log, id), req.Schema, req.Table, plan.InsertCols, rows, "")
	if ierr != nil {
		_ = tx.Rollback(ctx)
		writeJSON(w, 400, map[string]string{"error": generateErr(req.Mode, ierr)})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	inserted = n
	writeJSON(w, 200, map[string]any{
		"generated": int64(len(rows)), "inserted": inserted,
		"seed": seed, "duration_ms": time.Since(start).Milliseconds(), "in_txn": false,
	})
}

// previewErr tags generation-time failures; generateErr tags PostgreSQL
// insertion failures with the Simple-mode Advanced-mode hint.
func previewErr(mode string, err error) string {
	if strings.ToLower(mode) == "simple" {
		return err.Error()
	}
	return err.Error()
}

func generateErr(mode string, err error) string {
	if strings.ToLower(mode) == "simple" {
		return err.Error() + "\nUse Advanced mode to configure constraints."
	}
	return err.Error()
}
