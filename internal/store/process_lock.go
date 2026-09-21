package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrStoreLocked = errors.New("app store is locked by another pglight process")

type processLock struct {
	file *os.File
}

// AcquireProcessLock takes an OS advisory lock next to the SQLite database.
// The lock file is intentionally retained after release: deleting it creates
// a race where a new process can lock a replacement inode while another
// process still holds the old one.
func AcquireProcessLock(storePath string) (io.Closer, error) {
	path, err := ResolvePath(storePath)
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create app store directory: %w", err)
		}
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open app store lock: %w", err)
	}
	if err := lockProcessFile(file); err != nil {
		_ = file.Close()
		if isProcessLockBusy(err) {
			return nil, ErrStoreLocked
		}
		return nil, fmt.Errorf("lock app store: %w", err)
	}
	return &processLock{file: file}, nil
}

func (l *processLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockProcessFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
