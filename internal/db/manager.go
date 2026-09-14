package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/url"
	"sync"
	"time"
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
	mu       sync.RWMutex
	pools    map[string]*pgxpool.Pool
	txConns  map[string]*pgxpool.Conn
	txs      map[string]pgx.Tx
	seen     map[string]time.Time
	metas    map[string]ConnMeta
	txnLocks map[string]*sync.Mutex
}

// ConnMeta is display-only connection info for a session (no password).
type ConnMeta struct {
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	DbName      string    `json:"dbname"`
	SSLMode     string    `json:"sslmode"`
	ConnectedAt time.Time `json:"connected_at"`
}

// SessionInfo is the list entry returned by GET /api/sessions.
type SessionInfo struct {
	ID    string `json:"id"`
	InTxn bool   `json:"in_txn"`
	ConnMeta
}

func New() *Manager {
	return &Manager{
		pools:    make(map[string]*pgxpool.Pool),
		txConns:  make(map[string]*pgxpool.Conn),
		txs:      make(map[string]pgx.Tx),
		seen:     make(map[string]time.Time),
		metas:    make(map[string]ConnMeta),
		txnLocks: make(map[string]*sync.Mutex),
	}
}

// touch marks a session active now. Callers must NOT hold m.mu (it locks).
func (m *Manager) touch(id string) {
	m.mu.Lock()
	m.seen[id] = time.Now()
	m.mu.Unlock()
}

// txnLock serializes lifecycle operations for one session without holding the
// global Manager mutex across PostgreSQL I/O. It prevents Begin/Commit/Rollback/
// Close from racing each other while unrelated sessions remain concurrent.
func (m *Manager) txnLock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.txnLocks[id]; ok {
		return l
	}
	l := &sync.Mutex{}
	m.txnLocks[id] = l
	return l
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
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return err
	}
	// A GUI session rarely needs many simultaneous PostgreSQL backends. Keep
	// the default footprint small while allowing pgxpool to grow under load.
	cfg.MinConns = 0
	cfg.MaxConns = 4
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = 1 * time.Hour
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
	oldPool := m.pools[id]
	oldConn := m.txConns[id]
	oldTx := m.txs[id]
	delete(m.txConns, id)
	delete(m.txs, id)
	m.pools[id] = pool
	m.seen[id] = time.Now()
	m.mu.Unlock()

	if oldTx != nil {
		_ = oldTx.Rollback(context.Background())
	}
	if oldConn != nil {
		oldConn.Release()
	}
	if oldPool != nil {
		oldPool.Close()
	}
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
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	tx := m.txs[id]
	conn := m.txConns[id]
	pool := m.pools[id]
	delete(m.txs, id)
	delete(m.txConns, id)
	delete(m.pools, id)
	delete(m.seen, id)
	delete(m.metas, id)
	m.mu.Unlock()

	if tx != nil {
		_ = tx.Rollback(context.Background())
	}
	if conn != nil {
		conn.Release()
	}
	if pool != nil {
		pool.Close()
	}
}

// SetMeta stores display-only connection info for a session (no password).
// ConnectedAt is stamped on first set and preserved across re-Add calls.
func (m *Manager) SetMeta(id string, meta ConnMeta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.metas[id]; ok && !prev.ConnectedAt.IsZero() {
		meta.ConnectedAt = prev.ConnectedAt
	}
	if meta.ConnectedAt.IsZero() {
		meta.ConnectedAt = time.Now()
	}
	m.metas[id] = meta
}

// Info returns the stored meta for a session.
func (m *Manager) Info(id string) (ConnMeta, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.metas[id]
	return meta, ok
}

// List returns one entry per live pool (display info + txn flag).
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(m.pools))
	for id, meta := range m.metas {
		if _, ok := m.pools[id]; !ok {
			continue
		}
		_, inTxn := m.txs[id]
		out = append(out, SessionInfo{ID: id, InTxn: inTxn, ConnMeta: meta})
	}
	// Pools without meta (e.g. created before the upgrade) still show up.
	for id := range m.pools {
		if _, ok := m.metas[id]; ok {
			continue
		}
		_, inTxn := m.txs[id]
		out = append(out, SessionInfo{ID: id, InTxn: inTxn})
	}
	return out
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
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	pool, ok := m.pools[id]
	_, inTxn := m.txs[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("not connected")
	}
	if inTxn {
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

	m.mu.Lock()
	// The per-session lifecycle lock prevents another Begin/Close from
	// changing this state while the PostgreSQL operation was in flight.
	m.txConns[id] = conn
	m.txs[id] = tx
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return nil
}

// Commit commits the session transaction and releases the pinned connection.
func (m *Manager) Commit(ctx context.Context, id string) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	tx, ok := m.txs[id]
	conn := m.txConns[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no open transaction")
	}

	err := tx.Commit(ctx)
	if conn != nil {
		conn.Release()
	}
	m.mu.Lock()
	delete(m.txs, id)
	delete(m.txConns, id)
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return err
}

// Rollback aborts the session transaction and releases the pinned connection.
func (m *Manager) Rollback(ctx context.Context, id string) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	tx, ok := m.txs[id]
	conn := m.txConns[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no open transaction")
	}

	err := tx.Rollback(ctx)
	if conn != nil {
		conn.Release()
	}
	m.mu.Lock()
	delete(m.txs, id)
	delete(m.txConns, id)
	m.seen[id] = time.Now()
	m.mu.Unlock()
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
