package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"pglight/internal/jobs"
)

func TestJobEndpointScopesSnapshotsAndDownloadsToSession(t *testing.T) {
	dir := t.TempDir()
	m := jobs.New(dir, 1)
	defer m.Close()
	snapshot, err := m.Start("session-a", "backup", func(_ context.Context, p *jobs.Progress) error {
		f, err := p.CreateTemp(".dump")
		if err != nil {
			return err
		}
		if _, err = f.WriteString("archive"); err != nil {
			return err
		}
		path := f.Name()
		if err = f.Close(); err != nil {
			return err
		}
		return p.SetArtifact(path, "sample.dump")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Wait(context.Background(), "session-a", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Jobs: m}
	wrong := httptest.NewRequest("GET", "/api/jobs/"+snapshot.ID+"?session_id=session-b", nil)
	wrongRec := httptest.NewRecorder()
	h.Job(wrongRec, wrong)
	if wrongRec.Code != 404 {
		t.Fatalf("wrong-session status=%d body=%s", wrongRec.Code, wrongRec.Body)
	}
	request := httptest.NewRequest("POST", "/api/jobs/"+snapshot.ID+"?session_id=session-a&action=download", nil)
	recorder := httptest.NewRecorder()
	h.Job(recorder, request)
	if recorder.Code != 200 || recorder.Body.String() != "archive" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("missing attachment filename")
	}
}

func TestBackupRejectsMissingSession(t *testing.T) {
	h := &Handler{Jobs: jobs.New(t.TempDir(), 1)}
	recorder := httptest.NewRecorder()
	h.Backup(recorder, httptest.NewRequest("POST", "/api/backup", strings.NewReader(`{"format":"custom"}`)))
	if recorder.Code != 401 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
}

func TestRestoreCommandChoosesSafeUtilityFlags(t *testing.T) {
	tool, args, err := restoreCommand("daily.DUMP")
	if err != nil || tool != "pg_restore" {
		t.Fatalf("tool=%q err=%v", tool, err)
	}
	for _, required := range []string{"--clean", "--if-exists", "--exit-on-error", "--no-password"} {
		found := false
		for _, arg := range args {
			if arg == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("custom restore missing %s in %v", required, args)
		}
	}
	tool, args, err = restoreCommand("dump.sql")
	if err != nil || tool != "psql" {
		t.Fatalf("tool=%q err=%v", tool, err)
	}
	if len(args) < 3 || args[1] != "-v" || args[2] != "ON_ERROR_STOP=1" {
		t.Fatalf("plain restore flags=%v", args)
	}
	if _, _, err := restoreCommand("dump.tar.gz"); err == nil {
		t.Fatal("unknown format should be rejected")
	}
}
