package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/store"
)

func (h *Handler) Snippets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		xs, err := h.Store.ListSnippets(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, map[string]any{"id": x.ID, "name": x.Name, "sql": x.SQL, "created_at": x.CreatedAt, "updated_at": x.UpdatedAt})
		}
		writeJSON(w, 200, map[string]any{"snippets": out})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			SQL  string `json:"sql"`
		}
		if decodeBody(r, &req) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > 200 {
			writeJSON(w, 400, map[string]string{"error": "name must be 1-200 chars"})
			return
		}
		if strings.TrimSpace(req.SQL) == "" || len(req.SQL) > 100000 {
			writeJSON(w, 400, map[string]string{"error": "sql must be 1-100000 chars"})
			return
		}
		x, err := h.Store.UpsertSnippet(r.Context(), h.UserID, req.Name, req.SQL)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"snippet": map[string]any{"id": x.ID, "name": x.Name, "sql": x.SQL}})
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeJSON(w, 400, map[string]string{"error": "name is required"})
			return
		}
		ok, err := h.Store.DeleteSnippet(r.Context(), h.UserID, name)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "snippet not found"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			SessionID    string `json:"session_id"`
			SQL          string `json:"sql"`
			MS           int64  `json:"ms"`
			N            int64  `json:"n"`
			Success      *bool  `json:"success"`
			ErrorCode    string `json:"error_code"`
			ErrorMessage string `json:"error_message"`
		}
		if decodeBody(r, &req) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		req.SQL = strings.TrimSpace(req.SQL)
		if req.SQL == "" {
			writeJSON(w, 400, map[string]string{"error": "sql is required"})
			return
		}
		if req.MS < 0 {
			req.MS = 0
		}
		if req.N < 0 {
			req.N = 0
		}
		if len(req.ErrorCode) > 32 {
			req.ErrorCode = req.ErrorCode[:32]
		}
		connectionID, databaseName := "", ""
		if h.Mgr != nil {
			if meta, ok := h.Mgr.Info(req.SessionID); ok {
				connectionID, databaseName = meta.ProfileID, meta.DbName
			}
		}
		if connectionID == "" {
			connectionID = req.SessionID
		}
		entry, err := h.Store.AddHistoryDetailed(r.Context(), h.UserID, req.SQL, req.MS, req.N, connectionID, databaseName, queryStatementType(req.SQL), req.Success == nil || *req.Success, req.ErrorCode, req.ErrorMessage)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		// Enforce the retention policy (Privacy settings, default 30 days,
		// hard cap 2000 entries) so history cannot grow unbounded.
		_ = h.Store.PruneHistory(r.Context(), h.UserID,
			time.Now().AddDate(0, 0, -historyRetentionDays(r.Context(), h)), 2000)
		writeJSON(w, 200, map[string]any{"ok": true, "id": entry.ID})
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		if offset > 2000 {
			offset = 2000
		}
		minMS, _ := strconv.ParseInt(r.URL.Query().Get("min_ms"), 10, 64)
		if minMS < 0 {
			minMS = 0
		}
		search := r.URL.Query().Get("q")
		if len(search) > 256 {
			search = search[:256]
		}
		xs, err := h.Store.SearchHistory(r.Context(), h.UserID, store.HistoryFilter{Query: search, ConnectionID: r.URL.Query().Get("connection_id"), Status: r.URL.Query().Get("status"), StatementType: r.URL.Query().Get("statement_type"), MinDurationMS: minMS, PinnedOnly: r.URL.Query().Get("pinned") == "true", Limit: limit + 1, Offset: offset})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		hasMore := len(xs) > limit
		if hasMore {
			xs = xs[:limit]
		}
		out := make([]map[string]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, map[string]any{"id": x.ID, "sql": x.SQL, "ms": x.DurationMS, "n": x.RowsCount, "at": x.ExecutedAt, "connection_id": x.ConnectionID, "database": x.DatabaseName, "statement_type": x.StatementType, "success": x.Success, "error_code": x.ErrorCode, "error_message": x.ErrorMessage, "pinned": x.Pinned})
		}
		writeJSON(w, 200, map[string]any{"history": out, "has_more": hasMore})
	case http.MethodDelete:
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			ok, err := h.Store.DeleteHistoryEntry(r.Context(), h.UserID, id)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			if !ok {
				writeJSON(w, 404, map[string]string{"error": "history entry not found"})
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		if err := h.Store.ClearHistory(r.Context(), h.UserID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodPut:
		var req struct {
			ID     string `json:"id"`
			Pinned bool   `json:"pinned"`
		}
		if err := decodeBody(r, &req); err != nil || req.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id and pinned are required"})
			return
		}
		if err := h.Store.SetHistoryPinned(r.Context(), h.UserID, req.ID, req.Pinned); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func queryStatementType(sql string) string {
	keyword := firstSQLKeyword(strings.TrimSpace(sql))
	if keyword == "WITH" && topLevelResultKeyword(sql) {
		return "SELECT"
	}
	if keyword == "" {
		return "UNKNOWN"
	}
	return strings.ToUpper(keyword)
}
