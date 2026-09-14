package logging

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeQuerier struct{ row pgx.Row }

func (f fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f fakeQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

type fakeRow struct{ err error }

func (r fakeRow) Scan(...any) error { return r.err }

func TestWrapLogsQueryRowOnScan(t *testing.T) {
	l := New("")
	_, _ = l.UpdateConfig(Config{Enabled: true, Level: "debug", LogHTTP: true, LogQuery: true, SlowMs: 500, MaxEntries: 500})
	wrapped := Wrap(fakeQuerier{row: fakeRow{}}, l, "session-1")
	if err := wrapped.QueryRow(context.Background(), "SELECT 1").Scan(); err != nil {
		t.Fatal(err)
	}
	entries := l.Recent(10, "debug", "query")
	if len(entries) != 1 {
		t.Fatalf("expected one query log entry, got %d", len(entries))
	}
	if entries[0].Message != "SELECT 1" {
		t.Fatalf("unexpected log message %q", entries[0].Message)
	}
}

func TestWrapLogsQueryRowError(t *testing.T) {
	l := New("")
	_, _ = l.UpdateConfig(Config{Enabled: true, Level: "debug", LogHTTP: true, LogQuery: true, SlowMs: 500, MaxEntries: 500})
	wrapped := Wrap(fakeQuerier{row: fakeRow{err: context.Canceled}}, l, "session-1")
	if err := wrapped.QueryRow(context.Background(), "SELECT 1").Scan(); err == nil {
		t.Fatal("expected scan error")
	}
	entries := l.Recent(10, "error", "query")
	if len(entries) != 1 {
		t.Fatalf("expected one query error log entry, got %d", len(entries))
	}
}
