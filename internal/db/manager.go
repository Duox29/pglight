package db

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is satisfied by *pgxpool.Pool and pgx.Tx (and *pgxpool.Conn).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Manager holds one pgx pool per session plus optional explicit transaction state.
// Explicit txns (DataGrip/pgAdmin-style) pin a single pooled connection so that
// subsequent queries in the same session see uncommitted data.
type Manager struct {
	mu      sync.RWMutex
	pools   map[string]*pgxpool.Pool
	txConns map[string]*pgxpool.Conn
	txs     map[string]pgx.Tx
	seen    map[string]time.Time
}

func New() *Manager {
	return &Manager{
		pools:   make(map[string]*pgxpool.Pool),
		txConns: make(map[string]*pgxpool.Conn),
		txs:     make(map[string]pgx.Tx),
		seen:    make(map[string]time.Time),
	}
}

// touch marks a session active now. Callers must NOT hold m.mu (it locks).
func (m *Manager) touch(id string) {
	m.mu.Lock()
	m.seen[id] = time.Now()
	m.mu.Unlock()
}

func ConnString(host string, port int, user, password, dbname, sslmode string) string {
	if port == 0 {
		port = 5432
	}
	if sslmode == "" {
		sslmode = "disable"
	}
	if dbname == "" {
		dbname = "postgres"
	}
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		url.QueryEscape(user), url.QueryEscape(password), host, port, url.QueryEscape(dbname), url.QueryEscape(sslmode))
}

func (m *Manager) Add(id, connStr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return err
	}
	cfg.MaxConns = 8
	// Tag every backend of this pool so /api/activity (and the console
	// Cancel button) can attribute running queries to this session.
	app := "pglight:" + id
	if len(app) > 60 {
		app = app[len(app)-60:]
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return err
	}
	m.mu.Lock()
	if old, ok := m.pools[id]; ok {
		old.Close()
	}
	if c, ok := m.txConns[id]; ok {
		c.Release()
		delete(m.txConns, id)
		delete(m.txs, id)
	}
	m.pools[id] = pool
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return nil
}

// Alive reports whether the session has a healthy pool. A dead pool is closed
// and removed so the caller can transparently reconnect under the same id.
func (m *Manager) Alive(id string) bool {
	m.mu.RLock()
	p, ok := m.pools[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Ping(ctx); err != nil {
		m.Close(id)
		return false
	}
	m.touch(id)
	return true
}

func (m *Manager) Get(id string) (*pgxpool.Pool, bool) {
	m.mu.RLock()
	p, ok := m.pools[id]
	m.mu.RUnlock()
	if ok {
		m.touch(id)
	}
	return p, ok
}

func (m *Manager) Close(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tx, ok := m.txs[id]; ok {
		_ = tx.Rollback(context.Background())
		delete(m.txs, id)
	}
	if c, ok := m.txConns[id]; ok {
		c.Release()
		delete(m.txConns, id)
	}
	if p, ok := m.pools[id]; ok {
		p.Close()
		delete(m.pools, id)
	}
	delete(m.seen, id)
}

// InTxn reports whether the session has an open explicit transaction.
func (m *Manager) InTxn(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.txs[id]
	return ok
}

// Begin starts an explicit transaction for the session.
func (m *Manager) Begin(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.pools[id]
	if !ok {
		return fmt.Errorf("not connected")
	}
	if _, ok := m.txs[id]; ok {
		return fmt.Errorf("transaction already open")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return err
	}
	m.txConns[id] = conn
	m.txs[id] = tx
	m.seen[id] = time.Now()
	return nil
}

// Commit commits the session transaction and releases the pinned connection.
func (m *Manager) Commit(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.txs[id]
	if !ok {
		return fmt.Errorf("no open transaction")
	}
	conn := m.txConns[id]
	err := tx.Commit(ctx)
	if conn != nil {
		conn.Release()
	}
	delete(m.txs, id)
	delete(m.txConns, id)
	m.seen[id] = time.Now()
	return err
}

// Rollback aborts the session transaction and releases the pinned connection.
func (m *Manager) Rollback(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.txs[id]
	if !ok {
		return fmt.Errorf("no open transaction")
	}
	conn := m.txConns[id]
	err := tx.Rollback(ctx)
	if conn != nil {
		conn.Release()
	}
	delete(m.txs, id)
	delete(m.txConns, id)
	m.seen[id] = time.Now()
	return err
}

// Q returns the query target for a session: the open txn when present,
// otherwise the pool. Caller must NOT close/release the result.
func (m *Manager) Q(id string) (Querier, bool) {
	m.mu.RLock()
	tx, hasTx := m.txs[id]
	p, hasPool := m.pools[id]
	m.mu.RUnlock()
	if hasTx {
		m.touch(id)
		return tx, true
	}
	if !hasPool {
		return nil, false
	}
	m.touch(id)
	return p, true
}

// PoolOrTxn is a convenience for handlers that need *pgxpool.Pool for
// helpers but must stay txn-aware. It returns the txn when open.
func (m *Manager) PoolOrTxn(id string) (Querier, *pgxpool.Pool, bool) {
	m.mu.RLock()
	tx, hasTx := m.txs[id]
	p, hasPool := m.pools[id]
	m.mu.RUnlock()
	if hasTx {
		m.touch(id)
		return tx, nil, true
	}
	if hasPool {
		m.touch(id)
	}
	return p, p, hasPool
}

// Sweep tuning: idle sessions (no query/txn/connect activity) are closed to
// release their postgres connections. Sessions with an open explicit
// transaction are spared — rolling those back silently could lose user work.
const (
	DefaultSweepInterval = 5 * time.Minute
	DefaultIdleTTL       = 30 * time.Minute
)

// SweepIdle closes sessions idle longer than ttl (except open-txn ones) and
// returns the swept ids. Synchronous; call it from a ticker goroutine.
func (m *Manager) SweepIdle(ttl time.Duration) []string {
	cutoff := time.Now().Add(-ttl)
	m.mu.RLock()
	var stale []string
	for id, at := range m.seen {
		if at.Before(cutoff) {
			if _, open := m.txs[id]; !open {
				stale = append(stale, id)
			}
		}
	}
	m.mu.RUnlock()
	for _, id := range stale {
		m.Close(id)
	}
	return stale
}

// StartSweeper reaps idle sessions every interval until the process exits.
func (m *Manager) StartSweeper(interval, ttl time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			for _, id := range m.SweepIdle(ttl) {
				log.Printf("pglight: swept idle session %s", id)
			}
		}
	}()
}
