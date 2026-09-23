package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pglight/internal/backup"
	"pglight/internal/jobs"
)

type backupRequest struct {
	SessionID  string `json:"session_id"`
	Format     string `json:"format"`
	SchemaOnly bool   `json:"schema_only"`
	DataOnly   bool   `json:"data_only"`
}

const maxBackupUploadBytes int64 = 2 << 30

func (h *Handler) Backup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if h.Mgr == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	var req backupRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	sid := sessionFromBody(req.SessionID, r)
	envMap, ok := h.Mgr.ProcessEnv(sid)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	if h.Jobs == nil {
		writeJSON(w, 503, map[string]string{"error": "job service unavailable"})
		return
	}
	if req.Format == "" {
		req.Format = "custom"
	}
	if req.Format != "custom" && req.Format != "plain" {
		writeJSON(w, 400, map[string]string{"error": "format must be custom or plain"})
		return
	}
	if req.SchemaOnly && req.DataOnly {
		writeJSON(w, 400, map[string]string{"error": "schema_only and data_only cannot both be selected"})
		return
	}
	tool, err := backup.DiscoverTool("pg_dump")
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": err.Error()})
		return
	}
	meta, _ := h.Mgr.Info(sid)
	args := []string{"--no-password", "--format=" + req.Format}
	if req.Format == "plain" {
		args = append(args, "--clean", "--if-exists")
	}
	if req.SchemaOnly {
		args = append(args, "--schema-only")
	}
	if req.DataOnly {
		args = append(args, "--data-only")
	}
	name := safeBackupName(meta.DbName, req.Format)
	snapshot, err := h.Jobs.Start(sid, "backup", func(ctx context.Context, progress *jobs.Progress) error {
		f, err := progress.CreateTemp("." + req.Format)
		if err != nil {
			return err
		}
		path := f.Name()
		if err := f.Close(); err != nil {
			return err
		}
		if err := progress.SetArtifact(path, name); err != nil {
			return err
		}
		cmdArgs := append(append([]string(nil), args...), "--file", path)
		stopProgress := make(chan struct{})
		go func() {
			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopProgress:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if st, statErr := os.Stat(path); statErr == nil {
						progress.SetBytes(st.Size())
					}
				}
			}
		}()
		err = jobs.RunCommand(ctx, tool, cmdArgs, envLines(envMap))
		close(stopProgress)
		if err != nil {
			return err
		}
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		progress.SetBytes(st.Size())
		return nil
	})
	if err != nil {
		writeJSON(w, 429, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, snapshot)
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if h.Mgr == nil {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid or oversized restore upload"})
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	sid := r.FormValue("session_id")
	if sid == "" {
		sid = sessionID(r)
	}
	envMap, ok := h.Mgr.ProcessEnv(sid)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "not connected"})
		return
	}
	if h.Jobs == nil {
		writeJSON(w, 503, map[string]string{"error": "job service unavailable"})
		return
	}
	if r.FormValue("overwrite") != "true" {
		writeJSON(w, 400, map[string]string{"error": "explicit overwrite confirmation is required"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "backup file is required"})
		return
	}
	defer file.Close()
	toolName, args, err := restoreCommand(header.Filename)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	tool, err := backup.DiscoverTool(toolName)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": err.Error()})
		return
	}
	staged, err := os.CreateTemp("", "pglight-restore-*")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "could not stage restore file"})
		return
	}
	path := staged.Name()
	n, copyErr := io.Copy(staged, io.LimitReader(file, maxBackupUploadBytes+1))
	closeErr := staged.Close()
	if copyErr != nil || closeErr != nil || n > maxBackupUploadBytes {
		_ = os.Remove(path)
		writeJSON(w, 400, map[string]string{"error": "could not stage restore upload within size limit"})
		return
	}
	snapshot, err := h.Jobs.Start(sid, "restore", func(ctx context.Context, progress *jobs.Progress) error {
		defer os.Remove(path)
		progress.SetBytes(n)
		cmdArgs := append(append([]string(nil), args...), path)
		return jobs.RunCommand(ctx, tool, cmdArgs, envLines(envMap))
	})
	if err != nil {
		_ = os.Remove(path)
		writeJSON(w, 429, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, snapshot)
}

func restoreCommand(filename string) (string, []string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".dump", ".backup", ".custom":
		return "pg_restore", []string{"--clean", "--if-exists", "--exit-on-error", "--no-password"}, nil
	case ".sql":
		return "psql", []string{"--no-password", "-v", "ON_ERROR_STOP=1"}, nil
	default:
		return "", nil, fmt.Errorf("restore file must use .dump, .backup, .custom or .sql")
	}
}

func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	if h.Jobs == nil {
		writeJSON(w, 503, map[string]string{"error": "job service unavailable"})
		return
	}
	sid := sessionID(r)
	if sid == "" {
		writeJSON(w, 401, map[string]string{"error": "session_id required"})
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, 404, map[string]string{"error": "job not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		snapshot, err := h.Jobs.Get(sid, id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, snapshot)
	case http.MethodDelete:
		if err := h.Jobs.Cancel(sid, id); err != nil {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodPost:
		if r.URL.Query().Get("action") != "download" {
			writeJSON(w, 400, map[string]string{"error": "unsupported job action"})
			return
		}
		f, name, err := h.Jobs.OpenArtifact(sid, id)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		defer f.Close()
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, err := f.WriteTo(w); err != nil {
			return
		}
		_ = h.Jobs.RemoveTempFiles(sid, id)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func safeBackupName(database, format string) string {
	var b strings.Builder
	for _, r := range database {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		b.WriteString("database")
	}
	ext := ".dump"
	if format == "plain" {
		ext = ".sql"
	}
	return b.String() + "-" + time.Now().Format("20060102-150405") + ext
}

func envLines(values map[string]string) []string {
	lines := make([]string, 0, len(values))
	for key, value := range values {
		lines = append(lines, key+"="+value)
	}
	return lines
}
