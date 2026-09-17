package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Alias struct {
	ID        string
	Trigger   string
	Expansion string
	CreatedAt string
	UpdatedAt string
}

func (s *Store) EnsureUser(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("user id is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_users(id) VALUES (?) ON CONFLICT(id) DO NOTHING`, userID)
	return err
}

func (s *Store) ListAliases(ctx context.Context, userID string) ([]Alias, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, trigger, expansion, created_at, updated_at FROM aliases WHERE user_id=? ORDER BY trigger`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.ID, &a.Trigger, &a.Expansion, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAlias(ctx context.Context, userID, trigger, expansion string) (Alias, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO aliases(id,user_id,trigger,expansion,created_at,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id,trigger) DO UPDATE SET expansion=excluded.expansion, updated_at=excluded.updated_at`,
		id, userID, trigger, expansion, now, now)
	if err != nil {
		return Alias{}, err
	}
	var a Alias
	err = s.db.QueryRowContext(ctx, `SELECT id, trigger, expansion, created_at, updated_at FROM aliases WHERE user_id=? AND trigger=?`, userID, trigger).
		Scan(&a.ID, &a.Trigger, &a.Expansion, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Store) DeleteAlias(ctx context.Context, userID, trigger string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM aliases WHERE user_id=? AND trigger=?`, userID, trigger)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
