package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pglight/internal/logging"
)

func TestServerSettingsDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	cfg, err := LoadServerSettings(path)
	if err != nil {
		t.Fatalf("load missing settings: %v", err)
	}
	if cfg.AllowLANAccess {
		t.Fatal("missing settings must default to loopback")
	}
	if cfg.EffectiveMode() != "loopback" {
		t.Fatalf("unexpected default mode: %s", cfg.EffectiveMode())
	}

	if err := SaveServerSettings(path, ServerSettings{AllowLANAccess: true}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	got, err := LoadServerSettings(path)
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if !got.AllowLANAccess || got.EffectiveMode() != "lan" {
		t.Fatalf("unexpected saved binding: %+v", got)
	}
}

func TestSettingsSecurityPatchIsAdditive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	logger := logging.New(filepath.Join(t.TempDir(), "logging.json"))
	h := &Handler{Log: logger, ServerSettingsPath: path}

	initial := httptest.NewRecorder()
	h.Settings(initial, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if initial.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", initial.Code)
	}

	var initialBody struct {
		Logging  logging.Config `json:"logging"`
		Security struct {
			AllowLANAccess bool `json:"allow_lan_access"`
		} `json:"security"`
	}
	if err := json.Unmarshal(initial.Body.Bytes(), &initialBody); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if initialBody.Security.AllowLANAccess {
		t.Fatal("security must default to disabled")
	}

	post := httptest.NewRecorder()
	h.Settings(post, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"security":{"allow_lan_access":true}}`)))
	if post.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", post.Code)
	}
	var postBody struct {
		Logging  logging.Config `json:"logging"`
		Security struct {
			AllowLANAccess bool `json:"allow_lan_access"`
		} `json:"security"`
	}
	if err := json.Unmarshal(post.Body.Bytes(), &postBody); err != nil {
		t.Fatalf("decode POST: %v", err)
	}
	if !postBody.Security.AllowLANAccess || postBody.Logging.Level != logger.GetConfig().Level {
		t.Fatalf("security patch did not preserve settings: %+v", postBody)
	}
}

func TestSettingsUpdatesSecurityFilterLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	logger := logging.New(filepath.Join(t.TempDir(), "logging.json"))
	h := &Handler{Log: logger, ServerSettingsPath: path}

	w := httptest.NewRecorder()
	h.Settings(w, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"security":{"allow_lan_access":true}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", w.Code)
	}
	var body struct {
		Security struct {
			AllowLANAccess  bool `json:"allow_lan_access"`
			RestartRequired bool `json:"restart_required"`
		} `json:"security"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode POST: %v", err)
	}
	if !body.Security.AllowLANAccess || body.Security.RestartRequired || !h.currentServerConfig().AllowLANAccess {
		t.Fatalf("security setting was not applied live: current=%+v body=%+v", h.currentServerConfig(), body.Security)
	}
}

func TestLANAccessFiltersRemotePeers(t *testing.T) {
	h := &Handler{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	filtered := h.LANAccess(next)

	remote := httptest.NewRequest(http.MethodGet, "/", nil)
	remote.RemoteAddr = "192.168.1.20:54321"
	blocked := httptest.NewRecorder()
	filtered.ServeHTTP(blocked, remote)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("remote status = %d, want 403", blocked.Code)
	}

	local := httptest.NewRequest(http.MethodGet, "/", nil)
	local.RemoteAddr = "127.0.0.1:54321"
	allowed := httptest.NewRecorder()
	filtered.ServeHTTP(allowed, local)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("loopback status = %d, want 204", allowed.Code)
	}

	h.setServerConfig(ServerSettings{AllowLANAccess: true})
	remoteAllowed := httptest.NewRecorder()
	filtered.ServeHTTP(remoteAllowed, remote)
	if remoteAllowed.Code != http.StatusNoContent {
		t.Fatalf("remote LAN status = %d, want 204", remoteAllowed.Code)
	}
}
