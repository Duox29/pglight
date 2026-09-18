package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// historyRetentionDays reads the Privacy retention setting
// (frontend key "privacy.retentionDays", default 30, clamped 1..365).
func historyRetentionDays(ctx context.Context, h *Handler) int {
	raw, err := h.Store.GetPreferences(ctx, h.UserID)
	if err != nil {
		return 30
	}
	var data map[string]any
	if json.Unmarshal([]byte(raw), &data) != nil {
		return 30
	}
	days := 30
	if v, ok := data["privacy.retentionDays"]; ok {
		if f, ok := v.(float64); ok {
			days = int(f)
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	return days
}

func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw, err := h.Store.GetPreferences(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var data any
		if json.Unmarshal([]byte(raw), &data) != nil {
			data = map[string]any{}
		}
		writeJSON(w, 200, map[string]any{"preferences": data})
	case http.MethodPost:
		var patch map[string]any
		if json.NewDecoder(r.Body).Decode(&patch) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		raw, _ := h.Store.GetPreferences(r.Context(), h.UserID)
		data := map[string]any{}
		_ = json.Unmarshal([]byte(raw), &data)
		for k, v := range patch {
			data[k] = v
		}
		merged, _ := json.Marshal(data)
		if err := h.Store.SetPreferences(r.Context(), h.UserID, string(merged)); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"preferences": data})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
