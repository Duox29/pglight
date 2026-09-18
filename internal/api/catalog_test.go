package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func callGET(t *testing.T, h *Handler, path string) (int, string) {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	switch {
	case strings.HasPrefix(path, "/api/objects"):
		h.Objects(w, r)
	case strings.HasPrefix(path, "/api/columns"):
		h.Columns(w, r)
	case strings.HasPrefix(path, "/api/erd"):
		h.ERD(w, r)
	case strings.HasPrefix(path, "/api/view-def"):
		h.ViewDef(w, r)
	case strings.HasPrefix(path, "/api/constraints"):
		h.Constraints(w, r)
	default:
		t.Fatalf("unknown path %s", path)
	}
	return w.Code, w.Body.String()
}

func TestCatalogQuotedIdentifiers(t *testing.T) {
	h, sid := testMgr(t)
	execSQL(t, h, sid, `DROP SCHEMA IF EXISTS "My Schema" CASCADE`)
	execSQL(t, h, sid, `CREATE SCHEMA "My Schema"`)
	defer execSQL(t, h, sid, `DROP SCHEMA "My Schema" CASCADE`)
	execSQL(t, h, sid, `CREATE TABLE "My Schema"."Order.Items" (id int PRIMARY KEY, note text)`)
	execSQL(t, h, sid, `COMMENT ON TABLE "My Schema"."Order.Items" IS 'weird but valid'`)
	execSQL(t, h, sid, `CREATE VIEW "My Schema"."Odd View" AS SELECT id FROM "My Schema"."Order.Items"`)

	q := fmt.Sprintf("session_id=%s&schema=%s", sid, "My+Schema")
	if code, body := callGET(t, h, "/api/columns?"+q+"&table=Order.Items"); code != 200 || !strings.Contains(body, `"pk":true`) {
		t.Fatalf("columns on quoted table: %d %s", code, body)
	}
	if code, body := callGET(t, h, "/api/view-def?"+q+"&name=Odd+View"); code != 200 || !strings.Contains(body, "Order.Items") {
		t.Fatalf("view-def on quoted view: %d %s", code, body)
	}
	if code, body := callGET(t, h, "/api/constraints?"+q+"&table=Order.Items"); code != 200 || !strings.Contains(body, "PRIMARY KEY") {
		t.Fatalf("constraints on quoted table: %d %s", code, body)
	}
	// Sizes must resolve too (previously blew up on the naive cast).
	qq, _ := h.Mgr.Q(sid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := queryJSON(qq, ctx,
		`SELECT pg_size_pretty(pg_total_relation_size(to_regclass(format('%I.%I', $1::text, $2::text))))`, "My Schema", "Order.Items")
	if err != nil || len(data) != 1 {
		t.Fatalf("quoted size lookup: %v %v", data, err)
	}
}

func TestForeignTablesFilter(t *testing.T) {
	h, sid := testMgr(t)
	code, body := callGET(t, h, fmt.Sprintf("/api/objects?kind=foreign&session_id=%s&schema=public", sid))
	if code != 200 {
		t.Fatalf("foreign filter: %d %s", code, body)
	}
}

// Two tables with identically-named FK constraints must not cross-link in
// the ERD. The old information_schema join on constraint_name did.
func TestERDNoCrossMatch(t *testing.T) {
	h, sid := testMgr(t)
	execSQL(t, h, sid, `DROP SCHEMA IF EXISTS pglight_erd CASCADE`)
	execSQL(t, h, sid, `CREATE SCHEMA pglight_erd`)
	defer execSQL(t, h, sid, `DROP SCHEMA pglight_erd CASCADE`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.users (id int PRIMARY KEY)`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.companies (id int PRIMARY KEY)`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.orders (id int PRIMARY KEY, user_id int CONSTRAINT fk_user_id REFERENCES pglight_erd.users(id))`)
	execSQL(t, h, sid, `CREATE TABLE pglight_erd.invoices (id int PRIMARY KEY, user_id int CONSTRAINT fk_user_id REFERENCES pglight_erd.companies(id))`)

	code, body := callGET(t, h, fmt.Sprintf("/api/erd?session_id=%s&schema=pglight_erd", sid))
	if code != 200 {
		t.Fatalf("erd: %d %s", code, body)
	}
	// orders.fk_user_id -> users, invoices.fk_user_id -> companies, and no
	// orders->companies or invoices->users edge may exist.
	for _, want := range []string{
		`"dst_table":"users","fk":"fk_user_id","src_col":"user_id","src_table":"orders"`,
		`"dst_table":"companies","fk":"fk_user_id","src_col":"user_id","src_table":"invoices"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing edge %s in %s", want, body)
		}
	}
	for _, bad := range []string{
		`"dst_table":"companies","fk":"fk_user_id","src_col":"user_id","src_table":"orders"`,
		`"dst_table":"users","fk":"fk_user_id","src_col":"user_id","src_table":"invoices"`,
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("cross-matched edge %s in %s", bad, body)
		}
	}
}
