package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultStorePath = "data/pglight.db"

var ErrStoreNotFound = errors.New("app store does not exist")

// ResolvePath returns the canonical absolute path used for both SQLite and
// the process lock. Keeping this in one place prevents a relative-path reset
// from targeting a different database than the running application.
func ResolvePath(path string) (string, error) {
	if path == "" {
		path = defaultStorePath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve app store path: %w", err)
	}
	return filepath.Clean(abs), nil
}

func ExistingPath(path string) (string, error) {
	abs, err := ResolvePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrStoreNotFound, abs)
	}
	if err != nil {
		return "", fmt.Errorf("inspect app store: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("app store path is a directory: %s", abs)
	}
	return abs, nil
}
