package db

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnString must survive userinfo/path characters that QueryEscape mangled
// (spaces became `+`) and must bracket IPv6 hosts. Every output must parse.
func TestConnStringEscaping(t *testing.T) {
	cases := []struct {
		name               string
		host               string
		user, pass, dbname string
	}{
		{"spaces", "localhost", "pg user", "p@ss word", "my db"},
		{"specials", "localhost", "u@x", "p?x&y=z", "a/b"},
		{"ipv6", "::1", "postgres", "postgres", "postgres"},
	}
	for _, c := range cases {
		cs := ConnString(c.host, 5432, c.user, c.pass, c.dbname, "prefer")
		cfg, err := pgxpool.ParseConfig(cs)
		if err != nil {
			t.Fatalf("%s: unparsable %q: %v", c.name, cs, err)
		}
		if cfg.ConnConfig.User != c.user || cfg.ConnConfig.Password != c.pass || cfg.ConnConfig.Database != c.dbname {
			t.Fatalf("%s: round-trip mismatch: user=%q pass=%q db=%q",
				c.name, cfg.ConnConfig.User, string(cfg.ConnConfig.Password), cfg.ConnConfig.Database)
		}
	}
	cs := ConnString("::1", 5432, "u", "p", "d", "prefer")
	if !strings.Contains(cs, "[::1]") {
		t.Fatalf("ipv6 not bracketed: %q", cs)
	}
}

func TestNormalizeSSLMode(t *testing.T) {
	if got := NormalizeSSLMode(""); got != "prefer" {
		t.Fatalf("empty -> %q", got)
	}
	if got := NormalizeSSLMode("bogus"); got != "prefer" {
		t.Fatalf("bogus -> %q", got)
	}
	for _, m := range []string{"disable", "prefer", "require", "verify-ca", "verify-full"} {
		if got := NormalizeSSLMode(" " + m + " "); got != m {
			t.Fatalf("%q -> %q", m, got)
		}
	}
}

func TestInsecureTLS(t *testing.T) {
	if InsecureTLS("localhost", "disable") || InsecureTLS("127.0.0.1", "prefer") {
		t.Fatal("loopback flagged insecure")
	}
	if !InsecureTLS("db.example.com", "prefer") || !InsecureTLS("10.0.0.5", "disable") {
		t.Fatal("remote unverified not flagged")
	}
	if InsecureTLS("db.example.com", "verify-full") || InsecureTLS("db.example.com", "verify-ca") {
		t.Fatal("verified remote flagged insecure")
	}
}
