package api

import (
	"fmt"
	"net/http"
	"regexp"
)

// ddlRe detects schema-changing statements so the complete cache can be
// invalidated after they run (conservative: match now, drop after success).
var ddlRe = regexp.MustCompile(`(?i)\b(CREATE|ALTER|DROP|TRUNCATE|COMMENT)\b`)

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	q, sid, ok := h.q(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	// Conditional GET: the editor sends the snapshot version it holds;
	// a fresh cache entry short-circuits with 304 and zero PG queries.
	ifNone := r.Header.Get("If-None-Match")
	refresh := r.URL.Query().Get("refresh") == "1"
	snap, err := globalComplete.Get(r.Context(), q, sid, refresh)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	etag := fmt.Sprintf(`"%d"`, snap.Version)
	w.Header().Set("ETag", etag)
	if ifNone != "" && ifNone == etag && !refresh {
		w.WriteHeader(304)
		return
	}
	kw := []string{"SELECT", "FROM", "WHERE", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "OUTER", "CROSS", "ON", "USING", "ORDER", "BY", "GROUP", "HAVING", "LIMIT", "OFFSET", "FETCH", "FIRST", "DISTINCT", "ALL", "ASC", "DESC", "NULLS", "LAST", "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "RETURNING", "CONFLICT", "DO", "NOTHING", "WITH", "RECURSIVE", "AS", "UNION", "EXCEPT", "INTERSECT", "CREATE", "ALTER", "DROP", "TRUNCATE", "ADD", "COLUMN", "CONSTRAINT", "RENAME", "TO", "SCHEMA", "TABLE", "VIEW", "MATERIALIZED", "INDEX", "SEQUENCE", "FUNCTION", "TRIGGER", "TYPE", "EXTENSION", "DATABASE", "ROLE", "PRIMARY", "KEY", "FOREIGN", "REFERENCES", "CHECK", "UNIQUE", "DEFAULT", "NOT", "NULL", "CASCADE", "RESTRICT", "IF", "EXISTS", "BEGIN", "TRANSACTION", "COMMIT", "ROLLBACK", "SAVEPOINT", "EXPLAIN", "ANALYZE", "VACUUM", "REINDEX", "CLUSTER", "COPY", "GRANT", "REVOKE", "OWNER", "AND", "OR", "IS", "IN", "BETWEEN", "LIKE", "ILIKE", "CASE", "WHEN", "THEN", "ELSE", "END", "CAST", "WINDOW", "OVER", "PARTITION", "FILTER", "FOR", "SKIP", "LOCKED", "COUNT", "SUM", "AVG", "MIN", "MAX", "COALESCE", "NULLIF", "NOW()", "NOW", "STRING_AGG", "ARRAY_AGG", "EXTRACT", "GENERATE_SERIES", "CURRENT_DATE", "CURRENT_TIMESTAMP", "TRUE", "FALSE"}
	writeJSON(w, 200, map[string]any{"version": snap.Version, "tables": snap.Tables, "functions": snap.Funcs, "fks": snap.FKs, "keywords": kw, "in_txn": h.Mgr.InTxn(sid)})
}
