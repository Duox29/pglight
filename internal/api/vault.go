package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/store"
)

const (
	minMasterPasswordLen = 8
	maxMasterPasswordLen = 256
	vaultIdleTimeout     = 15 * time.Minute
	vaultMaxBackoff      = 30 * time.Second
)

func (h *Handler) vaultUnlocked() bool {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	if len(h.vaultKey) == 0 {
		return false
	}
	h.armVaultTimerLocked()
	return true
}

func (h *Handler) copyVaultKey() []byte {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	if len(h.vaultKey) == 0 {
		return nil
	}
	h.armVaultTimerLocked()
	return append([]byte(nil), h.vaultKey...)
}

func (h *Handler) setVaultKey(key []byte) {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	h.vaultEpoch++
	if h.vaultTimer != nil {
		h.vaultTimer.Stop()
		h.vaultTimer = nil
	}
	for i := range h.vaultKey {
		h.vaultKey[i] = 0
	}
	h.vaultKey = append([]byte(nil), key...)
	if len(h.vaultKey) > 0 {
		h.armVaultTimerLocked()
	}
}

func (h *Handler) armVaultTimerLocked() {
	if len(h.vaultKey) == 0 {
		return
	}
	h.vaultEpoch++
	epoch := h.vaultEpoch
	if h.vaultTimer != nil {
		h.vaultTimer.Stop()
	}
	h.vaultTimer = time.AfterFunc(vaultIdleTimeout, func() { h.lockVaultIfEpoch(epoch) })
}

func (h *Handler) lockVaultIfEpoch(epoch uint64) {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	if h.vaultEpoch != epoch || len(h.vaultKey) == 0 {
		return
	}
	h.vaultEpoch++
	if h.vaultTimer != nil {
		h.vaultTimer = nil
	}
	for i := range h.vaultKey {
		h.vaultKey[i] = 0
	}
	h.vaultKey = nil
}

func (h *Handler) lockVault() {
	h.setVaultKey(nil)
}

func validMasterPassword(password string) error {
	if len([]rune(password)) < minMasterPasswordLen {
		return errors.New("master password must be at least 8 characters")
	}
	if len([]rune(password)) > maxMasterPasswordLen {
		return errors.New("master password is too long")
	}
	return nil
}

func (h *Handler) recordVaultFailure() (time.Duration, int) {
	h.vaultFailMu.Lock()
	defer h.vaultFailMu.Unlock()
	if h.vaultFailures == nil {
		h.vaultFailures = make(map[string]vaultFailure)
	}
	f := h.vaultFailures[h.UserID]
	f.count++
	backoff := 250 * time.Millisecond
	for i := 1; i < f.count && backoff < vaultMaxBackoff; i++ {
		backoff *= 2
	}
	if backoff > vaultMaxBackoff {
		backoff = vaultMaxBackoff
	}
	f.retryAt = time.Now().Add(backoff)
	h.vaultFailures[h.UserID] = f
	return backoff, f.count
}

func (h *Handler) clearVaultFailures() {
	h.vaultFailMu.Lock()
	defer h.vaultFailMu.Unlock()
	if h.vaultFailures != nil {
		delete(h.vaultFailures, h.UserID)
	}
}

func vaultRetryResponse(w http.ResponseWriter, wait time.Duration) {
	seconds := int(wait / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many failed vault unlock attempts; try again later"})
}

func vaultAuthFailure(w http.ResponseWriter, wait time.Duration, count int) {
	if count == 1 {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid master password"})
		return
	}
	vaultRetryResponse(w, wait)
}

// Vault exposes the local encrypted credential vault. The master password is
// accepted only for lifecycle operations and is never returned or persisted.
func (h *Handler) Vault(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		record, err := h.Store.Vault(r.Context(), h.UserID)
		version := 0
		if err == nil {
			version = record.Version
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"exists":            err == nil,
			"unlocked":          h.vaultUnlocked(),
			"version":           version,
			"auto_lock_seconds": int(vaultIdleTimeout / time.Second),
		})
		return
	case http.MethodPost:
		var req struct {
			Action          string `json:"action"`
			MasterPassword  string `json:"master_password"`
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		switch action {
		case "setup":
			if err := validMasterPassword(req.MasterPassword); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			record, key, err := store.NewVault(req.MasterPassword)
			if err == nil {
				err = h.Store.CreateVault(r.Context(), h.UserID, record)
			}
			if err != nil {
				for i := range key {
					key[i] = 0
				}
				code := http.StatusInternalServerError
				if errors.Is(err, store.ErrVaultExists) {
					code = http.StatusConflict
				}
				writeJSON(w, code, map[string]string{"error": err.Error()})
				return
			}
			h.setVaultKey(key)
			h.clearVaultFailures()
			for i := range key {
				key[i] = 0
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exists": true, "unlocked": true})
		case "unlock":
			if err := validMasterPassword(req.MasterPassword); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			record, err := h.Store.Vault(r.Context(), h.UserID)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			key, err := store.UnlockVault(record, req.MasterPassword)
			if err != nil {
				wait, count := h.recordVaultFailure()
				vaultAuthFailure(w, wait, count)
				return
			}
			h.setVaultKey(key)
			h.clearVaultFailures()
			for i := range key {
				key[i] = 0
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exists": true, "unlocked": true})
		case "lock":
			h.lockVault()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exists": true, "unlocked": false})
		case "change_password":
			if err := validMasterPassword(req.CurrentPassword); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := validMasterPassword(req.NewPassword); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := h.Store.ChangeVaultMaster(r.Context(), h.UserID, req.CurrentPassword, req.NewPassword); err != nil {
				if errors.Is(err, store.ErrVaultInvalid) {
					wait, count := h.recordVaultFailure()
					vaultAuthFailure(w, wait, count)
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			record, err := h.Store.Vault(r.Context(), h.UserID)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			key, err := store.UnlockVault(record, req.NewPassword)
			if err != nil {
				h.lockVault()
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "vault rotation verification failed"})
				return
			}
			h.setVaultKey(key)
			for i := range key {
				key[i] = 0
			}
			h.clearVaultFailures()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exists": true, "unlocked": true, "version": record.Version})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown vault action (setup|unlock|lock|change_password)"})
		}
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
