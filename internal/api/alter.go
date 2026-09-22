package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"

	"github.com/jackc/pgx/v5"
)

// AlterTable runs pgAdmin-guarded DDL ops for columns, constraints, indexes
// and triggers. Identifiers are sanitized via pgx.Identifier; type names are
// validated with to_regtype() before interpolation (supports custom
// enum/domain types from the smart suggest list); default/constraint/
// predicate expressions reject statement separators.
func (h *Handler) AlterTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session    string   `json:"session_id"`
		Schema     string   `json:"schema"`
		Table      string   `json:"table"`
		Op         string   `json:"op"`
		Column     string   `json:"column"`
		NewName    string   `json:"new_name"`
		Type       string   `json:"type"`
		Nullable   *bool    `json:"nullable"`
		Default    string   `json:"default"`
		DropDef    bool     `json:"drop_default"`
		Constraint string   `json:"constraint"`
		Def        string   `json:"def"`
		Cascade    bool     `json:"cascade"`
		Index      string   `json:"index"`
		Unique     bool     `json:"unique"`
		Method     string   `json:"method"`
		Columns    []string `json:"columns"`
		Include    []string `json:"include"`
		Where      string   `json:"where"`
		Trigger    string   `json:"trigger"`
		Timing     string   `json:"timing"`
		Events     []string `json:"events"`
		ForEach    string   `json:"for_each"`
		Function   string   `json:"function"`
		When       string   `json:"when"`
		UpdateOf   []string `json:"update_of"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	qq, ok := h.Mgr.Q(id)
	if id == "" || !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	qq = logging.Wrap(qq, h.Log, id)
	schema := strings.TrimSpace(req.Schema)
	if schema == "" {
		schema = "public"
	}
	if strings.TrimSpace(req.Table) == "" {
		writeJSON(w, 400, map[string]string{"error": "table required"})
		return
	}
	if err := checkIdent(req.Table); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad table name"})
		return
	}
	if err := checkIdent(schema); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad schema name"})
		return
	}
	qt := pgx.Identifier{schema, req.Table}.Sanitize()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var sql string
	switch strings.ToLower(strings.TrimSpace(req.Op)) {
	case "add_column":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		typ, err := validateType(ctx, qq, req.Type)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		col := pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", qt, col, typ))
		if req.Nullable != nil && !*req.Nullable {
			sb.WriteString(" NOT NULL")
		}
		if strings.TrimSpace(req.Default) != "" {
			if err := checkExpr(req.Default); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			sb.WriteString(" DEFAULT " + strings.TrimSpace(req.Default))
		}
		sql = sb.String()
	case "drop_column":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		col := pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize()
		sql = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qt, col)
		if req.Cascade {
			sql += " CASCADE"
		}
	case "rename_column":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		if err := checkIdent(req.NewName); err != nil {
			writeJSON(w, 400, map[string]string{"error": "new_name required"})
			return
		}
		sql = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", qt,
			pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize(),
			pgx.Identifier{strings.TrimSpace(req.NewName)}.Sanitize())
	case "alter_type":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		typ, err := validateType(ctx, qq, req.Type)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		col := pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize()
		sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DATA TYPE %s USING %s::%s", qt, col, typ, col, typ)
	case "set_nullable":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		if req.Nullable == nil {
			writeJSON(w, 400, map[string]string{"error": "nullable required"})
			return
		}
		col := pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize()
		if *req.Nullable {
			sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", qt, col)
		} else {
			sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", qt, col)
		}
	case "set_default":
		if err := checkIdent(req.Column); err != nil {
			writeJSON(w, 400, map[string]string{"error": "column required"})
			return
		}
		col := pgx.Identifier{strings.TrimSpace(req.Column)}.Sanitize()
		if req.DropDef || strings.TrimSpace(req.Default) == "" {
			sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", qt, col)
		} else {
			if err := checkExpr(req.Default); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", qt, col, strings.TrimSpace(req.Default))
		}
	case "add_constraint":
		def := strings.TrimSpace(req.Def)
		if def == "" {
			writeJSON(w, 400, map[string]string{"error": "def required (e.g. CHECK (price > 0))"})
			return
		}
		if err := checkConstraintDef(def); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.Constraint) != "" {
			if err := checkIdent(req.Constraint); err != nil {
				writeJSON(w, 400, map[string]string{"error": "bad constraint name"})
				return
			}
			sql = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s", qt,
				pgx.Identifier{strings.TrimSpace(req.Constraint)}.Sanitize(), def)
		} else {
			sql = fmt.Sprintf("ALTER TABLE %s ADD %s", qt, def)
		}
	case "drop_constraint":
		if err := checkIdent(req.Constraint); err != nil {
			writeJSON(w, 400, map[string]string{"error": "constraint required"})
			return
		}
		sql = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", qt,
			pgx.Identifier{strings.TrimSpace(req.Constraint)}.Sanitize())
		if req.Cascade {
			sql += " CASCADE"
		}
	case "rename_table":
		if err := checkIdent(req.NewName); err != nil {
			writeJSON(w, 400, map[string]string{"error": "new_name required"})
			return
		}
		sql = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", qt,
			pgx.Identifier{strings.TrimSpace(req.NewName)}.Sanitize())
	case "create_index":
		if len(req.Columns) == 0 {
			writeJSON(w, 400, map[string]string{"error": "columns required (e.g. [\"email\"] or [\"(lower(email))\"])"})
			return
		}
		keys := make([]string, 0, len(req.Columns))
		for _, k := range req.Columns {
			kk, err := checkIndexKey(k)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			keys = append(keys, kk)
		}
		method := strings.ToLower(strings.TrimSpace(req.Method))
		if method == "" {
			method = "btree"
		}
		if !indexMethods[method] {
			writeJSON(w, 400, map[string]string{"error": "unknown method (btree|hash|gin|gist|spgist|brin)"})
			return
		}
		var sb strings.Builder
		sb.WriteString("CREATE")
		if req.Unique {
			sb.WriteString(" UNIQUE")
		}
		sb.WriteString(" INDEX ")
		if strings.TrimSpace(req.Index) != "" {
			if err := checkIdent(req.Index); err != nil {
				writeJSON(w, 400, map[string]string{"error": "bad index name"})
				return
			}
			// NOTE: CREATE INDEX forbids a schema-qualified name — the index
			// always lands in its table's schema.
			sb.WriteString(pgx.Identifier{strings.TrimSpace(req.Index)}.Sanitize() + " ")
		}
		sb.WriteString(fmt.Sprintf("ON %s USING %s (%s)", qt, method, strings.Join(keys, ", ")))
		if len(req.Include) > 0 {
			inc := make([]string, 0, len(req.Include))
			for _, c := range req.Include {
				if err := checkIdent(c); err != nil {
					writeJSON(w, 400, map[string]string{"error": "bad INCLUDE column " + strconv.Quote(strings.TrimSpace(c))})
					return
				}
				inc = append(inc, pgx.Identifier{strings.TrimSpace(c)}.Sanitize())
			}
			sb.WriteString(" INCLUDE (" + strings.Join(inc, ", ") + ")")
		}
		if strings.TrimSpace(req.Where) != "" {
			if err := checkExpr(req.Where); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			sb.WriteString(" WHERE " + strings.TrimSpace(req.Where))
		}
		sql = sb.String()
	case "drop_index":
		if err := checkIdent(req.Index); err != nil {
			writeJSON(w, 400, map[string]string{"error": "index required"})
			return
		}
		sql = "DROP INDEX " + pgx.Identifier{schema, strings.TrimSpace(req.Index)}.Sanitize()
		if req.Cascade {
			sql += " CASCADE"
		}
	case "rename_index":
		if err := checkIdent(req.Index); err != nil {
			writeJSON(w, 400, map[string]string{"error": "index required"})
			return
		}
		if err := checkIdent(req.NewName); err != nil {
			writeJSON(w, 400, map[string]string{"error": "new_name required"})
			return
		}
		sql = fmt.Sprintf("ALTER INDEX %s RENAME TO %s",
			pgx.Identifier{schema, strings.TrimSpace(req.Index)}.Sanitize(),
			pgx.Identifier{strings.TrimSpace(req.NewName)}.Sanitize())
	case "create_trigger":
		if err := checkIdent(req.Trigger); err != nil {
			writeJSON(w, 400, map[string]string{"error": "trigger name required"})
			return
		}
		timing := strings.ToUpper(strings.TrimSpace(req.Timing))
		if timing != "BEFORE" && timing != "AFTER" && timing != "INSTEAD OF" {
			writeJSON(w, 400, map[string]string{"error": "timing must be BEFORE|AFTER|INSTEAD OF"})
			return
		}
		events, err := checkTriggerEvents(req.Events, req.UpdateOf)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		forEach := strings.ToUpper(strings.TrimSpace(req.ForEach))
		if forEach == "" {
			forEach = "ROW"
		}
		if forEach != "ROW" && forEach != "STATEMENT" {
			writeJSON(w, 400, map[string]string{"error": "for_each must be ROW|STATEMENT"})
			return
		}
		fn, err := checkTriggerFunc(req.Function)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("CREATE TRIGGER %s %s %s ON %s FOR EACH %s",
			pgx.Identifier{strings.TrimSpace(req.Trigger)}.Sanitize(), timing, events, qt, forEach))
		if strings.TrimSpace(req.When) != "" {
			if err := checkExpr(req.When); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			sb.WriteString(" WHEN (" + strings.TrimSpace(req.When) + ")")
		}
		sb.WriteString(" EXECUTE FUNCTION " + fn)
		sql = sb.String()
	case "drop_trigger":
		if err := checkIdent(req.Trigger); err != nil {
			writeJSON(w, 400, map[string]string{"error": "trigger required"})
			return
		}
		sql = fmt.Sprintf("DROP TRIGGER %s ON %s",
			pgx.Identifier{strings.TrimSpace(req.Trigger)}.Sanitize(), qt)
		if req.Cascade {
			sql += " CASCADE"
		}
	case "enable_trigger", "disable_trigger":
		if err := checkIdent(req.Trigger); err != nil {
			writeJSON(w, 400, map[string]string{"error": "trigger required"})
			return
		}
		verb := "ENABLE"
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Op)), "disable") {
			verb = "DISABLE"
		}
		sql = fmt.Sprintf("ALTER TABLE %s %s TRIGGER %s", qt, verb,
			pgx.Identifier{strings.TrimSpace(req.Trigger)}.Sanitize())
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown op (add_column|drop_column|rename_column|alter_type|set_nullable|set_default|add_constraint|drop_constraint|rename_table|create_index|drop_index|rename_index|create_trigger|drop_trigger|enable_trigger|disable_trigger)"})
		return
	}
	if _, err := qq.Exec(ctx, sql); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	globalComplete.Invalidate(id)
	writeJSON(w, 200, map[string]any{"ok": true, "in_txn": h.Mgr.InTxn(id)})
}

func checkIdent(s string) error {
	t := strings.TrimSpace(s)
	if t == "" || len(t) > 63 || strings.Contains(t, "\x00") {
		return fmt.Errorf("bad identifier")
	}
	return nil
}

func checkExpr(s string) error {
	t := strings.TrimSpace(s)
	if t == "" || len(t) > 500 {
		return fmt.Errorf("bad expression")
	}
	if strings.Contains(t, ";") {
		return fmt.Errorf("expression must not contain ';'")
	}
	return nil
}

var indexMethods = map[string]bool{"btree": true, "hash": true, "gin": true, "gist": true, "spgist": true, "brin": true}

var indexDirRe = regexp.MustCompile(`(?i)^(ASC|DESC)(\s+NULLS\s+(FIRST|LAST))?$`)

// checkIndexKey validates one CREATE INDEX key spec: a plain column with an
// optional ASC/DESC [NULLS FIRST|LAST] suffix, or a parenthesized expression
// index. It returns the sanitized spec for interpolation.
func checkIndexKey(spec string) (string, error) {
	t := strings.TrimSpace(spec)
	if t == "" || len(t) > 200 || strings.Contains(t, ";") {
		return "", fmt.Errorf("bad index key %q", spec)
	}
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		return t, nil
	}
	parts := strings.Fields(t)
	if err := checkIdent(parts[0]); err != nil {
		return "", fmt.Errorf("bad index column %q", parts[0])
	}
	rest := strings.TrimSpace(t[len(parts[0]):])
	if rest != "" && !indexDirRe.MatchString(rest) {
		return "", fmt.Errorf("bad index direction in %q (use ASC|DESC [NULLS FIRST|LAST])", spec)
	}
	if rest == "" {
		return pgx.Identifier{parts[0]}.Sanitize(), nil
	}
	return pgx.Identifier{parts[0]}.Sanitize() + " " + strings.ToUpper(rest), nil
}

var triggerFuncRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*)*\s*(\(.*\))?$`)

func checkTriggerFunc(fn string) (string, error) {
	t := strings.TrimSpace(fn)
	if t == "" || len(t) > 500 || strings.Contains(t, ";") {
		return "", fmt.Errorf("function required (e.g. audit_fn())")
	}
	if !triggerFuncRe.MatchString(t) {
		return "", fmt.Errorf("bad function %q (use [schema.]name[(args)])", t)
	}
	return t, nil
}

// checkTriggerEvents validates the event list and builds the
// "INSERT OR UPDATE OF a, b OR DELETE" fragment.
func checkTriggerEvents(events []string, updateOf []string) (string, error) {
	if len(events) == 0 {
		return "", fmt.Errorf("at least one event required (INSERT|UPDATE|DELETE|TRUNCATE)")
	}
	seen := map[string]bool{}
	outs := []string{}
	for _, e := range events {
		u := strings.ToUpper(strings.TrimSpace(e))
		if u != "INSERT" && u != "UPDATE" && u != "DELETE" && u != "TRUNCATE" {
			return "", fmt.Errorf("bad event %q (INSERT|UPDATE|DELETE|TRUNCATE)", e)
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		if u == "UPDATE" && len(updateOf) > 0 {
			cols := make([]string, 0, len(updateOf))
			for _, c := range updateOf {
				if err := checkIdent(c); err != nil {
					return "", fmt.Errorf("bad UPDATE OF column %q", c)
				}
				cols = append(cols, pgx.Identifier{strings.TrimSpace(c)}.Sanitize())
			}
			outs = append(outs, "UPDATE OF "+strings.Join(cols, ", "))
		} else {
			outs = append(outs, u)
		}
	}
	return strings.Join(outs, " OR "), nil
}

var constraintHeadRe = regexp.MustCompile(`(?i)^(CHECK\s*\(|UNIQUE|PRIMARY\s+KEY|FOREIGN\s+KEY|EXCLUDE)`)

func checkConstraintDef(def string) error {
	if len(def) > 2000 || strings.Contains(def, ";") {
		return fmt.Errorf("bad constraint definition")
	}
	if !constraintHeadRe.MatchString(strings.TrimSpace(def)) {
		return fmt.Errorf("constraint must start with CHECK|UNIQUE|PRIMARY KEY|FOREIGN KEY|EXCLUDE")
	}
	return nil
}

// validateType allow-lists the shape and confirms the type exists via
// to_regtype (param-bound), then returns the trimmed original for use.
func validateType(ctx context.Context, qq db.Querier, typ string) (string, error) {
	t := strings.TrimSpace(typ)
	if t == "" || len(t) > 100 {
		return "", fmt.Errorf("type required")
	}
	if strings.Contains(t, ";") || strings.Contains(t, "'") || strings.Contains(t, "--") || strings.Contains(t, "/*") {
		return "", fmt.Errorf("bad type")
	}
	var got *string
	if err := qq.QueryRow(ctx, `SELECT to_regtype($1::text)::text`, t).Scan(&got); err != nil {
		return "", fmt.Errorf("unknown type %q", t)
	}
	if got == nil || strings.TrimSpace(*got) == "" {
		return "", fmt.Errorf("unknown type %q", t)
	}
	return t, nil
}
