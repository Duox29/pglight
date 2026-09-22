package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pglight/internal/db"
	"pglight/internal/store"
)

type connectionRequest struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Host            string   `json:"host"`
	User            string   `json:"user"`
	DBName          string   `json:"dbname"`
	SSLMode         string   `json:"sslmode"`
	Password        string   `json:"password"`
	SavePassword    bool     `json:"save_password"`
	ClearPassword   bool     `json:"clear_password"`
	DuplicateFrom   string   `json:"duplicate_from"`
	Port            int      `json:"port"`
	ProfileID       string   `json:"profile_id"`
	FolderID        string   `json:"folder_id"`
	Environment     string   `json:"environment"`
	Color           string   `json:"color"`
	Description     string   `json:"description"`
	Favorite        bool     `json:"favorite"`
	Default         bool     `json:"default"`
	Tags            []string `json:"tags"`
	ConnectTimeout  int      `json:"connect_timeout"`
	Keepalive       int      `json:"keepalive"`
	ApplicationName string   `json:"application_name"`
	SearchPath      string   `json:"search_path"`
	SSLRootCert     string   `json:"sslrootcert"`
	SSLCert         string   `json:"sslcert"`
	SSLKey          string   `json:"sslkey"`
	UnixSocket      string   `json:"unix_socket"`
}

type profileTransfer struct {
	Name        string                  `json:"name"`
	Host        string                  `json:"host"`
	Port        int                     `json:"port"`
	User        string                  `json:"user"`
	DBName      string                  `json:"dbname"`
	SSLMode     string                  `json:"sslmode"`
	Password    string                  `json:"password,omitempty"`
	FolderID    string                  `json:"folder_id,omitempty"`
	Environment string                  `json:"environment,omitempty"`
	Color       string                  `json:"color,omitempty"`
	Description string                  `json:"description,omitempty"`
	Favorite    bool                    `json:"favorite,omitempty"`
	Default     bool                    `json:"default,omitempty"`
	Tags        []string                `json:"tags,omitempty"`
	Options     store.ConnectionOptions `json:"options,omitempty"`
}

type profileTransferFile struct {
	Format      string            `json:"format"`
	Version     int               `json:"version"`
	Connections []profileTransfer `json:"connections"`
}

func connectionJSON(x store.ConnectionProfile) map[string]any {
	out := map[string]any{
		"id": x.ID, "name": x.Name, "host": x.Host, "port": x.Port,
		"user": x.Username, "dbname": x.DBName, "sslmode": x.SSLMode,
		"has_password": x.HasPassword, "last_used_at": x.LastUsedAt,
	}
	if x.FolderID != "" || x.Environment != "" || x.Color != "" || x.Description != "" || x.Favorite || x.Default || len(x.Tags) > 0 {
		out["folder_id"], out["environment"], out["color"], out["description"] = x.FolderID, x.Environment, x.Color, x.Description
		out["favorite"], out["default"], out["tags"] = x.Favorite, x.Default, x.Tags
	}
	if x.Options.ConnectTimeout != 0 || x.Options.Keepalive != 0 || x.Options.ApplicationName != "" || x.Options.SearchPath != "" || x.Options.SSLRootCert != "" || x.Options.SSLCert != "" || x.Options.SSLKey != "" || x.Options.UnixSocket != "" {
		out["options"] = x.Options
	}
	return out
}

func (h *Handler) Connections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		xs, err := h.Store.ListConnections(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		folder := strings.TrimSpace(r.URL.Query().Get("folder_id"))
		wantTag := strings.TrimSpace(r.URL.Query().Get("tag"))
		favoriteOnly := r.URL.Query().Get("favorite") == "1" || strings.EqualFold(r.URL.Query().Get("favorite"), "true")
		out := make([]map[string]any, 0, len(xs))
		for _, x := range xs {
			if folder != "" && x.FolderID != folder {
				continue
			}
			if favoriteOnly && !x.Favorite {
				continue
			}
			if wantTag != "" {
				hit := false
				for _, tag := range x.Tags {
					if strings.EqualFold(tag, wantTag) {
						hit = true
						break
					}
				}
				if !hit {
					continue
				}
			}
			if filter != "" {
				search := strings.ToLower(strings.Join([]string{x.Name, x.Host, x.Username, x.DBName, x.Environment, x.Description, strings.Join(x.Tags, " ")}, " "))
				if !strings.Contains(search, filter) {
					continue
				}
			}
			out = append(out, connectionJSON(x))
		}
		folders, folderErr := h.Store.ListConnectionFolders(r.Context(), h.UserID)
		if folderErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": folderErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": out, "folders": folders, "vault_unlocked": h.vaultUnlocked()})
	case http.MethodPost:
		var req connectionRequest
		if err := decodeBody(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Host = strings.TrimSpace(req.Host)
		req.User = strings.TrimSpace(req.User)
		req.DBName = strings.TrimSpace(req.DBName)
		req.SSLMode = db.NormalizeSSLMode(req.SSLMode)
		if req.Name == "" || req.Host == "" || req.User == "" || req.DBName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name,host,user,dbname are required"})
			return
		}
		if req.Port <= 0 {
			req.Port = 5432
		}
		if req.SavePassword && !h.vaultUnlocked() {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "vault is locked; unlock it before saving a password"})
			return
		}
		var key []byte
		if req.SavePassword {
			key = h.copyVaultKey()
			if len(key) == 0 {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "vault is locked; unlock it before saving a password"})
				return
			}
			defer func() {
				for i := range key {
					key[i] = 0
				}
			}()
		}
		x, err := h.Store.SaveConnectionProfile(r.Context(), h.UserID, store.ConnectionSave{
			ID: strings.TrimSpace(req.ID), Name: req.Name, Host: req.Host, Port: req.Port, User: req.User, DBName: req.DBName, SSLMode: req.SSLMode,
			Password: req.Password, SavePassword: req.SavePassword, ClearPassword: req.ClearPassword, DuplicateFrom: strings.TrimSpace(req.DuplicateFrom),
			FolderID: req.FolderID, Environment: req.Environment, Color: req.Color, Description: req.Description, Favorite: req.Favorite, Default: req.Default, Tags: req.Tags,
			Options: store.ConnectionOptions{ConnectTimeout: req.ConnectTimeout, Keepalive: req.Keepalive, ApplicationName: req.ApplicationName, SearchPath: req.SearchPath, SSLRootCert: req.SSLRootCert, SSLCert: req.SSLCert, SSLKey: req.SSLKey, UnixSocket: req.UnixSocket},
		}, key)
		if err != nil {
			code := http.StatusInternalServerError
			if strings.Contains(err.Error(), "invalid connection timeout") || strings.Contains(err.Error(), "folder") || strings.Contains(err.Error(), "tag") {
				code = http.StatusBadRequest
			}
			if strings.Contains(err.Error(), "vault is locked") {
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connection": connectionJSON(x)})
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		ok, err := h.Store.DeleteConnectionByID(r.Context(), h.UserID, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "connection not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *Handler) ConnectionFolders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folders, err := h.Store.ListConnectionFolders(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
	case http.MethodPost:
		var req struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			ParentID string `json:"parent_id"`
		}
		if err := decodeBody(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		folder, err := h.Store.UpsertConnectionFolder(r.Context(), h.UserID, strings.TrimSpace(req.ID), req.Name, strings.TrimSpace(req.ParentID))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"folder": folder})
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		ok, err := h.Store.DeleteConnectionFolder(r.Context(), h.UserID, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "folder not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// ConnectionExport returns a portable profile file. GET is deliberately
// password-free. POST may include passwords only inside an encrypted JSON
// envelope protected by an export password.
func (h *Handler) ConnectionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	encrypted := false
	exportPassword := ""
	if r.Method == http.MethodPost {
		var req struct {
			Encrypted bool   `json:"encrypted"`
			Password  string `json:"password"`
		}
		if err := decodeBody(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		encrypted, exportPassword = req.Encrypted, req.Password
	}
	if encrypted && validMasterPassword(exportPassword) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "encrypted export requires a master password of at least 8 characters"})
		return
	}
	profiles, err := h.Store.ListConnections(r.Context(), h.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	file := profileTransferFile{Format: "pglight-connection-profiles", Version: 1, Connections: make([]profileTransfer, 0, len(profiles))}
	for _, x := range profiles {
		item := profileTransfer{Name: x.Name, Host: x.Host, Port: x.Port, User: x.Username, DBName: x.DBName, SSLMode: x.SSLMode, FolderID: x.FolderID, Environment: x.Environment, Color: x.Color, Description: x.Description, Favorite: x.Favorite, Default: x.Default, Tags: x.Tags, Options: x.Options}
		if encrypted && x.HasPassword {
			key := h.copyVaultKey()
			if len(key) == 0 {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "vault is locked; unlock it before encrypted export"})
				return
			}
			item.Password, _, err = h.Store.ConnectionSecret(r.Context(), h.UserID, x.ID, key)
			for i := range key {
				key[i] = 0
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		file.Connections = append(file.Connections, item)
	}
	payload, err := json.Marshal(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if encrypted {
		payload, err = store.EncryptJSON(exportPassword, payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="pglight-connections.json"`)
	writeJSON(w, http.StatusOK, json.RawMessage(payload))
}

func (h *Handler) ConnectionImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Payload  json.RawMessage `json:"payload"`
		Password string          `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil || len(req.Payload) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload is required"})
		return
	}
	plain := req.Payload
	var envelope store.EncryptedJSON
	if json.Unmarshal(plain, &envelope) == nil && envelope.Format == "pglight-encrypted-json" {
		if validMasterPassword(req.Password) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "encrypted import requires a password"})
			return
		}
		var err error
		plain, err = store.DecryptJSON(req.Password, plain)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid encrypted profile file or password"})
			return
		}
	}
	var file profileTransferFile
	if err := json.Unmarshal(plain, &file); err != nil || file.Format != "pglight-connection-profiles" || file.Version != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile export"})
		return
	}
	items := make([]store.ConnectionImportItem, 0, len(file.Connections))
	needsVault := false
	for _, item := range file.Connections {
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Host) == "" || strings.TrimSpace(item.User) == "" || strings.TrimSpace(item.DBName) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "profile name,host,user,dbname are required"})
			return
		}
		if item.Port <= 0 {
			item.Port = 5432
		}
		if item.Password != "" {
			needsVault = true
		}
		items = append(items, store.ConnectionImportItem{
			Name: item.Name, Host: item.Host, Port: item.Port, User: item.User, DBName: item.DBName,
			SSLMode: db.NormalizeSSLMode(item.SSLMode), Password: item.Password, FolderID: item.FolderID,
			Environment: item.Environment, Color: item.Color, Description: item.Description,
			Favorite: item.Favorite, Default: item.Default, Tags: item.Tags, Options: item.Options,
		})
	}
	var key []byte
	if needsVault {
		key = h.copyVaultKey()
		if len(key) == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "vault is locked; unlock it before importing passwords"})
			return
		}
		defer func() {
			for i := range key {
				key[i] = 0
			}
		}()
	}
	imported, err := h.Store.ImportConnectionProfiles(r.Context(), h.UserID, items, key)
	if err != nil {
		if strings.Contains(err.Error(), "vault is locked") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		} else if strings.Contains(err.Error(), "invalid connection timeout") || strings.Contains(err.Error(), "folder") || strings.Contains(err.Error(), "tag") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": imported})
}

func (h *Handler) connectionTarget(ctx context.Context, profileID, suppliedPassword string) (store.ConnectionProfile, string, error) {
	x, err := h.Store.GetConnection(ctx, h.UserID, profileID)
	if err != nil {
		return store.ConnectionProfile{}, "", err
	}
	if suppliedPassword != "" {
		return x, suppliedPassword, nil
	}
	key := h.copyVaultKey()
	if len(key) == 0 {
		if x.HasPassword {
			return store.ConnectionProfile{}, "", errors.New("vault is locked; unlock it before connecting with this profile")
		}
		return store.ConnectionProfile{}, "", errors.New("no password saved for this profile; enter it manually")
	}
	password, ok, err := h.Store.ConnectionSecret(ctx, h.UserID, x.ID, key)
	for i := range key {
		key[i] = 0
	}
	if err != nil {
		return store.ConnectionProfile{}, "", err
	}
	if !ok {
		return store.ConnectionProfile{}, "", errors.New("no password saved for this profile; enter it manually")
	}
	return x, password, nil
}

func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req connectionRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	var profile store.ConnectionProfile
	var password string
	var opts db.ConnOptions
	var err error
	if strings.TrimSpace(req.ProfileID) != "" {
		profile, password, err = h.connectionTarget(r.Context(), strings.TrimSpace(req.ProfileID), req.Password)
		opts = db.ConnOptions{ConnectTimeout: profile.Options.ConnectTimeout, Keepalive: profile.Options.Keepalive, ApplicationName: profile.Options.ApplicationName, SearchPath: profile.Options.SearchPath, SSLRootCert: profile.Options.SSLRootCert, SSLCert: profile.Options.SSLCert, SSLKey: profile.Options.SSLKey, UnixSocket: profile.Options.UnixSocket}
	} else {
		profile = store.ConnectionProfile{Host: strings.TrimSpace(req.Host), Port: req.Port, Username: strings.TrimSpace(req.User), DBName: strings.TrimSpace(req.DBName), SSLMode: db.NormalizeSSLMode(req.SSLMode)}
		opts = db.ConnOptions{ConnectTimeout: req.ConnectTimeout, Keepalive: req.Keepalive, ApplicationName: req.ApplicationName, SearchPath: req.SearchPath, SSLRootCert: req.SSLRootCert, SSLCert: req.SSLCert, SSLKey: req.SSLKey, UnixSocket: req.UnixSocket}
		password = req.Password
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if profile.Port <= 0 {
		profile.Port = 5432
	}
	if profile.Host == "" || profile.Username == "" || profile.DBName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host,user,dbname are required"})
		return
	}
	id := "test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	started := time.Now()
	if err := h.Mgr.AddWithOptions(id, db.ConnStringWithOptions(profile.Host, profile.Port, profile.Username, password, profile.DBName, profile.SSLMode, opts), opts); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	var database, version string
	if q, ok := h.Mgr.Q(id); ok {
		if err := q.QueryRow(r.Context(), `SELECT current_database(), current_setting('server_version')`).Scan(&database, &version); err != nil {
			h.Mgr.Close(id)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	h.Mgr.Close(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("connected to %s", profile.DBName), "database": database, "version": version, "latency_ms": time.Since(started).Milliseconds()})
}

func (h *Handler) profileUsed(ctx context.Context, profileID string) {
	if profileID != "" {
		_ = h.Store.MarkConnectionUsed(ctx, h.UserID, profileID)
	}
}
