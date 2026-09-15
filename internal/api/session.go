package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pglight/internal/db"
)

type connectReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DbName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
	Session  string `json:"session_id"`
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req connectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := req.Session
	if id != "" && h.Mgr.Alive(id) {
		// Reload/resume path: same browser session, healthy pool — reuse it
		// instead of leaking a new pool per refresh.
		meta, _ := h.Mgr.Info(id)
		writeJSON(w, 200, map[string]any{"session_id": id, "info": sessionInfo(id, meta, h.Mgr.InTxn(id))})
		return
	}
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	cs := db.ConnString(req.Host, req.Port, req.User, req.Password, req.DbName, req.SSLMode)
	if err := h.Mgr.Add(id, cs); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.Mgr.SetMeta(id, db.ConnMeta{Host: req.Host, Port: req.Port, User: req.User, DbName: req.DbName, SSLMode: req.SSLMode})
	meta, _ := h.Mgr.Info(id)
	writeJSON(w, 200, map[string]any{"session_id": id, "info": sessionInfo(id, meta, false)})
}

func sessionInfo(id string, meta db.ConnMeta, inTxn bool) map[string]any {
	dbname := meta.DbName
	if dbname == "" {
		dbname = "postgres"
	}
	return map[string]any{
		"id": id, "host": meta.Host, "port": meta.Port, "user": meta.User,
		"dbname": dbname, "sslmode": meta.SSLMode, "in_txn": inTxn,
		"connected_at": meta.ConnectedAt.Format(time.RFC3339),
	}
}

// Sessions lists all live backend sessions (display info only, no passwords).
func (h *Handler) Sessions(w http.ResponseWriter, r *http.Request) {
	list := h.Mgr.List()
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, sessionInfo(s.ID, s.ConnMeta, s.InTxn))
	}
	writeJSON(w, 200, map[string]any{"sessions": out})
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	h.Mgr.Close(sid)
	globalComplete.Invalidate(sid)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// --- transactions (DataGrip-style explicit txn) ---

func (h *Handler) Txn(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Session string `json:"session_id"`
		Action  string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id := sessionFromBody(req.Session, r)
	if id == "" {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	action := strings.ToLower(strings.TrimSpace(req.Action))
	var err error
	switch action {
	case "begin":
		err = h.Mgr.Begin(ctx, id)
	case "commit":
		err = h.Mgr.Commit(ctx, id)
	case "rollback":
		err = h.Mgr.Rollback(ctx, id)
	case "status":
		// no-op, just report
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action (begin|commit|rollback|status)"})
		return
	}
	if h.Log != nil && action != "status" {
		h.Log.LogTxn(action, err)
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "in_txn": h.Mgr.InTxn(id)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "in_txn": h.Mgr.InTxn(id)})
}
