package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) GetPreferences(ctx context.Context, userID string) (string, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM user_preferences WHERE user_id=?`, userID).Scan(&data)
	if err != nil {
		if err == sql.ErrNoRows {
			return "{}", nil
		}
		return "{}", err
	}
	return data, nil
}
func (s *Store) SetPreferences(ctx context.Context, userID, data string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_preferences(user_id,data) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET data=excluded.data,updated_at=CURRENT_TIMESTAMP`, userID, data)
	if err != nil {
		return fmt.Errorf("set preferences: %w", err)
	}
	return nil
}
