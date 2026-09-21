package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
//
// Concurrency model: pool queries run concurrently, but once an explicit
// transaction is open, every operation on that session's pgx.Tx is serialized
// through the txEntry mutex (see Q). Commit/Rollback/Close wait for any
// in-flight txn operation to finish instead of racing it.
type Manager struct {
	mu       sync.RWMutex
	pools    map[string]*pgxpool.Pool
	txs      map[string]*txEntry
	seen     map[string]time.Time
	metas    map[string]ConnMeta
	txnLocks map[string]*sync.Mutex
}

// txEntry is one open explicit transaction: the pinned connection, the pgx
// transaction, and a mutex serializing all use of that transaction. done is
// an atomic flag set once the txn is committed/rolled back so late arrivals
// fail cleanly instead of touching a closed transaction (all readers use
// Load — never read it under m.mu — so there is no data race). lastUse backs
// the abandoned-transaction sweeper and is guarded by mu.
type txEntry struct {
	mu      sync.Mutex
	tx      pgx.Tx
	conn    *pgxpool.Conn
	done    atomic.Bool
	lastUse time.Time
}

func (e *txEntry) touch() { e.lastUse = time.Now() }

// isDone reports lifecycle state without taking e.mu (atomic, race-free).
func (e *txEntry) isDone() bool { return e.done.Load() }

// ConnMeta is display-only connection info for a session (no password).
type ConnMeta struct {
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	DbName      string    `json:"dbname"`
	SSLMode     string    `json:"sslmode"`
	ConnectedAt time.Time `json:"connected_at"`
	ProfileID   string    `json:"profile_id,omitempty"`
	ProfileName string    `json:"profile_name,omitempty"`
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
		txs:      make(map[string]*txEntry),
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

// validSSLModes is the full meaningful PostgreSQL set. Unknown values fall
// back to prefer (encrypted when the server offers it, plaintext otherwise).
var validSSLModes = map[string]bool{
	"disable": true, "prefer": true, "require": true,
	"verify-ca": true, "verify-full": true,
}

// NormalizeSSLMode lowercases/trims and allow-lists the sslmode; anything
// unknown (or empty) becomes prefer.
func NormalizeSSLMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if validSSLModes[s] {
		return s
	}
	return "prefer"
}

// IsLoopbackHost reports whether host is a loopback address or name.
func IsLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// InsecureTLS reports whether connecting to host with sslmode skips
// certificate verification — worth a visible warning for non-loopback hosts.
func InsecureTLS(host, sslmode string) bool {
	if IsLoopbackHost(host) {
		return false
	}
	m := NormalizeSSLMode(sslmode)
	return m != "verify-ca" && m != "verify-full"
}

func ConnString(host string, port int, user, password, dbname, sslmode string) string {
	return ConnStringWithOptions(host, port, user, password, dbname, sslmode, ConnOptions{})
}

// ConnOptions contains PostgreSQL libpq/pgx connection settings that are safe
// to persist with a profile. Certificate paths are local filesystem paths;
// their contents never enter the profile database or API response. Keepalive
// is applied to the TCP dialer, not encoded as a PostgreSQL runtime parameter.
type ConnOptions struct {
	ConnectTimeout  int
	Keepalive       int
	ApplicationName string
	SearchPath      string
	SSLRootCert     string
	SSLCert         string
	SSLKey          string
	UnixSocket      string
}

func ConnStringWithOptions(host string, port int, user, password, dbname, sslmode string, opts ConnOptions) string {
	if port == 0 {
		port = 5432
	}
	sslmode = NormalizeSSLMode(sslmode)
	if dbname == "" {
		dbname = "postgres"
	}
	if host == "" {
		host = "localhost"
	}
	// Build the URL from parts instead of Sprintf: UserPassword encodes
	// userinfo correctly (QueryEscape turned spaces into `+`, which is not
	// a space outside query strings), JoinHostPort brackets IPv6, and
	// Path escaping handles special characters in the database name.
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbname,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	if opts.ConnectTimeout > 0 {
		q.Set("connect_timeout", strconv.Itoa(opts.ConnectTimeout))
	}
	if opts.ApplicationName != "" {
		q.Set("application_name", opts.ApplicationName)
	}
	if opts.SearchPath != "" {
		q.Set("options", "-c search_path="+opts.SearchPath)
	}
	if opts.SSLRootCert != "" {
		q.Set("sslrootcert", opts.SSLRootCert)
	}
	if opts.SSLCert != "" {
		q.Set("sslcert", opts.SSLCert)
	}
	if opts.SSLKey != "" {
		q.Set("sslkey", opts.SSLKey)
	}
	if opts.UnixSocket != "" {
		q.Set("host", opts.UnixSocket)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Manager) Add(id, connStr string) error {
	return m.AddWithOptions(id, connStr, ConnOptions{})
}

// AddWithOptions creates a pool and applies socket-level options before the
// first connection is opened. Keepalive must be configured on net.Dialer;
// sending libpq's keepalives keywords as pgx runtime parameters makes
// PostgreSQL treat them as unknown GUCs and reject the startup packet.
func (m *Manager) AddWithOptions(id, connStr string, opts ConnOptions) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return err
	}
	if opts.Keepalive > 0 {
		dialer := &net.Dialer{KeepAlive: time.Duration(opts.Keepalive) * time.Second}
		dialer.Timeout = cfg.ConnConfig.ConnectTimeout
		cfg.ConnConfig.DialFunc = dialer.DialContext
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
	oldEntry := m.txs[id]
	delete(m.txs, id)
	m.pools[id] = pool
	m.seen[id] = time.Now()
	m.mu.Unlock()

	if oldEntry != nil {
		oldEntry.mu.Lock()
		if !oldEntry.done.Load() {
			_ = oldEntry.tx.Rollback(context.Background())
			oldEntry.done.Store(true)
		}
		oldEntry.mu.Unlock()
		oldEntry.conn.Release()
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
	entry := m.txs[id]
	pool := m.pools[id]
	delete(m.txs, id)
	delete(m.pools, id)
	delete(m.seen, id)
	delete(m.metas, id)
	m.mu.Unlock()

	if entry != nil {
		// Wait for any in-flight txn operation before tearing down.
		entry.mu.Lock()
		if !entry.done.Load() {
			_ = entry.tx.Rollback(context.Background())
			entry.done.Store(true)
		}
		entry.mu.Unlock()
		entry.conn.Release()
	}
	if pool != nil {
		pool.Close()
	}
}

// CloseAll closes every session pool, rolling back any open explicit txn.
// Used by /api/shutdown before stopping the process.
func (m *Manager) CloseAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.pools))
	for id := range m.pools {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.Close(id)
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

// InTxn reports whether the session has an open explicit transaction
// (atomic done flag — race-free, no e.mu needed).
func (m *Manager) InTxn(id string) bool {
	m.mu.RLock()
	e, ok := m.txs[id]
	m.mu.RUnlock()
	if !ok || e == nil {
		return false
	}
	return !e.done.Load()
}

// Snapshot returns the pool and txn state for a session atomically, so
// callers never observe Get() and InTxn() from different points in time.
// The txn flag uses the atomic done flag (no e.mu, no data race).
func (m *Manager) Snapshot(id string) (pool *pgxpool.Pool, hasPool, inTxn bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pool, hasPool = m.pools[id]
	if e, ok := m.txs[id]; ok && e != nil && !e.done.Load() {
		inTxn = true
	}
	return pool, hasPool, inTxn
}

// Begin starts an explicit transaction for the session.
func (m *Manager) Begin(ctx context.Context, id string) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	pool, ok := m.pools[id]
	e, inTxn := m.txs[id]
	if inTxn && e != nil && e.done.Load() {
		inTxn = false
	}
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
	m.txs[id] = &txEntry{tx: tx, conn: conn, lastUse: time.Now()}
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return nil
}

// Commit commits the session transaction and releases the pinned connection.
// It waits for any in-flight txn query (or a held operation lease) to finish
// first, so Query+Commit from two tabs cannot race on the underlying
// connection. Lock order is always txnLock → e.mu → m.mu (never m.mu → e.mu
// while holding m.mu), so the sweeper cannot deadlock against it.
func (m *Manager) Commit(ctx context.Context, id string) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	e, ok := m.txs[id]
	m.mu.RUnlock()
	if !ok || e == nil {
		return fmt.Errorf("no open transaction")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done.Load() {
		return fmt.Errorf("no open transaction")
	}
	err := e.tx.Commit(ctx)
	e.done.Store(true)
	e.conn.Release()
	m.mu.Lock()
	delete(m.txs, id)
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return err
}

// Rollback aborts the session transaction and releases the pinned connection.
// Like Commit, it waits for in-flight txn work (or a held lease) before
// rolling back. Same lock order as Commit.
func (m *Manager) Rollback(ctx context.Context, id string) error {
	lock := m.txnLock(id)
	lock.Lock()
	defer lock.Unlock()

	m.mu.RLock()
	e, ok := m.txs[id]
	m.mu.RUnlock()
	if !ok || e == nil {
		return fmt.Errorf("no open transaction")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done.Load() {
		return fmt.Errorf("no open transaction")
	}
	err := e.tx.Rollback(ctx)
	e.done.Store(true)
	e.conn.Release()
	m.mu.Lock()
	delete(m.txs, id)
	m.seen[id] = time.Now()
	m.mu.Unlock()
	return err
}

// serialQuerier serializes every operation on one explicit transaction so
// concurrent HTTP requests from multiple tabs cannot share the underlying
// PostgreSQL connection. Pool queries bypass it and stay concurrent.
type serialQuerier struct {
	e *txEntry
}

func (s serialQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.e.mu.Lock()
	if s.e.done.Load() {
		s.e.mu.Unlock()
		return nil, fmt.Errorf("transaction already closed")
	}
	s.e.touch()
	rows, err := s.e.tx.Query(ctx, sql, args...)
	if err != nil {
		s.e.mu.Unlock()
		return nil, err
	}
	return serialRows{Rows: rows, unlock: sync.OnceFunc(s.e.mu.Unlock)}, nil
}

func (s serialQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.e.mu.Lock()
	defer s.e.mu.Unlock()
	if s.e.done.Load() {
		return pgconn.CommandTag{}, fmt.Errorf("transaction already closed")
	}
	s.e.touch()
	return s.e.tx.Exec(ctx, sql, args...)
}

func (s serialQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	s.e.mu.Lock()
	if s.e.done.Load() {
		s.e.mu.Unlock()
		return closedRow{err: fmt.Errorf("transaction already closed")}
	}
	s.e.touch()
	return serialRow{Row: s.e.tx.QueryRow(ctx, sql, args...), unlock: sync.OnceFunc(s.e.mu.Unlock)}
}

// serialRows releases the txn mutex when the result set is closed. Handlers
// already defer rows.Close(), so a forgotten Close can only hold the lock
// until the transaction ends.
type serialRows struct {
	pgx.Rows
	unlock func()
}

func (r serialRows) Close() {
	r.Rows.Close()
	r.unlock()
}

// serialRow releases the txn mutex on first Scan. pgx performs its network
// work in Scan, so the lock must span QueryRow→Scan.
type serialRow struct {
	pgx.Row
	unlock func()
}

func (r serialRow) Scan(dest ...any) error {
	defer r.unlock()
	return r.Row.Scan(dest...)
}

// closedRow is a pgx.Row that always fails (used after txn end).
type closedRow struct {
	err error
}

func (r closedRow) Scan(...any) error { return r.err }

// Q returns the query target for a session: a serialized view of the open
// txn when present, otherwise the pool. Caller must Close pgx.Rows and Scan
// pgx.Row promptly so the txn lock is released; Exec needs no cleanup.
func (m *Manager) Q(id string) (Querier, bool) {
	m.mu.RLock()
	e, hasTx := m.txs[id]
	p, hasPool := m.pools[id]
	m.mu.RUnlock()
	if hasTx && e != nil && !e.done.Load() {
		m.touch(id)
		return serialQuerier{e}, true
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
	e, hasTx := m.txs[id]
	p, hasPool := m.pools[id]
	m.mu.RUnlock()
	if hasTx && e != nil && !e.done.Load() {
		m.touch(id)
		return serialQuerier{e}, nil, true
	}
	if hasPool {
		m.touch(id)
	}
	return p, p, hasPool
}

// leaseQuerier is the raw pgx.Tx held under a Manager operation lease: the
// caller already owns e.mu for the whole multi-statement operation, so no
// per-statement locking happens here. Commit/Rollback block on e.mu until
// Release is called, which gives end-to-end atomicity (no partial commit
// between batches).
type leaseQuerier struct {
	tx pgx.Tx
}

func (l leaseQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return l.tx.Query(ctx, sql, args...)
}

func (l leaseQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return l.tx.Exec(ctx, sql, args...)
}

func (l leaseQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return l.tx.QueryRow(ctx, sql, args...)
}

// AcquireLease locks the session's explicit transaction (when open) for one
// entire multi-statement operation — metadata reads, generation, and ALL
// INSERT batches — and returns the raw pgx.Tx without per-statement
// unlocking. The caller MUST call release (prefer defer) to unblock
// Commit/Rollback. When no txn is open it returns the pool with a no-op
// release. ok=false means not connected. A txn that closed before the lease
// is acquired falls back to the pool when one exists, else not-ok.
func (m *Manager) AcquireLease(id string) (q Querier, release func(), inTxn bool, pool *pgxpool.Pool, ok bool) {
	m.mu.RLock()
	e, hasTx := m.txs[id]
	p, hasPool := m.pools[id]
	m.mu.RUnlock()
	if hasTx && e != nil && !e.done.Load() {
		e.mu.Lock()
		if e.done.Load() {
			e.mu.Unlock()
			if !hasPool {
				return nil, func() {}, false, nil, false
			}
			m.touch(id)
			return p, func() {}, false, p, true
		}
		e.touch()
		m.touch(id)
		return leaseQuerier{e.tx}, sync.OnceFunc(e.mu.Unlock), true, nil, true
	}
	if !hasPool {
		return nil, func() {}, false, nil, false
	}
	m.touch(id)
	return p, func() {}, false, p, true
}

// Sweep tuning: idle sessions (no query/txn/connect activity) are closed to
// release their postgres connections. Sessions with an open explicit
// transaction are spared by SweepIdle — but a browser tab abandoned
// mid-transaction would otherwise hold locks and block vacuum forever, so
// SweepAbandonedTxns rolls back transactions idle longer than txnTTL.
const (
	DefaultSweepInterval = 5 * time.Minute
	DefaultIdleTTL       = 30 * time.Minute
	DefaultTxnIdleTTL    = 15 * time.Minute
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

// SweepAbandonedTxns rolls back explicit transactions idle longer than ttl
// and returns the affected session ids. The UI learns about it on its next
// txn-status poll (in_txn flips to false), so abandoned work is surfaced
// instead of silently holding locks.
//
// Lock discipline: m.mu is held only to copy (id, *txEntry) references and
// is always released BEFORE touching any e.mu. Commit/Rollback take
// e.mu and then m.mu, so holding both in the opposite order here would
// deadlock (sweeper: m.mu → e.mu vs commit: e.mu → m.mu).
func (m *Manager) SweepAbandonedTxns(ttl time.Duration) []string {
	cutoff := time.Now().Add(-ttl)
	m.mu.RLock()
	type ref struct {
		id string
		e  *txEntry
	}
	refs := make([]ref, 0, len(m.txs))
	for id, e := range m.txs {
		if e == nil {
			continue
		}
		refs = append(refs, ref{id, e})
	}
	m.mu.RUnlock()
	var stale []string
	for _, r := range refs {
		r.e.mu.Lock()
		idle := r.e.lastUse.Before(cutoff) && !r.e.done.Load()
		r.e.mu.Unlock()
		if idle {
			stale = append(stale, r.id)
		}
	}
	var rolled []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, id := range stale {
		if err := m.Rollback(ctx, id); err == nil {
			rolled = append(rolled, id)
		}
	}
	return rolled
}

// StartSweeper reaps idle sessions every interval until the process exits.
func (m *Manager) StartSweeper(interval, ttl, txnTTL time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			for _, id := range m.SweepIdle(ttl) {
				log.Printf("pglight: swept idle session %s", id)
			}
			for _, id := range m.SweepAbandonedTxns(txnTTL) {
				log.Printf("pglight: rolled back abandoned transaction for session %s", id)
			}
		}
	}()
}
