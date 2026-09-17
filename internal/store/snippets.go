package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Snippet struct {
	ID        string
	Name      string
	SQL       string
	CreatedAt string
	UpdatedAt string
}

func (s *Store) ListSnippets(ctx context.Context, userID string) ([]Snippet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,sql,created_at,updated_at FROM snippets WHERE user_id=? ORDER BY updated_at DESC, name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list snippets: %w", err)
	}
	defer rows.Close()
	out := []Snippet{}
	for rows.Next() {
		var x Snippet
		if err := rows.Scan(&x.ID, &x.Name, &x.SQL, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan snippet: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSnippet(ctx context.Context, userID, name, sqlText string) (Snippet, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO snippets(id,user_id,name,sql) VALUES(?,?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET sql=excluded.sql,updated_at=CURRENT_TIMESTAMP`, id, userID, name, sqlText)
	if err != nil {
		return Snippet{}, fmt.Errorf("upsert snippet: %w", err)
	}
	var x Snippet
	err = s.db.QueryRowContext(ctx, `SELECT id,name,sql,created_at,updated_at FROM snippets WHERE user_id=? AND name=?`, userID, name).Scan(&x.ID, &x.Name, &x.SQL, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func (s *Store) DeleteSnippet(ctx context.Context, userID, name string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM snippets WHERE user_id=? AND name=?`, userID, name)
	if err != nil {
		return false, fmt.Errorf("delete snippet: %w", err)
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
