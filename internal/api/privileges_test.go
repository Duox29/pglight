package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildPrivilegeStatementsQuotesIdentifiersAndAllowListsPrivileges(t *testing.T) {
	got, err := buildPrivilegeStatements(`odd"schema`, `user-data`, []privilegeChange{{Role: `reader"x`, Privilege: "select", Granted: true}, {Role: "PUBLIC", Privilege: "UPDATE", Granted: false}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != `GRANT SELECT ON TABLE "odd""schema"."user-data" TO "reader""x"` || got[1] != `REVOKE UPDATE ON TABLE "odd""schema"."user-data" FROM PUBLIC` {
		t.Fatalf("statements=%q", got)
	}
	if _, err := buildPrivilegeStatements("public", "t", []privilegeChange{{Role: "app", Privilege: "SELECT; DROP TABLE users", Granted: true}}); err == nil {
		t.Fatal("arbitrary privilege string must be rejected")
	}
	if _, err := buildPrivilegeStatements("public", "t", make([]privilegeChange, 101)); err == nil {
		t.Fatal("oversized change batch should be rejected")
	}
}

func TestBuildRLSStatementsConstrainsPolicySQLAndQuotesNames(t *testing.T) {
	got, err := buildPolicyStatements(policyRequest{Operation: "create_policy", Schema: "tenant space", Table: "account", Name: `read"own`, Command: "SELECT", Roles: []string{"tenant_user"}, Using: "tenant_id = current_setting('app.tenant')::uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("policy statements=%q", got)
	}
	for _, part := range []string{`"read""own"`, `"tenant space"."account"`, `TO "tenant_user"`, `USING (tenant_id = current_setting('app.tenant')::uuid)`} {
		if !strings.Contains(got[0], part) {
			t.Fatalf("SQL %q missing %q", got[0], part)
		}
	}
	if _, err := buildPolicyStatements(policyRequest{Operation: "create_policy", Schema: "public", Table: "t", Name: "p", Command: "SELECT", Using: "true); DROP TABLE users; --"}); err == nil {
		t.Fatal("policy expression must reject statement separators and comments")
	}
	if _, err := buildPolicyStatements(policyRequest{Operation: "create_policy", Schema: "public", Table: "t", Name: "p", Command: "GRANT", Using: "true"}); err == nil {
		t.Fatal("policy command must be allow-listed")
	}
}

func TestBuildRLSReplaceAndExpressionLimits(t *testing.T) {
	got, err := buildPolicyStatements(policyRequest{Operation: "replace_policy", Schema: "public", Table: "items", Name: "tenant_policy", Command: "UPDATE", Permissive: "RESTRICTIVE", Roles: []string{"PUBLIC"}, Using: "tenant_id = 7", WithCheck: "tenant_id = 7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], `DROP POLICY IF EXISTS "tenant_policy"`) || !strings.HasPrefix(got[1], `CREATE POLICY "tenant_policy"`) {
		t.Fatalf("replace statements=%q", got)
	}
	if !strings.Contains(got[1], " AS RESTRICTIVE FOR UPDATE ") {
		t.Fatalf("policy mode missing from SQL: %q", got[1])
	}
	if _, err := buildPolicyStatements(policyRequest{Operation: "create_policy", Schema: "public", Table: "items", Name: "insert_policy", Command: "INSERT", Roles: []string{"app"}, Using: "true"}); err == nil {
		t.Fatal("INSERT policy must reject USING")
	}
	if _, err := buildPolicyStatements(policyRequest{Operation: "create_policy", Schema: "public", Table: "items", Name: "large", Command: "SELECT", Roles: []string{"app"}, Using: strings.Repeat("x", 16385)}); err == nil {
		t.Fatal("oversized expressions must be rejected")
	}
}

func TestPrivilegesRequiresConnectedSessionForCatalogAndChanges(t *testing.T) {
	h := &Handler{}
	get := httptest.NewRecorder()
	h.Privileges(get, httptest.NewRequest("GET", "/api/privileges?schema=public&table=items", nil))
	if get.Code != 401 {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body)
	}
	post := httptest.NewRecorder()
	h.Privileges(post, httptest.NewRequest("POST", "/api/privileges", strings.NewReader(`{"session_id":"missing","action":"preview_grants","schema":"public","table":"items","changes":[{"role":"reader","privilege":"SELECT","granted":true}]}`)))
	if post.Code != 401 {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body)
	}
}
