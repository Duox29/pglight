package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Store owns pglight's application data. It is deliberately separate from
// PostgreSQL target connections managed by internal/db.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "data/pglight.db"
	}
	if dir := filepath.Dir(path); dir != "." {
		// Application data can hold query text and snippets: keep the
		// directory private where the OS supports it.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create store directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Best-effort: the file may predate the 0600 policy.
	_ = os.Chmod(path, 0o600)
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	const version = 1
	var applied int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied >= version {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS app_users (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS snippets (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			sql TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_snippets_user_updated ON snippets(user_id, updated_at)`,
		`CREATE TABLE IF NOT EXISTS query_history (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			sql TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			rows_count INTEGER NOT NULL DEFAULT 0,
			executed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_history_user_time ON query_history(user_id, executed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS aliases (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			trigger TEXT NOT NULL,
			expansion TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, trigger)
		)`,
		`CREATE TABLE IF NOT EXISTS connection_profiles (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 5432,
			username TEXT NOT NULL,
			dbname TEXT NOT NULL,
			sslmode TEXT NOT NULL DEFAULT 'prefer',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at TEXT,
			UNIQUE(user_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_connections_user_updated ON connection_profiles(user_id, updated_at)`,
		`CREATE TABLE IF NOT EXISTS user_preferences (
			user_id TEXT PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS erd_layouts (
			user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			connection_id TEXT NOT NULL,
			schema_name TEXT NOT NULL,
			layout_json TEXT NOT NULL DEFAULT '{}',
			viewport_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(user_id, connection_id, schema_name)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (1)`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %d: %w", version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
