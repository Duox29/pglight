package db

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Manager struct {
	mu    sync.RWMutex
	pools map[string]*pgxpool.Pool
}

func New() *Manager {
	return &Manager{pools: make(map[string]*pgxpool.Pool)}
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
	cfg.MaxConns = 5
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
	m.pools[id] = pool
	m.mu.Unlock()
	return nil
}

func (m *Manager) Get(id string) (*pgxpool.Pool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	return p, ok
}

func (m *Manager) Close(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[id]; ok {
		p.Close()
		delete(m.pools, id)
	}
}
