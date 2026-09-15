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
	kw := []string{"SELECT", "FROM", "WHERE", "JOIN", "LEFT JOIN", "ORDER BY", "GROUP BY", "HAVING", "LIMIT", "OFFSET", "INSERT INTO", "VALUES", "UPDATE", "SET", "DELETE FROM", "EXPLAIN", "ANALYZE", "CREATE TABLE", "ALTER TABLE", "DROP TABLE", "CREATE INDEX", "VACUUM", "BEGIN", "COMMIT", "ROLLBACK", "WITH", "RETURNING", "ON CONFLICT", "DISTINCT", "COUNT", "SUM", "AVG", "NOW()", "COALESCE"}
	writeJSON(w, 200, map[string]any{"version": snap.Version, "tables": snap.Tables, "functions": snap.Funcs, "fks": snap.FKs, "keywords": kw, "in_txn": h.Mgr.InTxn(sid)})
}
