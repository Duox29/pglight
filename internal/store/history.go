package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type HistoryEntry struct {
	ID            string
	SQL           string
	DurationMS    int64
	RowsCount     int64
	ExecutedAt    string
	ConnectionID  string
	DatabaseName  string
	StatementType string
	Success       bool
	ErrorCode     string
	ErrorMessage  string
	Pinned        bool
}

type HistoryFilter struct {
	Query         string
	ConnectionID  string
	Status        string
	StatementType string
	MinDurationMS int64
	PinnedOnly    bool
	Limit         int
	Offset        int
}

func (s *Store) ListHistory(ctx context.Context, userID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	return s.SearchHistory(ctx, userID, HistoryFilter{Limit: limit})
}

func (s *Store) SearchHistory(ctx context.Context, userID string, filter HistoryFilter) ([]HistoryEntry, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	clauses := []string{"user_id=?"}
	args := []any{userID}
	if filter.Query != "" {
		clauses = append(clauses, `(sql LIKE ? ESCAPE '\' OR database_name LIKE ? ESCAPE '\' OR error_message LIKE ? ESCAPE '\')`)
		term := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Query) + "%"
		args = append(args, term, term, term)
	}
	if filter.ConnectionID != "" {
		clauses = append(clauses, "connection_id=?")
		args = append(args, filter.ConnectionID)
	}
	switch filter.Status {
	case "success":
		clauses = append(clauses, "success=1")
	case "failed":
		clauses = append(clauses, "success=0")
	}
	if filter.StatementType != "" {
		clauses = append(clauses, "statement_type=?")
		args = append(args, strings.ToUpper(filter.StatementType))
	}
	if filter.MinDurationMS > 0 {
		clauses = append(clauses, "duration_ms>=?")
		args = append(args, filter.MinDurationMS)
	}
	if filter.PinnedOnly {
		clauses = append(clauses, "pinned=1")
	}
	args = append(args, filter.Limit, filter.Offset)
	query := `SELECT id,sql,duration_ms,rows_count,executed_at,connection_id,database_name,statement_type,success,error_code,error_message,pinned FROM query_history WHERE ` + strings.Join(clauses, " AND ") + ` ORDER BY pinned DESC, executed_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var x HistoryEntry
		var success, pinned int
		if err := rows.Scan(&x.ID, &x.SQL, &x.DurationMS, &x.RowsCount, &x.ExecutedAt, &x.ConnectionID, &x.DatabaseName, &x.StatementType, &success, &x.ErrorCode, &x.ErrorMessage, &pinned); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		x.Success, x.Pinned = success != 0, pinned != 0
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) AddHistory(ctx context.Context, userID, sqlText string, durationMS, rowsCount int64) (HistoryEntry, error) {
	return s.AddHistoryDetailed(ctx, userID, sqlText, durationMS, rowsCount, "", "", "", true, "", "")
}

const maxHistorySQLBytes = 1 << 20
const maxHistoryErrorBytes = 4096

func (s *Store) AddHistoryDetailed(ctx context.Context, userID, sqlText string, durationMS, rowsCount int64, connectionID, databaseName, statementType string, success bool, errorCode, errorMessage string) (HistoryEntry, error) {
	if len(sqlText) > maxHistorySQLBytes {
		return HistoryEntry{}, fmt.Errorf("query history SQL exceeds %d bytes", maxHistorySQLBytes)
	}
	if len(errorMessage) > maxHistoryErrorBytes {
		errorMessage = errorMessage[:maxHistoryErrorBytes]
	}
	x := HistoryEntry{ID: uuid.NewString(), SQL: sqlText, DurationMS: durationMS, RowsCount: rowsCount, ExecutedAt: time.Now().UTC().Format(time.RFC3339Nano), ConnectionID: connectionID, DatabaseName: databaseName, StatementType: strings.ToUpper(statementType), Success: success, ErrorCode: errorCode, ErrorMessage: errorMessage}
	_, err := s.db.ExecContext(ctx, `INSERT INTO query_history(id,user_id,sql,duration_ms,rows_count,executed_at,connection_id,database_name,statement_type,success,error_code,error_message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, userID, x.SQL, x.DurationMS, x.RowsCount, x.ExecutedAt, x.ConnectionID, x.DatabaseName, x.StatementType, success, x.ErrorCode, x.ErrorMessage)
	if err != nil {
		return HistoryEntry{}, fmt.Errorf("add history: %w", err)
	}
	return x, nil
}

func (s *Store) SetHistoryPinned(ctx context.Context, userID, id string, pinned bool) error {
	value := 0
	if pinned {
		value = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE query_history SET pinned=? WHERE user_id=? AND id=?`, value, userID, id); err != nil {
		return fmt.Errorf("pin history entry: %w", err)
	}
	return nil
}

func (s *Store) DeleteHistoryEntry(ctx context.Context, userID, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM query_history WHERE user_id=? AND id=?`, userID, id)
	if err != nil {
		return false, fmt.Errorf("delete history entry: %w", err)
	}
	n, err := result.RowsAffected()
	return n > 0, err
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
