package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestProcessLockExcludesSecondOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pglight.db")
	first, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := AcquireProcessLock(path)
	if !errors.Is(err, ErrStoreLocked) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second lock error = %v, want ErrStoreLocked", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer third.Close()
}

func TestExistingPathDoesNotCreateStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := ExistingPath(path); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing path error = %v, want ErrStoreNotFound", err)
	}
}
