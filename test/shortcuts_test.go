package test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pglight/internal/api"
	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"
)

func TestShortcutPreferencesDefaultsAndRoundTrip(t *testing.T) {
	h := newStoreHandler(t)

	code, body := callGET(t, h.ShortcutPreferences, "/api/preferences/shortcuts")
	requireStatus(t, body, code, http.StatusOK)
	requireDeep(t, body, "default.version", decodeObj(t, body)["version"], 1)
	requireDeep(t, body, "default.overrides", decodeObj(t, body)["overrides"], map[string]any{})

	code, body = callMethod(t, h.ShortcutPreferences, http.MethodPut, "/api/preferences/shortcuts", `{"version":1,"overrides":{"query.run":["F9","Mod+Enter"],"palette.open":["Mod+K"]}}`)
	requireStatus(t, body, code, http.StatusOK)
	requireDeep(t, body, "put.version", decodeObj(t, body)["version"], 1)

	code, body = callGET(t, h.ShortcutPreferences, "/api/preferences/shortcuts")
	requireStatus(t, body, code, http.StatusOK)
	out := decodeObj(t, body)
	requireDeep(t, body, "round-trip.run", out["overrides"].(map[string]any)["query.run"], []any{"F9", "Mod+Enter"})
	requireDeep(t, body, "round-trip.palette", out["overrides"].(map[string]any)["palette.open"], []any{"Mod+K"})
}

func TestShortcutPreferencesValidation(t *testing.T) {
	h := newStoreHandler(t)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"bad json", `{`, "invalid json"},
		{"bad version", `{"version":2,"overrides":{}}`, "unsupported shortcut settings version"},
		{"unknown command", `{"version":1,"overrides":{"server.shell.execute":["F9"]}}`, "unknown shortcut command"},
		{"too many bindings", `{"version":1,"overrides":{"query.run":["F1","F2","F3","F4","F5"]}}`, "too many bindings"},
		{"duplicate modifier", `{"version":1,"overrides":{"query.run":["Ctrl+Ctrl+K"]}}`, "invalid shortcut binding"},
		{"modifier as key", `{"version":1,"overrides":{"query.run":["Ctrl"]}}`, "invalid shortcut binding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := callMethod(t, h.ShortcutPreferences, http.MethodPut, "/api/preferences/shortcuts", tc.body)
			requireErrContains(t, body, code, http.StatusBadRequest, tc.want)
		})
	}

	tooLarge := `{"version":1,"overrides":{"query.run":["` + strings.Repeat("A", 70) + `"]}}`
	code, body := callMethod(t, h.ShortcutPreferences, http.MethodPut, "/api/preferences/shortcuts", tooLarge)
	requireErrContains(t, body, code, http.StatusBadRequest, "invalid shortcut binding")

	huge := `{"version":1,"overrides":{}}` + strings.Repeat(" ", 70<<10)
	code, body = callMethod(t, h.ShortcutPreferences, http.MethodPut, "/api/preferences/shortcuts", huge)
	requireErrContains(t, body, code, http.StatusRequestEntityTooLarge, "body is too large")

	code, body = callMethod(t, h.ShortcutPreferences, http.MethodPost, "/api/preferences/shortcuts", `{}`)
	requireErrContains(t, body, code, http.StatusMethodNotAllowed, "method not allowed")
}

func TestShortcutPreferencesUserIsolation(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callMethod(t, h.ShortcutPreferences, http.MethodPut, "/api/preferences/shortcuts", `{"version":1,"overrides":{"query.run":["F9"]}}`)
	requireStatus(t, body, code, http.StatusOK)
	if err := h.Store.EnsureUser(context.Background(), "other-user"); err != nil {
		t.Fatal(err)
	}
	h.UserID = "other-user"
	code, body = callGET(t, h.ShortcutPreferences, "/api/preferences/shortcuts")
	requireStatus(t, body, code, http.StatusOK)
	requireDeep(t, body, "isolated.overrides", decodeObj(t, body)["overrides"], map[string]any{})
}

func TestIntegrationShortcutPreferencesHTTP(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	const user = "shortcut-http-user"
	if err := s.EnsureUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	h := &api.Handler{Mgr: db.New(), Log: logging.New(""), Store: s, UserID: user}
	srv := httptest.NewServer(integrationMux(h))
	t.Cleanup(srv.Close)

	resp, err := integrationClient.Get(srv.URL + "/api/preferences/shortcuts")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", resp.StatusCode, initial)
	}

	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/preferences/shortcuts", strings.NewReader(`{"version":1,"overrides":{"query.new":["F8"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = integrationClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", resp.StatusCode, updated)
	}
	var payload map[string]any
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != float64(1) {
		t.Fatalf("PUT response=%s", updated)
	}

	code, body := iget(t, srv.URL+"/api/preferences/shortcuts")
	requireStatus(t, body, code, http.StatusOK)
	requireDeep(t, body, "http.persisted", decodeObj(t, body)["overrides"].(map[string]any)["query.new"], []any{"F8"})
}
