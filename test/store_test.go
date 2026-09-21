package test

import (
	"context"
	"path/filepath"
	"testing"

	"pglight/internal/store"
)

func TestStoreOpenSchemaIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pglight.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var version int
	if err := s.DB().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if version != 4 {
		s.Close()
		t.Fatalf("schema version = %d, want 4", version)
	}
	for _, table := range []string{"app_users", "snippets", "query_history", "aliases", "connection_profiles", "vaults", "connection_secrets", "user_preferences", "erd_layouts"} {
		var got string
		if err := s.DB().QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&got); err != nil {
			s.Close()
			t.Fatalf("table %s: %v", table, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// reopen is idempotent
	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	// edge: empty path defaults (must not crash); use temp cwd-safe check via error only
	// (do not actually create data/ in repo — just verify Open("") picks default path logic
	// by failing closedir? no: skip filesystem side effects, assert migrate is idempotent instead)
	var v2 int
	if err := s2.DB().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v2); err != nil || v2 != 4 {
		t.Fatalf("reopen version=%d err=%v", v2, err)
	}
}

func TestStoreDomainsRoundTrip(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.EnsureUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	// EnsureUser is idempotent (edge)
	if err := s.EnsureUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertAlias(ctx, "u1", "foo", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertSnippet(ctx, "u1", "test", "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddHistory(ctx, "u1", "SELECT 3", 7, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertConnection(ctx, "u1", "local", "localhost", 5432, "postgres", "postgres", "disable"); err != nil {
		t.Fatal(err)
	}
	aliases, _ := s.ListAliases(ctx, "u1")
	if len(aliases) != 1 || aliases[0].Trigger != "foo" {
		t.Fatalf("aliases=%+v", aliases)
	}
	snippets, _ := s.ListSnippets(ctx, "u1")
	if len(snippets) != 1 || snippets[0].SQL != "SELECT 2" {
		t.Fatalf("snippets=%+v", snippets)
	}
	history, _ := s.ListHistory(ctx, "u1", 10)
	if len(history) != 1 || history[0].RowsCount != 2 {
		t.Fatalf("history=%+v", history)
	}
	connections, _ := s.ListConnections(ctx, "u1")
	if len(connections) != 1 || connections[0].Name != "local" {
		t.Fatalf("connections=%+v", connections)
	}
	if err := s.UpsertErdLayout(ctx, "u1", connections[0].ID, "public", `{"a":{"x":1}}`, `{"x":1,"y":2,"zoom":1}`); err != nil {
		t.Fatal(err)
	}
	layout, err := s.GetErdLayout(ctx, "u1", connections[0].ID, "public")
	if err != nil || layout.LayoutJSON == "" {
		t.Fatalf("erd layout=%+v err=%v", layout, err)
	}
	// edge: preferences round-trip + missing layout delete is safe
	if err := s.SetPreferences(ctx, "u1", `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	raw, err := s.GetPreferences(ctx, "u1")
	if err != nil || raw == "" {
		t.Fatalf("prefs=%q err=%v", raw, err)
	}
	if err := s.DeleteErdLayout(ctx, "u1", "nope", "public"); err != nil {
		t.Fatalf("delete missing layout: %v", err)
	}
}

func TestStoreFKAndIsolation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if _, err := s.DB().ExecContext(ctx, `INSERT INTO app_users(id) VALUES ('u1'), ('u2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO snippets(id,user_id,name,sql) VALUES ('s1','u1','one','select 1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO snippets(id,user_id,name,sql) VALUES ('s2','u1','one','select 2')`); err == nil {
		t.Fatal("expected per-user unique name constraint")
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO snippets(id,user_id,name,sql) VALUES ('s2','u2','one','select 2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM app_users WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets WHERE user_id='u1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cascade deleted snippets = %d, want 0", count)
	}
	// edge: u2 rows survive the cascade
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets WHERE user_id='u2'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("u2 isolation broken: count=%d err=%v", count, err)
	}
}
