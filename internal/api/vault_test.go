package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pglight/internal/store"
)

func vaultTestHandler(t *testing.T) *Handler {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.EnsureUser(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	return &Handler{Store: s, UserID: "u"}
}

func vaultPost(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.Vault(w, httptest.NewRequest(http.MethodPost, "/api/vault", strings.NewReader(body)))
	return w
}

func TestVaultWrongPasswordBackoffAndChange(t *testing.T) {
	h := vaultTestHandler(t)
	if w := vaultPost(t, h, `{"action":"setup","master_password":"old-password"}`); w.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", w.Code, w.Body.String())
	}
	if w := vaultPost(t, h, `{"action":"lock"}`); w.Code != http.StatusOK {
		t.Fatalf("lock status=%d", w.Code)
	}
	if w := vaultPost(t, h, `{"action":"unlock","master_password":"wrong-password"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong status=%d", w.Code)
	}
	if w := vaultPost(t, h, `{"action":"unlock","master_password":"wrong-password"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("backoff status=%d", w.Code)
	}
	h.clearVaultFailures()
	if w := vaultPost(t, h, `{"action":"unlock","master_password":"old-password"}`); w.Code != http.StatusOK {
		t.Fatalf("unlock status=%d body=%s", w.Code, w.Body.String())
	}
	if w := vaultPost(t, h, `{"action":"change_password","current_password":"old-password","new_password":"new-password"}`); w.Code != http.StatusOK {
		t.Fatalf("change status=%d body=%s", w.Code, w.Body.String())
	}
	if w := vaultPost(t, h, `{"action":"lock"}`); w.Code != http.StatusOK {
		t.Fatalf("lock2 status=%d", w.Code)
	}
	h.clearVaultFailures()
	if w := vaultPost(t, h, `{"action":"unlock","master_password":"old-password"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("old password status=%d", w.Code)
	}
	h.clearVaultFailures()
	if w := vaultPost(t, h, `{"action":"unlock","master_password":"new-password"}`); w.Code != http.StatusOK {
		t.Fatalf("new password status=%d", w.Code)
	}
}

func TestVaultStatusReportsVersionAndLockState(t *testing.T) {
	h := vaultTestHandler(t)
	w := httptest.NewRecorder()
	h.Vault(w, httptest.NewRequest(http.MethodGet, "/api/vault", nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["exists"] != false || body["unlocked"] != false || body["auto_lock_seconds"] == nil {
		t.Fatalf("unexpected status: %+v", body)
	}
}
