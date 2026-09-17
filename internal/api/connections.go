package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Handler) Connections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		xs, err := h.Store.ListConnections(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, map[string]any{"id": x.ID, "name": x.Name, "host": x.Host, "port": x.Port, "user": x.Username, "dbname": x.DBName, "sslmode": x.SSLMode})
		}
		writeJSON(w, 200, map[string]any{"connections": out})
	case http.MethodPost:
		var req struct {
			Name, Host, User, DBName, SSLMode string
			Port                              int `json:"port"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Host = strings.TrimSpace(req.Host)
		req.User = strings.TrimSpace(req.User)
		req.DBName = strings.TrimSpace(req.DBName)
		req.SSLMode = strings.TrimSpace(req.SSLMode)
		if req.Name == "" || req.Host == "" || req.User == "" || req.DBName == "" {
			writeJSON(w, 400, map[string]string{"error": "name,host,user,dbname are required"})
			return
		}
		if req.Port <= 0 {
			req.Port = 5432
		}
		if req.SSLMode == "" {
			req.SSLMode = "prefer"
		}
		x, err := h.Store.UpsertConnection(r.Context(), h.UserID, req.Name, req.Host, req.Port, req.User, req.DBName, req.SSLMode)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"connection": map[string]any{"id": x.ID, "name": x.Name, "host": x.Host, "port": x.Port, "user": x.Username, "dbname": x.DBName, "sslmode": x.SSLMode}})
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeJSON(w, 400, map[string]string{"error": "name is required"})
			return
		}
		ok, err := h.Store.DeleteConnection(r.Context(), h.UserID, name)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "connection not found"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
