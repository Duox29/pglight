package store

import (
	"context"
	"fmt"
)

type ErdLayout struct {
	LayoutJSON   string
	ViewportJSON string
	UpdatedAt    string
}

func (s *Store) GetErdLayout(ctx context.Context, userID, connectionID, schema string) (ErdLayout, error) {
	var x ErdLayout
	err := s.db.QueryRowContext(ctx, `SELECT layout_json,viewport_json,updated_at FROM erd_layouts WHERE user_id=? AND connection_id=? AND schema_name=?`, userID, connectionID, schema).Scan(&x.LayoutJSON, &x.ViewportJSON, &x.UpdatedAt)
	if err != nil {
		return ErdLayout{}, err
	}
	return x, nil
}
func (s *Store) UpsertErdLayout(ctx context.Context, userID, connectionID, schema, layout, viewport string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO erd_layouts(user_id,connection_id,schema_name,layout_json,viewport_json) VALUES(?,?,?,?,?) ON CONFLICT(user_id,connection_id,schema_name) DO UPDATE SET layout_json=excluded.layout_json,viewport_json=excluded.viewport_json,updated_at=CURRENT_TIMESTAMP`, userID, connectionID, schema, layout, viewport)
	if err != nil {
		return fmt.Errorf("upsert erd layout: %w", err)
	}
	return nil
}
func (s *Store) DeleteErdLayout(ctx context.Context, userID, connectionID, schema string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM erd_layouts WHERE user_id=? AND connection_id=? AND schema_name=?`, userID, connectionID, schema)
	return err
}
