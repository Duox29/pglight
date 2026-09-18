package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

// waitTimeout fails the test if wg isn't done in time (deadlock detector).
func waitTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("deadlock? %s not done in %s", what, d)
	}
}

func beginTxn(t *testing.T, m *Manager, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Add(id, testConnStr()); err != nil {
		t.Fatal(err)
	}
	if err := m.Begin(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func queryOne(ctx context.Context, t *testing.T, m *Manager, id string) {
	t.Helper()
	qq, ok := m.Q(id)
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
		if err := rows.Scan(&v); err != nil {
			t.Error(err)
			return
		}
		if v != 1 {
			t.Errorf("got %d, want 1", v)
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Error(err)
	}
}

// Concurrent queries inside one explicit txn must serialize, not race on
// the pgx connection. Run with -race.
func TestTxnConcurrentQueries(t *testing.T) {
	m := requirePG(t)
	beginTxn(t, m, "conc-q")
	defer m.Close("conc-q")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			queryOne(ctx, t, m, "conc-q")
		}()
	}
	waitTimeout(t, &wg, 30*time.Second, "concurrent txn queries")
}

// Commit landing while a query streams must wait for the query, not corrupt
// the connection.
func TestTxnQueryVsCommit(t *testing.T) {
	m := requirePG(t)
	beginTxn(t, m, "q-commit")
	defer m.Close("q-commit")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		qq, ok := m.Q("q-commit")
		if !ok {
			t.Error("session lost")
			return
		}
		rows, err := qq.Query(ctx, `SELECT pg_sleep(0.5)`)
		if err != nil {
			t.Error(err)
			return
		}
		rows.Close()
	}()
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = m.Commit(ctx, "q-commit") // may or may not win; must not race
	}()
	waitTimeout(t, &wg, 30*time.Second, "query vs commit")
	if m.InTxn("q-commit") {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Rollback(ctx, "q-commit")
	}
}

// Two concurrent Begins: exactly one wins.
func TestTxnBeginVsBegin(t *testing.T) {
	m := requirePG(t)
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
	waitTimeout(t, &wg, 20*time.Second, "begin vs begin")
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one Begin to win, errs=%v", errs)
	}
}

// Disconnect racing active queries must not deadlock or panic.
func TestTxnDisconnectVsQuery(t *testing.T) {
	m := requirePG(t)
	beginTxn(t, m, "disc-q")

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			qq, ok := m.Q("disc-q")
			if !ok {
				return
			}
			rows, err := qq.Query(ctx, `SELECT 1`)
			if err != nil {
				return
			}
			rows.Close()
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		m.Close("disc-q")
	}()
	waitTimeout(t, &wg, 30*time.Second, "disconnect vs query")
}

// Transactions idle past the TTL are rolled back so abandoned tabs can't
// hold locks/vacuum gates forever.
func TestSweepAbandonedTxns(t *testing.T) {
	m := requirePG(t)
	beginTxn(t, m, "abandoned")
	defer m.Close("abandoned")

	m.mu.Lock()
	if e, ok := m.txs["abandoned"]; ok {
		e.mu.Lock()
		e.lastUse = time.Now().Add(-time.Hour)
		e.mu.Unlock()
	}
	m.mu.Unlock()

	if rolled := m.SweepAbandonedTxns(50 * time.Millisecond); len(rolled) != 1 {
		t.Fatalf("expected [abandoned] rolled back, got %v", rolled)
	}
	if m.InTxn("abandoned") {
		t.Fatal("abandoned txn still open after sweep")
	}
}

// Snapshot must report pool+txn state atomically under concurrency.
func TestSnapshotAtomic(t *testing.T) {
	m := requirePG(t)
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
	waitTimeout(t, &wg, 20*time.Second, "snapshot atomicity")
}
