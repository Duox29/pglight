package test

// Transaction-lease regressions (P0): the operation lease must hold the
// explicit transaction across multi-statement work so Commit blocks instead
// of partially committing; the sweeper must not deadlock against Commit;
// InTxn/Snapshot must be race-clean under concurrent Commit (run with
// -race: any e.done data race fails the run).

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLeaseBlocksCommit(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)

	q, release, inTxn, _, ok := h.Mgr.AcquireLease(sid)
	if !ok || !inTxn {
		t.Fatalf("want in-txn lease, ok=%v inTxn=%v", ok, inTxn)
	}
	if q == nil {
		release()
		t.Fatal("nil lease querier")
	}
	// Commit racing a held lease must wait (not fail, not interleave):
	// with per-statement locking it would slip between batches and
	// partially commit; with the lease it blocks until release.
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done <- h.Mgr.Commit(ctx, sid)
	}()
	select {
	case err := <-done:
		release()
		t.Fatalf("Commit returned while lease held (no mutual exclusion): %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Commit after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Commit blocked even after release")
	}
	if h.Mgr.InTxn(sid) {
		t.Fatal("still in txn after commit")
	}
}

func TestSweeperConcurrentWithCommit(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	// Hammer the sweeper (which used to take m.mu.RLock then e.mu — the
	// opposite order of Commit's e.mu then m.mu.Lock) while committing.
	// A lock-order inversion deadlocks here; completion proves the fix.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Mgr.SweepAbandonedTxns(time.Nanosecond)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = h.Mgr.Commit(ctx, sid)
	}()
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock: sweeper + Commit did not finish")
	}
}

func TestInTxnSnapshotRaceClean(t *testing.T) {
	h, sid := newHandler(t)
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = h.Mgr.InTxn(sid)
				_, _, _ = h.Mgr.Snapshot(sid)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = h.Mgr.Commit(ctx, sid)
	}()
	wg.Wait()
	// Post-commit reads must observe the closed state (atomic done flag).
	if h.Mgr.InTxn(sid) {
		t.Fatal("InTxn true after commit")
	}
}

func TestMockGenerateMultiBatchInTxnAtomic(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (id SERIAL PRIMARY KEY, v TEXT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"begin"`))
	requireStatus(t, body, code, 200)
	// 600 rows = 2 INSERT batches: both must land in the explicit txn
	// (invisible to a second session) and survive only as a unit.
	code, body = callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"simple","count":600,"seed":11`, tbl)))
	requireStatus(t, body, code, 200)
	if n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]; fmt.Sprint(n) != "600" {
		t.Fatalf("in-txn count=%v want 600 (%s)", n, body)
	}
	code, body = callPOST(t, h.Txn, "/api/txn", postBody(sid, `"action":"rollback"`))
	requireStatus(t, body, code, 200)
	if n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]; fmt.Sprint(n) != "0" {
		t.Fatalf("after rollback count=%v want 0", n)
	}
}

func TestDefaultOnlyBatchInsert(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ DEFAULT now())`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	// 1200 default-only rows exercise the batched DEFAULT VALUES path.
	code, body := callPOST(t, h.MockGenerate, "/api/mock-data/generate",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":1200,"seed":7`, tbl)))
	requireStatus(t, body, code, 200)
	if n := queryRows(t, h, sid, "SELECT count(*) FROM public."+tbl)[0][0]; fmt.Sprint(n) != "1200" {
		t.Fatalf("default-only count=%v want 1200 (%s)", n, body)
	}
}

func TestCompositeUniqueWarns(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (
		a INT NOT NULL, b INT NOT NULL, UNIQUE (a, b))`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	code, body := callGET(t, h.MockMeta, withSID(sid, "/api/mock-data/meta?schema=public&table="+tbl))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	cu, _ := out["composite_uniques"].([]any)
	if len(cu) == 0 {
		t.Fatalf("want composite_uniques in meta (%s)", body)
	}
	code, body = callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":5`, tbl)))
	requireStatus(t, body, code, 200)
	if !containsStr(decodeObj(t, body), "Composite UNIQUE") {
		t.Fatalf("want composite-UNIQUE warning in preview (%s)", body)
	}
}

func TestPreviewNumericLiteralConstraintEndToEnd(t *testing.T) {
	h, sid := newHandler(t)
	tbl := tempTable(t)
	execSQL(t, h, sid, fmt.Sprintf(`CREATE TABLE public.%s (n INT NOT NULL)`, tbl))
	t.Cleanup(func() { execSQL(t, h, sid, "DROP TABLE IF EXISTS public."+tbl) })
	// Literal 10 travels as json.Number (UseNumber); generated values are
	// int64. A lexicographic fallback would compare "2" vs "10" wrong.
	code, body := callPOST(t, h.MockPreview, "/api/mock-data/preview",
		postBody(sid, fmt.Sprintf(`"schema":"public","table":%q,"mode":"advanced","count":20,"seed":4,`+
			`"fields":[{"column":"n","generator":"integer","params":{"min":0,"max":20}}],`+
			`"constraints":[{"kind":"compare","left":{"field":"n"},"operator":"<","right":{"value":10}}]`, tbl)))
	requireStatus(t, body, code, 200)
	out := decodeObj(t, body)
	rows, _ := out["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("no preview rows (%s)", body)
	}
	cols, _ := out["columns"].([]any)
	ni := -1
	for i, c := range cols {
		if c == "n" {
			ni = i
		}
	}
	if ni < 0 {
		t.Fatalf("no n column (%s)", body)
	}
	for i, r := range rows {
		cells, _ := r.([]any)
		var v float64
		switch n := cells[ni].(type) {
		case float64:
			v = n
		default:
			t.Fatalf("row %d n not numeric: %T (%s)", i, cells[ni], body)
		}
		if v >= 10 {
			t.Fatalf("row %d violates n<10: %v (%s)", i, v, body)
		}
	}
}

func containsStr(out map[string]any, frag string) bool {
	raw, _ := out["warnings"].([]any)
	for _, w := range raw {
		if s, _ := w.(string); len(s) > 0 && len(frag) > 0 {
			for i := 0; i+len(frag) <= len(s); i++ {
				if s[i:i+len(frag)] == frag {
					return true
				}
			}
		}
	}
	return false
}
