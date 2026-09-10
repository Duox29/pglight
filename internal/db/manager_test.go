package db

import (
	"context"
	"testing"
	"time"
)

func testConnStr() string {
	return ConnString("localhost", 5432, "postgres", "postgres", "postgres", "disable")
}

// requirePG returns a Manager, skipping when the docker test DB is down.
// requirePG returns a Manager, skipping when the docker test DB is down.
func requirePG(t *testing.T) *Manager {
	t.Helper()
	m := New()
	if err := m.Add("probe", testConnStr()); err != nil {
		t.Skipf("test postgres unreachable: %v", err)
	}
	m.Close("probe")
	return m
}

func TestSweepIdleClosesStale(t *testing.T) {
	m := requirePG(t)
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
}

func TestSweepIdleSparesOpenTxn(t *testing.T) {
	m := requirePG(t)
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
	if _, ok := m.Q("txn"); !ok {
		t.Fatal("open-txn session not queryable after sweep")
	}
	if !m.InTxn("txn") {
		t.Fatal("open txn lost after sweep")
	}
	if err := m.Rollback(ctx, "txn"); err != nil {
		t.Fatal(err)
	}
}

func TestSweepIdleKeepsActive(t *testing.T) {
	m := requirePG(t)
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

func TestAlive(t *testing.T) {
	m := requirePG(t)
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
}
