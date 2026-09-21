package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"pglight/internal/logging"
)

const ServerSettingsPath = "data/server.json"

// ServerSettings contains process-level HTTP exposure settings. It is kept
// separate from logging.json because binding affects startup, not logging.
type ServerSettings struct {
	AllowLANAccess bool `json:"allow_lan_access"`
}

func (s ServerSettings) EffectiveMode() string {
	if s.AllowLANAccess {
		return "lan"
	}
	return "loopback"
}

func LoadServerSettings(path string) (ServerSettings, error) {
	if path == "" {
		path = ServerSettingsPath
	}
	cfg := ServerSettings{}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read server settings: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ServerSettings{}, fmt.Errorf("decode server settings: %w", err)
	}
	return cfg, nil
}

func SaveServerSettings(path string, cfg ServerSettings) error {
	if path == "" {
		path = ServerSettingsPath
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create server settings directory: %w", err)
		}
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server settings: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write server settings: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func (h *Handler) serverSettingsPath() string {
	if h.ServerSettingsPath == "" {
		return ServerSettingsPath
	}
	return h.ServerSettingsPath
}

func (h *Handler) currentServerConfig() ServerSettings {
	h.serverConfigMu.RLock()
	defer h.serverConfigMu.RUnlock()
	return h.ServerConfig
}

func (h *Handler) setServerConfig(cfg ServerSettings) {
	h.serverConfigMu.Lock()
	h.ServerConfig = cfg
	h.serverConfigMu.Unlock()
}

// LANAccess filters HTTP requests by the peer address. The listener remains
// available on all interfaces so the setting can be changed without a server
// restart; when disabled, non-loopback peers receive a 403 response.
func (h *Handler) LANAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.currentServerConfig().AllowLANAccess && !isLoopbackPeer(r.RemoteAddr) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "LAN access disabled"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Settings reads (GET) or updates (POST) the runtime config. Logging remains
// persisted by the logger; server exposure is persisted separately because it
// is consumed by the live HTTP access filter and at startup.
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	if h.Log == nil {
		writeJSON(w, 500, map[string]string{"error": "logging not initialized"})
		return
	}
	switch r.Method {
	case "GET":
		effective := h.currentServerConfig()
		server, err := LoadServerSettings(h.serverSettingsPath())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{
			"logging": h.Log.GetConfig(),
			"security": map[string]any{
				"allow_lan_access": server.AllowLANAccess,
				"effective_mode":   effective.EffectiveMode(),
				"restart_required": server.AllowLANAccess != effective.AllowLANAccess,
			},
		})
	case "POST":
		var req struct {
			Logging  *logging.Config `json:"logging"`
			Security *ServerSettings `json:"security"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		effective := h.currentServerConfig()
		if req.Logging != nil {
			if _, err := h.Log.UpdateConfig(*req.Logging); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
		}
		server, err := LoadServerSettings(h.serverSettingsPath())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if req.Security != nil {
			server = *req.Security
			if err := SaveServerSettings(h.serverSettingsPath(), server); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			h.setServerConfig(server)
			effective = server
		}
		writeJSON(w, 200, map[string]any{
			"logging": h.Log.GetConfig(),
			"security": map[string]any{
				"allow_lan_access": server.AllowLANAccess,
				"effective_mode":   effective.EffectiveMode(),
				"restart_required": server.AllowLANAccess != effective.AllowLANAccess,
			},
		})
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
