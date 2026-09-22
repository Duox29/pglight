package api

import (
	"net/http"
	"strings"
	"time"
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
			SQL string `json:"sql"`
			MS  int64  `json:"ms"`
			N   int64  `json:"n"`
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
		if len(req.SQL) > 2000 {
			req.SQL = req.SQL[:2000]
		}
		if _, err := h.Store.AddHistory(r.Context(), h.UserID, req.SQL, req.MS, req.N); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Enforce the retention policy (Privacy settings, default 30 days,
		// hard cap 2000 entries) so history cannot grow unbounded.
		_ = h.Store.PruneHistory(r.Context(), h.UserID,
			time.Now().AddDate(0, 0, -historyRetentionDays(r.Context(), h)), 2000)
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodGet:
		xs, err := h.Store.ListHistory(r.Context(), h.UserID, 200)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, map[string]any{"id": x.ID, "sql": x.SQL, "ms": x.DurationMS, "n": x.RowsCount, "at": x.ExecutedAt})
		}
		writeJSON(w, 200, map[string]any{"history": out})
	case http.MethodDelete:
		if err := h.Store.ClearHistory(r.Context(), h.UserID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
