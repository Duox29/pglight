package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"pglight/internal/logging"
)

// Settings reads (GET) or replaces (POST) the runtime config, currently the
// logging section edited from the web Settings panel.
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	if h.Log == nil {
		writeJSON(w, 500, map[string]string{"error": "logging not initialized"})
		return
	}
	switch r.Method {
	case "GET":
		writeJSON(w, 200, map[string]any{"logging": h.Log.GetConfig()})
	case "POST":
		var req struct {
			Logging logging.Config `json:"logging"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		cfg, err := h.Log.UpdateConfig(req.Logging)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"logging": cfg})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// Logs serves the recent ring-buffer entries (GET) or clears them (DELETE).
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	if h.Log == nil {
		writeJSON(w, 500, map[string]string{"error": "logging not initialized"})
		return
	}
	switch r.Method {
	case "GET":
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, 200, map[string]any{
			"entries": h.Log.Recent(limit, r.URL.Query().Get("level"), r.URL.Query().Get("category")),
		})
	case "DELETE":
		h.Log.Clear()
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
