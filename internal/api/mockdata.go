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
		GROUP BY a.attname HAVING count(*)>=1`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range urows {
		uniqCols[fmt.Sprint(r[0])] = true
	}
	// Unique indexes also enforce uniqueness (e.g. UNIQUE(email) via CREATE
	// UNIQUE INDEX): treat single-column unique-index members as unique.
	_, irows, err := queryJSON(q, ctx, `
		SELECT a.attname FROM pg_index ix
		JOIN pg_class t ON t.oid=ix.indrelid
		JOIN pg_namespace n ON n.oid=t.relnamespace
		JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=ix.indkey[0]
		WHERE n.nspname=$1 AND t.relname=$2 AND ix.indisunique AND array_length(ix.indkey,1)=1`, schema, table)
	if err != nil {
		return meta, err
	}
	for _, r := range irows {
		uniqCols[fmt.Sprint(r[0])] = true
	}
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
		SELECT con.conname, src_att.attname, dst_ns.nspname, dst.relname, dst_att.attname
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
		if len(r) < 5 {
			continue
		}
		meta.ForeignKeys = append(meta.ForeignKeys, mockgen.ForeignKeyMeta{
			Name:      fmt.Sprint(r[0]),
			Column:    fmt.Sprint(r[1]),
			RefSchema: fmt.Sprint(r[2]),
			RefTable:  fmt.Sprint(r[3]),
			RefColumn: fmt.Sprint(r[4]),
		})
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
		_, kind, ok := mockgen.ParseCheckHint(def)
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

// fetchFKValues loads referenced-column value pools for FK generation
// (uniform-source for both random and sequential modes). A field may pin an
// explicit source via params ref_schema/ref_table/ref_column (the dialog's
// FK source picker); otherwise the column's own FK default is used.
func fetchFKValues(ctx context.Context, q db.Querier, meta mockgen.TableMeta, plan *mockgen.Plan) (map[string][]any, error) {
	out := map[string][]any{}
	for _, f := range plan.Fields {
		if f.Generator != "foreign_key" {
			continue
		}
		rs, rt, rc, err := fkSourceFor(meta, f)
		if err != nil {
			return nil, err
		}
		qt := pgx.Identifier{rs, rt}.Sanitize()
		qc := pgx.Identifier{rc}.Sanitize()
		_, data, err := queryJSON(q, ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL LIMIT 5000", qc, qt, qc))
		if err != nil {
			return nil, fmt.Errorf("foreign key %s (source %s.%s.%s): %v", f.Column, rs, rt, rc, err)
		}
		vals := make([]any, 0, len(data))
		for _, row := range data {
			if len(row) > 0 && row[0] != nil {
				vals = append(vals, row[0])
			}
		}
		out[f.Column] = vals
	}
	return out, nil
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
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
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
	fkVals, err := fetchFKValues(ctx, qq, meta, plan)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	mr.FKValues = fkVals
	// Re-plan is unnecessary: FK pools only feed generation, not planning,
	// except empty-pool NULL fallback already handled with a warning pass.
	// Re-resolve generators that flipped to null for empty pools.
	for i, f := range plan.Fields {
		if f.Generator == "foreign_key" && len(fkVals[f.Column]) == 0 {
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
		"in_txn":   h.Mgr.InTxn(id),
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
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
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
	fkVals, err := fetchFKValues(ctx, qq, meta, plan)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	for i, f := range plan.Fields {
		if f.Generator == "foreign_key" && len(fkVals[f.Column]) == 0 {
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
	rows, err := plan.GenerateRows(seed, req.Count, fkVals)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": previewErr(req.Mode, err)})
		return
	}
	var inserted int64
	if h.Mgr.InTxn(id) {
		qqRaw, _ := h.Mgr.Q(id)
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
	pool, hasPool := h.Mgr.Get(id)
	if !hasPool {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
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
