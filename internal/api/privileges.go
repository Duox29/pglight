package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/logging"

	"github.com/jackc/pgx/v5"
)

var allowedTablePrivileges = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "TRUNCATE": true, "REFERENCES": true, "TRIGGER": true}

type privilegeChange struct {
	Role      string `json:"role"`
	Privilege string `json:"privilege"`
	Granted   bool   `json:"granted"`
}

type policyRequest struct {
	Operation  string   `json:"operation"`
	Schema     string   `json:"schema"`
	Table      string   `json:"table"`
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Permissive string   `json:"permissive"`
	Roles      []string `json:"roles"`
	Using      string   `json:"using"`
	WithCheck  string   `json:"with_check"`
}

type privilegeRequest struct {
	SessionID string            `json:"session_id"`
	Action    string            `json:"action"`
	Schema    string            `json:"schema"`
	Table     string            `json:"table"`
	Changes   []privilegeChange `json:"changes"`
	Policy    policyRequest     `json:"policy"`
}

func (h *Handler) Privileges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getPrivileges(w, r)
	case http.MethodPost:
		h.changePrivileges(w, r)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (h *Handler) getPrivileges(w http.ResponseWriter, r *http.Request) {
	q, sid, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	schema, table := r.URL.Query().Get("schema"), r.URL.Query().Get("table")
	if schema == "" || table == "" {
		writeJSON(w, 400, map[string]string{"error": "schema and table are required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, roles, err := queryJSON(q, ctx, `SELECT rolname, rolsuper, rolcreaterole, rolcreatedb, rolcanlogin FROM pg_roles WHERE rolname !~ '^pg_' ORDER BY rolname`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, grants, err := queryJSON(q, ctx, `SELECT COALESCE(grantee.rolname,'PUBLIC'), acl.privilege_type, acl.is_grantable FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault('r',c.relowner))) acl LEFT JOIN pg_roles grantee ON grantee.oid=acl.grantee WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind IN ('r','p','v','m','f') ORDER BY 1,2`, schema, table)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, rel, err := queryJSON(q, ctx, `SELECT c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind IN ('r','p')`, schema, table)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, policies, err := queryJSON(q, ctx, `SELECT policyname, permissive, roles, cmd, qual, with_check FROM pg_policies WHERE schemaname=$1 AND tablename=$2 ORDER BY policyname`, schema, table)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var rlsEnabled, rlsForced any
	if len(rel) > 0 && len(rel[0]) >= 2 {
		rlsEnabled, rlsForced = rel[0][0], rel[0][1]
	}
	writeJSON(w, 200, map[string]any{"session_id": sid, "schema": schema, "table": table, "roles": rowsToMaps([]string{"name", "superuser", "create_role", "create_db", "login"}, roles), "grants": rowsToMaps([]string{"role", "privilege", "grantable"}, grants), "rls": map[string]any{"enabled": rlsEnabled, "forced": rlsForced, "policies": rowsToMaps([]string{"name", "permissive", "roles", "command", "using", "with_check"}, policies), "in_txn": h.Mgr.InTxn(sid)}})
}

func (h *Handler) changePrivileges(w http.ResponseWriter, r *http.Request) {
	var req privilegeRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	sid := sessionFromBody(req.SessionID, r)
	if h.Mgr == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	if _, ok := h.Mgr.Q(sid); !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	var statements []string
	var err error
	switch req.Action {
	case "preview_grants", "apply_grants":
		statements, err = buildPrivilegeStatements(req.Schema, req.Table, req.Changes)
	case "preview_policy", "apply_policy":
		statements, err = buildPolicyStatements(req.Policy)
	default:
		writeJSON(w, 400, map[string]string{"error": "action must be preview_grants, apply_grants, preview_policy, or apply_policy"})
		return
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if strings.HasPrefix(req.Action, "preview_") {
		writeJSON(w, 200, map[string]any{"sql": statements})
		return
	}
	lease, release, inTxn, pool, ok := h.Mgr.AcquireLease(sid)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var q db.Querier
	var tx pgx.Tx
	if inTxn {
		q = logging.Wrap(lease, h.Log, sid)
		if _, err = q.Exec(ctx, "SAVEPOINT pglight_privileges"); err == nil {
			for _, statement := range statements {
				if _, err = q.Exec(ctx, statement); err != nil {
					break
				}
			}
		}
		if err != nil {
			_, _ = q.Exec(ctx, "ROLLBACK TO SAVEPOINT pglight_privileges")
			_, _ = q.Exec(ctx, "RELEASE SAVEPOINT pglight_privileges")
		} else {
			_, err = q.Exec(ctx, "RELEASE SAVEPOINT pglight_privileges")
		}
	} else {
		tx, err = pool.BeginTx(ctx, pgx.TxOptions{})
		if err == nil {
			q = logging.Wrap(tx, h.Log, sid)
			for _, statement := range statements {
				if _, err = q.Exec(ctx, statement); err != nil {
					break
				}
			}
			if err == nil {
				err = tx.Commit(ctx)
			}
			if err != nil {
				_ = tx.Rollback(context.Background())
			}
		}
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "in_txn": h.Mgr.InTxn(sid)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "count": len(statements), "in_txn": h.Mgr.InTxn(sid)})
}

func buildPrivilegeStatements(schema, table string, changes []privilegeChange) ([]string, error) {
	if strings.TrimSpace(schema) == "" || strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("schema and table are required")
	}
	if len(changes) == 0 || len(changes) > 100 {
		return nil, fmt.Errorf("change count must be between 1 and 100")
	}
	target := pgx.Identifier{schema, table}.Sanitize()
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		privilege := strings.ToUpper(strings.TrimSpace(change.Privilege))
		if !allowedTablePrivileges[privilege] {
			return nil, fmt.Errorf("unsupported table privilege %q", change.Privilege)
		}
		role := strings.TrimSpace(change.Role)
		if role == "" {
			return nil, fmt.Errorf("role is required")
		}
		roleSQL := pgx.Identifier{role}.Sanitize()
		if strings.EqualFold(role, "PUBLIC") {
			roleSQL = "PUBLIC"
		}
		verb, prep := "GRANT", "TO"
		if !change.Granted {
			verb, prep = "REVOKE", "FROM"
		}
		out = append(out, verb+" "+privilege+" ON TABLE "+target+" "+prep+" "+roleSQL)
	}
	return out, nil
}

func buildPolicyStatements(req policyRequest) ([]string, error) {
	target := pgx.Identifier{req.Schema, req.Table}.Sanitize()
	if strings.TrimSpace(req.Schema) == "" || strings.TrimSpace(req.Table) == "" {
		return nil, fmt.Errorf("schema and table are required")
	}
	switch req.Operation {
	case "replace_policy":
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("policy name is required")
		}
		drop := "DROP POLICY IF EXISTS " + pgx.Identifier{req.Name}.Sanitize() + " ON " + target
		req.Operation = "create_policy"
		create, err := buildPolicyStatements(req)
		if err != nil {
			return nil, err
		}
		return append([]string{drop}, create...), nil
	case "enable_rls":
		return []string{"ALTER TABLE " + target + " ENABLE ROW LEVEL SECURITY"}, nil
	case "disable_rls":
		return []string{"ALTER TABLE " + target + " DISABLE ROW LEVEL SECURITY"}, nil
	case "force_rls":
		return []string{"ALTER TABLE " + target + " FORCE ROW LEVEL SECURITY"}, nil
	case "no_force_rls":
		return []string{"ALTER TABLE " + target + " NO FORCE ROW LEVEL SECURITY"}, nil
	case "drop_policy":
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("policy name is required")
		}
		return []string{"DROP POLICY IF EXISTS " + pgx.Identifier{req.Name}.Sanitize() + " ON " + target}, nil
	case "create_policy":
		if strings.TrimSpace(req.Name) == "" || len(req.Name) > 200 {
			return nil, fmt.Errorf("policy name must be 1-200 characters")
		}
		command := strings.ToUpper(strings.TrimSpace(req.Command))
		if command != "ALL" && command != "SELECT" && command != "INSERT" && command != "UPDATE" && command != "DELETE" {
			return nil, fmt.Errorf("unsupported policy command")
		}
		permissive := strings.ToUpper(strings.TrimSpace(req.Permissive))
		if permissive == "" {
			permissive = "PERMISSIVE"
		}
		if permissive != "PERMISSIVE" && permissive != "RESTRICTIVE" {
			return nil, fmt.Errorf("policy mode must be PERMISSIVE or RESTRICTIVE")
		}
		if len(req.Roles) == 0 || len(req.Roles) > 100 {
			return nil, fmt.Errorf("select between 1 and 100 policy roles")
		}
		for _, role := range req.Roles {
			if strings.TrimSpace(role) == "" {
				return nil, fmt.Errorf("policy role is required")
			}
		}
		if req.Using != "" && command == "INSERT" {
			return nil, fmt.Errorf("INSERT policies cannot have a USING expression")
		}
		if req.WithCheck != "" && command != "INSERT" && command != "UPDATE" && command != "ALL" {
			return nil, fmt.Errorf("WITH CHECK is supported only for INSERT, UPDATE, or ALL policies")
		}
		if err := validatePolicyExpression(req.Using); err != nil {
			return nil, err
		}
		if err := validatePolicyExpression(req.WithCheck); err != nil {
			return nil, err
		}
		roles := make([]string, len(req.Roles))
		for i, role := range req.Roles {
			if strings.EqualFold(role, "PUBLIC") {
				roles[i] = "PUBLIC"
			} else {
				roles[i] = pgx.Identifier{role}.Sanitize()
			}
		}
		sql := "CREATE POLICY " + pgx.Identifier{req.Name}.Sanitize() + " ON " + target + " AS " + permissive + " FOR " + command + " TO " + strings.Join(roles, ", ")
		if req.Using != "" {
			sql += " USING (" + req.Using + ")"
		}
		if req.WithCheck != "" {
			sql += " WITH CHECK (" + req.WithCheck + ")"
		}
		return []string{sql}, nil
	default:
		return nil, fmt.Errorf("unsupported policy operation")
	}
}

func validatePolicyExpression(expression string) error {
	if len(expression) > 16384 {
		return fmt.Errorf("policy expression exceeds 16 KiB")
	}
	if strings.ContainsAny(expression, ";\x00") || strings.Contains(expression, "--") || strings.Contains(expression, "/*") || strings.Contains(expression, "*/") {
		return fmt.Errorf("policy expressions cannot contain statement separators or comments")
	}
	return nil
}
