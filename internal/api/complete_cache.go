package api

// Per-session schema snapshot cache for query autocomplete.
//
// The query editor needs schema data (tables + their columns, functions, FKs)
// on every keystroke. Hitting Postgres per keystroke would spam the server,
// so the flow is:
//
//	CodeMirror keystroke -> frontend memory (zero network)
//	-> GET /api/complete (served from this cache, zero PG queries while fresh)
//	-> single PG fetch when the entry is stale/missing (single-flighted)
//
// Invalidation: TTL expiry (completeTTL), explicit ?refresh=1, DDL executed
// via /api/query (see ddlRe in complete.go), session disconnect/sweep.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pglight/internal/db"
)

// completeTTL bounds how stale a snapshot can get without a refresh.
// 60s keeps DDL-from-another-tool visible quickly while collapsing hundreds
// of keystrokes into (usually) zero PG queries.
const completeTTL = 60 * time.Second

// CompleteColumn is one column of a table, with display type.
type CompleteColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// CompleteTable is a table/view with its columns nested (scope-aware
// completion needs columns attached to their table, not a flat list).
type CompleteTable struct {
	Schema  string           `json:"schema"`
	Name    string           `json:"name"`
	Columns []CompleteColumn `json:"columns"`
}

// CompleteFunc is a user function with its argument signature.
type CompleteFunc struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Args   string `json:"args"`
}

// CompleteFK is a foreign-key edge, used to boost JOIN suggestions.
type CompleteFK struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

// CompleteSnapshot is the cached payload served to the editor.
type CompleteSnapshot struct {
	Version int64           `json:"version"`
	Tables  []CompleteTable `json:"tables"`
	Funcs   []CompleteFunc  `json:"funcs"`
	FKs     []CompleteFK    `json:"fks"`
	Fetched time.Time       `json:"-"`
}

// completeCache is a per-session snapshot store with TTL + single-flight:
// concurrent Get calls for the same session share one PG fetch.
type completeCache struct {
	mu       sync.Mutex
	snaps    map[string]*CompleteSnapshot
	inflight map[string]chan struct{}
}

func newCompleteCache() *completeCache {
	return &completeCache{snaps: map[string]*CompleteSnapshot{}, inflight: map[string]chan struct{}{}}
}

// globalComplete is process-wide (pglight is a single-process tool;
// sessions are already isolated by key).
var globalComplete = newCompleteCache()

// Get returns the cached snapshot, fetching from PG once when missing/stale
// or refresh is set. ok==false means another caller is fetching; the caller
// should wait on Wait then retry (single-flight).
func (c *completeCache) Get(ctx context.Context, qq db.Querier, sid string, refresh bool) (*CompleteSnapshot, error) {
	c.mu.Lock()
	if s, ok := c.snaps[sid]; ok && !refresh && time.Since(s.Fetched) < completeTTL {
		c.mu.Unlock()
		return s, nil
	}
	if ch, dup := c.inflight[sid]; dup {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		s := c.snaps[sid]
		c.mu.Unlock()
		if s == nil {
			return nil, fmt.Errorf("schema snapshot unavailable")
		}
		return s, nil
	}
	ch := make(chan struct{})
	c.inflight[sid] = ch
	c.mu.Unlock()

	snap, err := fetchCompleteSnapshot(ctx, qq)
	c.mu.Lock()
	if err == nil {
		c.snaps[sid] = snap
	}
	delete(c.inflight, sid)
	close(ch)
	c.mu.Unlock()
	return snap, err
}

// Invalidate drops the snapshot so the next Get refetches (DDL path).
func (c *completeCache) Invalidate(sid string) {
	c.mu.Lock()
	delete(c.snaps, sid)
	c.mu.Unlock()
}

// fetchCompleteSnapshot loads tables+columns, functions and FK edges.
// Three small capped catalog queries; called at most once per TTL per
// session however many keystrokes the user types.
func fetchCompleteSnapshot(ctx context.Context, qq db.Querier) (*CompleteSnapshot, error) {
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, trows, err := queryJSON(qq, fctx, `
		SELECT c.table_schema, c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t USING (table_schema, table_name)
		WHERE c.table_schema NOT IN ('pg_catalog','information_schema')
		  AND c.table_schema NOT LIKE 'pg\_%'
		ORDER BY c.table_schema, c.table_name, c.ordinal_position
		LIMIT 5000`)
	if err != nil {
		return nil, err
	}
	_, frows, err := queryJSON(qq, fctx, `
		SELECT n.nspname, p.proname, pg_get_function_arguments(p.oid)
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname NOT IN ('pg_catalog','information_schema')
		  AND n.nspname NOT LIKE 'pg\_%'
		ORDER BY 1, 2 LIMIT 500`)
	if err != nil {
		return nil, err
	}
	_, erows, err := queryJSON(qq, fctx, `
		SELECT src_ns.nspname||'.'||src.relname||'.'||src_col.attname,
		       dst_ns.nspname||'.'||dst.relname||'.'||dst_col.attname
		FROM pg_constraint con
		JOIN pg_class src ON src.oid = con.conrelid
		JOIN pg_namespace src_ns ON src_ns.oid = src.relnamespace
		JOIN pg_class dst ON dst.oid = con.confrelid
		JOIN pg_namespace dst_ns ON dst_ns.oid = dst.relnamespace
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS sk(attnum, ord) ON true
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS tk(attnum, ord) ON tk.ord = sk.ord
		JOIN pg_attribute src_col ON src_col.attrelid = src.oid AND src_col.attnum = sk.attnum
		JOIN pg_attribute dst_col ON dst_col.attrelid = dst.oid AND dst_col.attnum = tk.attnum
		WHERE con.contype = 'f'
		  AND src_ns.nspname NOT IN ('pg_catalog','information_schema')
		  AND src_ns.nspname NOT LIKE 'pg\_%'
		ORDER BY src_ns.nspname, src.relname, con.oid, sk.ord
		LIMIT 500`)
	if err != nil {
		return nil, err
	}

	tables := []CompleteTable{}
	idx := map[string]int{}
	for _, r := range trows {
		if len(r) < 4 {
			continue
		}
		sch, tbl, col, typ := fmt.Sprint(r[0]), fmt.Sprint(r[1]), fmt.Sprint(r[2]), fmt.Sprint(r[3])
		key := sch + "." + tbl
		i, ok := idx[key]
		if !ok {
			i = len(tables)
			idx[key] = i
			tables = append(tables, CompleteTable{Schema: sch, Name: tbl})
		}
		tables[i].Columns = append(tables[i].Columns, CompleteColumn{Name: col, Type: typ})
	}
	funcs := []CompleteFunc{}
	for _, r := range frows {
		if len(r) < 3 {
			continue
		}
		funcs = append(funcs, CompleteFunc{Schema: fmt.Sprint(r[0]), Name: fmt.Sprint(r[1]), Args: fmt.Sprint(r[2])})
	}
	fks := []CompleteFK{}
	for _, r := range erows {
		if len(r) < 2 {
			continue
		}
		fks = append(fks, CompleteFK{Src: fmt.Sprint(r[0]), Dst: fmt.Sprint(r[1])})
	}
	if tables == nil {
		tables = []CompleteTable{}
	}
	if funcs == nil {
		funcs = []CompleteFunc{}
	}
	if fks == nil {
		fks = []CompleteFK{}
	}
	now := time.Now()
	return &CompleteSnapshot{Version: now.UnixNano(), Tables: tables, Funcs: funcs, FKs: fks, Fetched: now}, nil
}
