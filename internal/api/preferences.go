package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const shortcutSettingsVersion = 1

var shortcutCommands = map[string]struct{}{
	"palette.open": {}, "query.new": {}, "query.run": {}, "query.complete": {}, "query.cancel": {}, "query.format": {},
	"query.explain": {}, "query.clearResults": {}, "query.saveSnippet": {},
	"tab.close": {}, "tab.closeOthers": {}, "tab.closeLeft": {}, "tab.closeRight": {}, "tab.closeAll": {},
	"tab.next": {}, "tab.previous": {}, "tab.activate.1": {}, "tab.activate.2": {}, "tab.activate.3": {},
	"tab.activate.4": {}, "tab.activate.5": {}, "tab.activate.6": {}, "tab.activate.7": {}, "tab.activate.8": {}, "tab.activate.9": {},
	"table.refresh": {}, "table.nextPage": {}, "table.previousPage": {}, "workspace.history": {}, "workspace.snippets": {},
	"workspace.dashboard": {}, "workspace.settings": {}, "workspace.shortcuts": {}, "workspace.docs": {}, "split.right": {}, "split.down": {},
	"split.swap": {}, "split.close": {}, "explorer.refresh": {}, "session.connect": {}, "session.disconnect": {},
}

type shortcutSettings struct {
	Version   int                 `json:"version"`
	Overrides map[string][]string `json:"overrides"`
}

func validShortcut(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	parts := strings.Split(value, "+")
	if len(parts) < 1 || strings.TrimSpace(parts[len(parts)-1]) == "" {
		return false
	}
	seen := map[string]bool{}
	for i, raw := range parts {
		part := strings.TrimSpace(raw)
		if part == "" || seen[part] {
			return false
		}
		seen[part] = true
		if i < len(parts)-1 {
			switch part {
			case "Mod", "Ctrl", "Meta", "Alt", "Shift":
			default:
				return false
			}
		} else {
			switch part {
			case "Mod", "Ctrl", "Meta", "Alt", "Shift":
				return false
			}
		}
	}
	return true
}

func validateShortcutSettings(settings shortcutSettings) error {
	if settings.Version != shortcutSettingsVersion {
		return fmt.Errorf("unsupported shortcut settings version")
	}
	if len(settings.Overrides) > 128 {
		return fmt.Errorf("too many shortcut commands")
	}
	for command, bindings := range settings.Overrides {
		if _, ok := shortcutCommands[command]; !ok {
			return fmt.Errorf("unknown shortcut command %q", command)
		}
		if len(bindings) > 4 {
			return fmt.Errorf("too many bindings for %q", command)
		}
		for _, binding := range bindings {
			if !validShortcut(binding) {
				return fmt.Errorf("invalid shortcut binding for %q", command)
			}
		}
	}
	return nil
}

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

// ShortcutPreferences persists application-wide shortcut overrides separately
// from the target PostgreSQL sessions. Defaults remain a frontend concern so
// new commands can ship without rewriting every user's preference blob.
func (h *Handler) ShortcutPreferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw, err := h.Store.GetPreferences(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var root map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &root) != nil {
			root = map[string]json.RawMessage{}
		}
		settings := shortcutSettings{Version: shortcutSettingsVersion, Overrides: map[string][]string{}}
		if encoded, ok := root["shortcuts"]; ok {
			var stored shortcutSettings
			if json.Unmarshal(encoded, &stored) == nil && validateShortcutSettings(stored) == nil {
				settings = stored
			}
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "shortcut settings body is too large"})
			return
		}
		var settings shortcutSettings
		if json.Unmarshal(body, &settings) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := validateShortcutSettings(settings); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		raw, err := h.Store.GetPreferences(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var root map[string]any
		if json.Unmarshal([]byte(raw), &root) != nil {
			root = map[string]any{}
		}
		root["shortcuts"] = settings
		merged, _ := json.Marshal(root)
		if err := h.Store.SetPreferences(r.Context(), h.UserID, string(merged)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, settings)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
