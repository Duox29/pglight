package test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"pglight/internal/logging"
)

type fakeQuerier struct{ row pgx.Row }

func (f fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f fakeQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

type fakeRow struct{ err error }

func (r fakeRow) Scan(...any) error { return r.err }

func logCfg() logging.Config {
	return logging.Config{Enabled: true, Level: "debug", LogHTTP: true, LogQuery: true, SlowMs: 500, MaxEntries: 500}
}

func TestLoggingWrapRowScan(t *testing.T) {
	l := logging.New("")
	if _, err := l.UpdateConfig(logCfg()); err != nil {
		t.Fatal(err)
	}
	wrapped := logging.Wrap(fakeQuerier{row: fakeRow{}}, l, "session-1")
	if err := wrapped.QueryRow(context.Background(), "SELECT 1").Scan(); err != nil {
		t.Fatal(err)
	}
	entries := l.Recent(10, "debug", "query")
	if len(entries) != 1 {
		t.Fatalf("expected one query log entry, got %d", len(entries))
	}
	requireDeep(t, "", "entry", map[string]any{
		"level": entries[0].Level, "category": entries[0].Category,
		"message": entries[0].Message,
	}, map[string]any{
		"level": "debug", "category": "query", "message": "SELECT 1",
	})
}

func TestLoggingWrapRowError(t *testing.T) {
	l := logging.New("")
	if _, err := l.UpdateConfig(logCfg()); err != nil {
		t.Fatal(err)
	}
	wrapped := logging.Wrap(fakeQuerier{row: fakeRow{err: context.Canceled}}, l, "session-1")
	if err := wrapped.QueryRow(context.Background(), "SELECT 1").Scan(); err == nil {
		t.Fatal("expected scan error")
	}
	entries := l.Recent(10, "error", "query")
	if len(entries) != 1 {
		t.Fatalf("expected one query error log entry, got %d", len(entries))
	}
}

func TestLoggingEdge(t *testing.T) {
	// nil logger => Wrap returns inner unwrapped (no panic, no log)
	inner := fakeQuerier{row: fakeRow{}}
	if w := logging.Wrap(inner, nil, "s"); w == nil {
		t.Fatal("nil-log Wrap returned nil")
	}
	// disabled logger records nothing
	l := logging.New("")
	if _, err := l.UpdateConfig(logging.Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	wrapped := logging.Wrap(inner, l, "s")
	_ = wrapped.QueryRow(context.Background(), "SELECT 1").Scan()
	if got := l.Recent(10, "debug", "query"); len(got) != 0 {
		t.Fatalf("disabled logger recorded %d entries", len(got))
	}
	// Clear drops entries
	l2 := logging.New("")
	if _, err := l2.UpdateConfig(logCfg()); err != nil {
		t.Fatal(err)
	}
	_ = logging.Wrap(inner, l2, "s").QueryRow(context.Background(), "SELECT 1").Scan()
	l2.Clear()
	if got := l2.Recent(10, "debug", "query"); len(got) != 0 {
		t.Fatalf("clear failed, %d entries remain", len(got))
	}
}
