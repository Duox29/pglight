package api

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSplitStatementsOffsets(t *testing.T) {
	sql := "-- lead\nSELECT 1;\n\n/* mid */\nSELECT 2;"
	stmts := splitStatements(sql)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	for i, st := range stmts {
		if got := sql[st.offset : st.offset+len(st.text)]; got != st.text {
			t.Fatalf("stmt %d slice mismatch: offset %d text %q", i, st.offset, st.text)
		}
	}
	if line, _ := lineCol(sql, stmts[1].offset); line != 5 {
		t.Fatalf("second statement starts at line 5, got %d", line)
	}
}

func TestErrLocationMapsPosition(t *testing.T) {
	script := "SELECT 1;\nSELEC 2;"
	stmts := splitStatements(script)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	// SELEC typo: Postgres points at char 3 of the second statement.
	qerr := &pgconn.PgError{Code: "42601", Message: "syntax error", Position: 3}
	loc := errLocation(script, 1, stmts[1].offset, stmts[1].text, stmts[1].text, qerr)
	if loc["line"] != 2 || loc["column"] != 3 {
		t.Fatalf("expected line 2 col 3, got %v", loc)
	}
	if loc["statement_index"] != 1 || loc["code"] != "42601" {
		t.Fatalf("missing index/code in %v", loc)
	}
}

func TestErrLocationUnwrapsSelectLimit(t *testing.T) {
	stmt := "SELECT * FROM t WHERE x = "
	exec := wrapSelect(stmt, 200)
	if exec == stmt {
		t.Fatal("expected SELECT wrapper to apply")
	}
	qerr := &pgconn.PgError{Code: "42601", Position: int32(len([]rune("SELECT * FROM (")) + 8)}
	loc := errLocation(stmt, -1, 0, stmt, exec, qerr)
	if loc["line"] != 1 || loc["column"] != 8 {
		t.Fatalf("expected line 1 col 8, got %v", loc)
	}
	if _, ok := loc["statement_index"]; ok {
		t.Fatalf("single statement must not carry statement_index: %v", loc)
	}
}

func TestErrLocationFallsBackToStatementStart(t *testing.T) {
	script := "SELECT 1;\nCREATE DOMAIN shop.email AS text;"
	stmts := splitStatements(script)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	// 42710 (duplicate object) carries no Position: jump to the statement.
	qerr := &pgconn.PgError{Code: "42710", Message: `type "email" already exists`}
	loc := errLocation(script, 1, stmts[1].offset, stmts[1].text, stmts[1].text, qerr)
	if loc["line"] != 2 || loc["column"] != 1 {
		t.Fatalf("expected statement start line 2 col 1, got %v", loc)
	}
	if loc["statement_index"] != 1 || loc["code"] != "42710" {
		t.Fatalf("missing index/code in %v", loc)
	}
	if loc := errLocation("SELECT 1", 0, 0, "SELECT 1", "SELECT 1", errors.New("boom")); loc != nil {
		t.Fatalf("non-PgError must yield nil, got %v", loc)
	}
}
