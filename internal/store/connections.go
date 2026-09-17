package store

import (
	"context"
	"fmt"
	"github.com/google/uuid"
)

type ConnectionProfile struct {
	ID, Name, Host                                              string
	Port                                                        int
	Username, DBName, SSLMode, CreatedAt, UpdatedAt, LastUsedAt string
}

func (s *Store) ListConnections(ctx context.Context, userID string) ([]ConnectionProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,username,dbname,sslmode,created_at,updated_at,COALESCE(last_used_at,'') FROM connection_profiles WHERE user_id=? ORDER BY updated_at DESC,name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()
	out := []ConnectionProfile{}
	for rows.Next() {
		var x ConnectionProfile
		if err := rows.Scan(&x.ID, &x.Name, &x.Host, &x.Port, &x.Username, &x.DBName, &x.SSLMode, &x.CreatedAt, &x.UpdatedAt, &x.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) UpsertConnection(ctx context.Context, userID, name, host string, port int, username, dbname, sslmode string) (ConnectionProfile, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO connection_profiles(id,user_id,name,host,port,username,dbname,sslmode) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET host=excluded.host,port=excluded.port,username=excluded.username,dbname=excluded.dbname,sslmode=excluded.sslmode,updated_at=CURRENT_TIMESTAMP`, id, userID, name, host, port, username, dbname, sslmode)
	if err != nil {
		return ConnectionProfile{}, fmt.Errorf("upsert connection: %w", err)
	}
	var x ConnectionProfile
	err = s.db.QueryRowContext(ctx, `SELECT id,name,host,port,username,dbname,sslmode,created_at,updated_at,COALESCE(last_used_at,'') FROM connection_profiles WHERE user_id=? AND name=?`, userID, name).Scan(&x.ID, &x.Name, &x.Host, &x.Port, &x.Username, &x.DBName, &x.SSLMode, &x.CreatedAt, &x.UpdatedAt, &x.LastUsedAt)
	return x, err
}
func (s *Store) DeleteConnection(ctx context.Context, userID, name string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM connection_profiles WHERE user_id=? AND name=?`, userID, name)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
