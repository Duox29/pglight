package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"pglight/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestParseFilterValuePreservesDecimalPrecision(t *testing.T) {
	got, err := parseFilterValue("123456789012.123456")
	if err != nil {
		t.Fatal(err)
	}
	n, ok := got.(pgtype.Numeric)
	if !ok {
		t.Fatalf("decimal filter became %T, want pgtype.Numeric", got)
	}
	value, err := n.Value()
	if err != nil || value != "123456789012.123456" {
		t.Fatalf("decimal filter lost precision: value=%v err=%v", value, err)
	}
}

type contractRows struct {
	valuesErr error
}

func (r *contractRows) Close()                        {}
func (r *contractRows) Err() error                    { return nil }
func (r *contractRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 1") }
func (r *contractRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{{Name: "value", DataTypeOID: 23}}
}
func (r *contractRows) Next() bool             { return true }
func (r *contractRows) Scan(...any) error      { return nil }
func (r *contractRows) Values() ([]any, error) { return nil, r.valuesErr }
func (r *contractRows) RawValues() [][]byte    { return nil }
func (r *contractRows) Conn() *pgx.Conn        { return nil }
func (r *contractRows) TypeMap() *pgtype.Map   { return nil }

type contractQuerier struct{ rows pgx.Rows }

func (q contractQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) { return q.rows, nil }
func (contractQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (contractQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func TestExecQueryStreamErrorIncludesTxnState(t *testing.T) {
	h := &Handler{Mgr: db.New()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/query", nil)
	h.execQuery(w, r, contractQuerier{rows: &contractRows{valuesErr: errors.New("decode failed")}}, "missing", "SELECT 1", nil, -1)

	if w.Code != 500 {
		t.Fatalf("status=%d want 500 body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["in_txn"] != false {
		t.Fatalf("stream errors must include in_txn=false, body=%s", w.Body.String())
	}
}

func TestDecodeBodyRejectsTrailingJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}{"second":true}`))
	var body struct {
		OK bool `json:"ok"`
	}
	if err := decodeBody(r, &body); err == nil {
		t.Fatal("decodeBody accepted trailing JSON")
	}
}
