package db

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTxnCleanupContextIsBounded(t *testing.T) {
	ctx, cancel := txnCleanupContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("transaction cleanup context must have a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > txnCleanupTimeout {
		t.Fatalf("unexpected cleanup deadline: %s", remaining)
	}
	if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("unexpected cleanup context error: %v", ctx.Err())
	}
}

func TestProcessEnvironmentUsesConnectionFieldsWithoutEmbeddingSecretsInArgs(t *testing.T) {
	cfg := &pgx.ConnConfig{Config: pgconn.Config{Host: "127.0.0.1", Port: 5544, User: "backup-user", Password: "secret-value", Database: "app db", TLSConfig: &tls.Config{}, RuntimeParams: map[string]string{"application_name": "pglight:test", "options": "-c search_path=public"}}}
	opts := ConnOptions{SSLRootCert: "/tmp/root.crt", SSLCert: "/tmp/client.crt", SSLKey: "/tmp/client.key"}
	env := processEnvironment(cfg, opts, "verify-full")
	for key, want := range map[string]string{
		"PGHOST": "127.0.0.1", "PGPORT": "5544", "PGUSER": "backup-user", "PGPASSWORD": "secret-value",
		"PGDATABASE": "app db", "PGSSLMODE": "verify-full", "PGSSLROOTCERT": "/tmp/root.crt",
		"PGSSLCERT": "/tmp/client.crt", "PGSSLKEY": "/tmp/client.key", "PGAPPNAME": "pglight:test",
		"PGOPTIONS": "-c search_path=public",
	} {
		if env[key] != want {
			t.Errorf("%s=%q, want %q", key, env[key], want)
		}
	}
	for _, arg := range []string{"pg_dump", "--format=custom", "--no-password"} {
		if arg == cfg.Password {
			t.Fatal("password leaked into process arguments")
		}
	}
}
