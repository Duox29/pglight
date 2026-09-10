package logging

import (
	"context"
	"time"

	"pglight/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier wraps a db.Querier and logs every statement. It is applied in one
// place (api.Handler.q) so all query paths are covered without touching
// individual handlers. QueryRow is passed through: it only hits the network
// on Scan, so timing it here would mislead.
type Querier struct {
	Inner   db.Querier
	Log     *Logger
	Session string
}

func Wrap(inner db.Querier, log *Logger, session string) db.Querier {
	if log == nil {
		return inner
	}
	return Querier{Inner: inner, Log: log, Session: session}
}

func (q Querier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	start := time.Now()
	rows, err := q.Inner.Query(ctx, sql, args...)
	q.Log.LogQuery(sql, time.Since(start).Milliseconds(), err)
	return rows, err
}

func (q Querier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	start := time.Now()
	tag, err := q.Inner.Exec(ctx, sql, args...)
	q.Log.LogQuery(sql, time.Since(start).Milliseconds(), err)
	return tag, err
}

func (q Querier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q.Inner.QueryRow(ctx, sql, args...)
}
