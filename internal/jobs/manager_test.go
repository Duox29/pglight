package jobs

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestJobCompletesWithProgressAndScopedLookup(t *testing.T) {
	m := New(t.TempDir(), 1)
	defer m.Close()
	started := make(chan struct{})
	snapshot, err := m.Start("session-a", "backup", func(ctx context.Context, progress *Progress) error {
		progress.AddRows(12)
		progress.AddBytes(4096)
		close(started)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	done, err := m.Wait(context.Background(), "session-a", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != Done || done.Rows != 12 || done.Bytes != 4096 || done.FinishedAt == nil {
		t.Fatalf("unexpected completed job snapshot: %+v", done)
	}
	if _, err := m.Get("session-b", snapshot.ID); err == nil {
		t.Fatal("job was visible to another session")
	}
}

func TestJobCancelReleasesSessionSlotAndRemovesTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	m := New(root, 1)
	defer m.Close()
	started := make(chan struct{})
	snapshot, err := m.Start("session-a", "restore", func(ctx context.Context, progress *Progress) error {
		file, createErr := progress.CreateTemp(".partial")
		if createErr != nil {
			return createErr
		}
		if _, createErr = file.WriteString("partial"); createErr != nil {
			return createErr
		}
		if createErr = file.Close(); createErr != nil {
			return createErr
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	if err := m.Cancel("session-a", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	done, err := m.Wait(context.Background(), "session-a", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != Cancelled {
		t.Fatalf("state=%s, want cancelled", done.State)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancel left temp files: %v", entries)
	}
	second, err := m.Start("session-a", "backup", func(context.Context, *Progress) error { return nil })
	if err != nil {
		t.Fatalf("cancel did not release session slot: %v", err)
	}
	if _, err := m.Wait(context.Background(), "session-a", second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestJobSessionLimitAndFailureState(t *testing.T) {
	m := New(t.TempDir(), 1)
	defer m.Close()
	started, release := make(chan struct{}), make(chan struct{})
	first, err := m.Start("session-a", "export", func(context.Context, *Progress) error { close(started); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := m.Start("session-a", "backup", func(context.Context, *Progress) error { return nil }); err == nil {
		t.Fatal("concurrent heavy job should be rejected")
	}
	failed, err := m.Start("session-b", "backup", func(context.Context, *Progress) error { return context.DeadlineExceeded })
	if err != nil {
		t.Fatal(err)
	}
	if state, err := m.Wait(context.Background(), "session-b", failed.ID); err != nil || state.State != Failed {
		t.Fatalf("job state=%+v err=%v", state, err)
	}
	close(release)
	if _, err := m.Wait(context.Background(), "session-a", first.ID); err != nil {
		t.Fatal(err)
	}
}
