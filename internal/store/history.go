package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type HistoryEntry struct {
	ID         string
	SQL        string
	DurationMS int64
	RowsCount  int64
	ExecutedAt string
}

func (s *Store) ListHistory(ctx context.Context, userID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,sql,duration_ms,rows_count,executed_at FROM query_history WHERE user_id=? ORDER BY executed_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var x HistoryEntry
		if err := rows.Scan(&x.ID, &x.SQL, &x.DurationMS, &x.RowsCount, &x.ExecutedAt); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) AddHistory(ctx context.Context, userID, sqlText string, durationMS, rowsCount int64) (HistoryEntry, error) {
	x := HistoryEntry{ID: uuid.NewString(), SQL: sqlText, DurationMS: durationMS, RowsCount: rowsCount, ExecutedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO query_history(id,user_id,sql,duration_ms,rows_count,executed_at) VALUES(?,?,?,?,?,?)`, x.ID, userID, x.SQL, x.DurationMS, x.RowsCount, x.ExecutedAt)
	if err != nil {
		return HistoryEntry{}, fmt.Errorf("add history: %w", err)
	}
	return x, nil
}

// PruneHistory deletes entries older than before, then caps the per-user
// history at keepMax rows. Retention is enforced on every add (see History).
func (s *Store) PruneHistory(ctx context.Context, userID string, before time.Time, keepMax int) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM query_history WHERE user_id=? AND executed_at < ?`,
		userID, before.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("prune history by age: %w", err)
	}
	if keepMax > 0 {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM query_history WHERE user_id=? AND id NOT IN (
			SELECT id FROM query_history WHERE user_id=? ORDER BY executed_at DESC LIMIT ?)`,
			userID, userID, keepMax); err != nil {
			return fmt.Errorf("prune history by count: %w", err)
		}
	}
	return nil
}

func (s *Store) ClearHistory(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM query_history WHERE user_id=?`, userID)
	if err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	return nil
}
