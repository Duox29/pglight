package test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pglight/internal/db"
)

// Ports of internal/db/*_test.go (unit test is standard: same expects).

func TestDBConnStringEscaping(t *testing.T) {
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
		cs := db.ConnString(c.host, 5432, c.user, c.pass, c.dbname, "prefer")
		cfg, err := pgxpool.ParseConfig(cs)
		if err != nil {
			t.Fatalf("%s: unparsable %q: %v", c.name, cs, err)
		}
		if cfg.ConnConfig.User != c.user || string(cfg.ConnConfig.Password) != c.pass || cfg.ConnConfig.Database != c.dbname {
			t.Fatalf("%s: round-trip mismatch: user=%q db=%q", c.name, cfg.ConnConfig.User, cfg.ConnConfig.Database)
		}
	}
	cs := db.ConnString("::1", 5432, "u", "p", "d", "prefer")
	if !strings.Contains(cs, "[::1]") {
		t.Fatalf("ipv6 not bracketed: %q", cs)
	}
	// edge: empty sslmode normalizes; weird port format never panics
	if got := db.NormalizeSSLMode(""); got != "prefer" {
		t.Fatalf("empty -> %q", got)
	}
}

func TestDBNormalizeSSLMode(t *testing.T) {
	if got := db.NormalizeSSLMode(""); got != "prefer" {
		t.Fatalf("empty -> %q", got)
	}
	if got := db.NormalizeSSLMode("bogus"); got != "prefer" {
		t.Fatalf("bogus -> %q", got)
	}
	for _, m := range []string{"disable", "prefer", "require", "verify-ca", "verify-full"} {
		if got := db.NormalizeSSLMode(" " + m + " "); got != m {
			t.Fatalf("%q -> %q", m, got)
		}
	}
	// edge: case-insensitive
	if got := db.NormalizeSSLMode("DISABLE"); got != "disable" {
		t.Fatalf("DISABLE -> %q", got)
	}
}

func TestDBInsecureTLS(t *testing.T) {
	if db.InsecureTLS("localhost", "disable") || db.InsecureTLS("127.0.0.1", "prefer") {
		t.Fatal("loopback flagged insecure")
	}
	if !db.InsecureTLS("db.example.com", "prefer") || !db.InsecureTLS("10.0.0.5", "disable") {
		t.Fatal("remote unverified not flagged")
	}
	if db.InsecureTLS("db.example.com", "verify-full") || db.InsecureTLS("db.example.com", "verify-ca") {
		t.Fatal("verified remote flagged insecure")
	}
	// edge: empty host is not remote
	if db.InsecureTLS("", "prefer") {
		t.Fatal("empty host flagged insecure")
	}
}

// --- Manager lifecycle (needs PG) ---

func requireMgr(t *testing.T) *db.Manager {
	t.Helper()
	m := db.New()
	if err := m.Add("probe", testConnStr()); err != nil {
		t.Skipf("test postgres unreachable: %v", err)
	}
	m.Close("probe")
	return m
}

func TestDBSweepIdle(t *testing.T) {
	m := requireMgr(t)
	if err := m.Add("idle", testConnStr()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	swept := m.SweepIdle(50 * time.Millisecond)
	if len(swept) != 1 || swept[0] != "idle" {
		t.Fatalf("expected [idle] swept, got %v", swept)
	}
	if _, ok := m.Q("idle"); ok {
		t.Fatal("swept session still queryable")
	}
	// edge: sweeping empty manager is a no-op
	if swept := m.SweepIdle(time.Hour); len(swept) != 0 {
		t.Fatalf("unexpected sweep: %v", swept)
	}
}

func TestDBSweepSparesTxnAndActive(t *testing.T) {
	m := requireMgr(t)
	if err := m.Add("txn", testConnStr()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Begin(ctx, "txn"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if swept := m.SweepIdle(50 * time.Millisecond); len(swept) != 0 {
		t.Fatalf("open-txn session swept: %v", swept)
	}
	if !m.InTxn("txn") {
		t.Fatal("open txn lost after sweep")
	}
	if err := m.Rollback(ctx, "txn"); err != nil {
		t.Fatal(err)
	}
	m.Close("txn")

	if err := m.Add("busy", testConnStr()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, ok := m.Q("busy"); !ok {
			t.Fatal("active session lost mid-test")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if swept := m.SweepIdle(50 * time.Millisecond); len(swept) != 0 {
		t.Fatalf("active session swept: %v", swept)
	}
	m.Close("busy")
}

func TestDBAlive(t *testing.T) {
	m := requireMgr(t)
	if m.Alive("ghost") {
		t.Fatal("unknown session reported alive")
	}
	if err := m.Add("s", testConnStr()); err != nil {
		t.Fatal(err)
	}
	if !m.Alive("s") {
		t.Fatal("fresh session not alive")
	}
	m.Close("s")
	if m.Alive("s") {
		t.Fatal("closed session reported alive")
	}
	// edge: double close is safe
	m.Close("s")
}

func TestDBTxnConcurrency(t *testing.T) {
	m := requireMgr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Add("conc-q", testConnStr()); err != nil {
		t.Fatal(err)
	}
	defer m.Close("conc-q")
	if err := m.Begin(ctx, "conc-q"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			qq, ok := m.Q("conc-q")
			if !ok {
				t.Error("session lost mid-test")
				return
			}
			rows, err := qq.Query(ctx, `SELECT $1::int`, 1)
			if err != nil {
				t.Error(err)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var v int
				if err := rows.Scan(&v); err != nil || v != 1 {
					t.Errorf("scan %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deadlock? concurrent txn queries not done")
	}
	_ = m.Rollback(ctx, "conc-q")
}

func TestDBBeginVsBegin(t *testing.T) {
	m := requireMgr(t)
	if err := m.Add("2begin", testConnStr()); err != nil {
		t.Fatal(err)
	}
	defer m.Close("2begin")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			errs[i] = m.Begin(ctx, "2begin")
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock? begin vs begin")
	}
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one Begin to win, errs=%v", errs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.Rollback(ctx, "2begin")
}

func TestDBSweepAbandoned(t *testing.T) {
	m := requireMgr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Add("abandoned", testConnStr()); err != nil {
		t.Fatal(err)
	}
	defer m.Close("abandoned")
	if err := m.Begin(ctx, "abandoned"); err != nil {
		t.Fatal(err)
	}
	// Backdate lastUse via Snapshot-independent path is unexported; instead
	// wait out a tiny TTL by sleeping past it.
	time.Sleep(60 * time.Millisecond)
	if rolled := m.SweepAbandonedTxns(50 * time.Millisecond); len(rolled) != 1 {
		t.Fatalf("expected [abandoned] rolled back, got %v", rolled)
	}
	if m.InTxn("abandoned") {
		t.Fatal("abandoned txn still open after sweep")
	}
}

func TestDBSnapshotAtomic(t *testing.T) {
	m := requireMgr(t)
	if err := m.Add("snap", testConnStr()); err != nil {
		t.Fatal(err)
	}
	defer m.Close("snap")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if i%2 == 0 {
				_ = m.Begin(ctx, "snap")
			} else {
				_, hasPool, _ := m.Snapshot("snap")
				if !hasPool {
					t.Error("pool lost mid-test")
				}
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock? snapshot atomicity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.Rollback(ctx, "snap")
}
