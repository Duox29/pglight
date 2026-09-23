package backup

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveToolPrefersConfiguredExecutable(t *testing.T) {
	lookups := []string{}
	tool, err := resolveTool("pg_dump", "/custom/pg_dump", func(path string) (string, error) {
		lookups = append(lookups, path)
		return filepath.Clean(path), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool != filepath.Clean("/custom/pg_dump") {
		t.Fatalf("tool=%q", tool)
	}
	if len(lookups) != 1 || lookups[0] != "/custom/pg_dump" {
		t.Fatalf("lookups=%v", lookups)
	}
}

func TestResolveToolFallsBackToPathAndReportsMissingTool(t *testing.T) {
	tool, err := resolveTool("pg_restore", "", func(path string) (string, error) {
		if path == "pg_restore" {
			return "/usr/bin/pg_restore", nil
		}
		return "", errors.New("not found")
	}, nil)
	if err != nil || tool != "/usr/bin/pg_restore" {
		t.Fatalf("tool=%q err=%v", tool, err)
	}
	if _, err := resolveTool("psql", "", func(string) (string, error) { return "", errors.New("not found") }, func(string) ([]string, error) { return nil, nil }); err == nil {
		t.Fatal("missing utility should return an actionable error")
	}
}

func TestResolveToolSearchesPostgreSQLInstallationDirectories(t *testing.T) {
	tool, err := resolveTool("pg_dump", "", func(path string) (string, error) {
		if path == "/opt/postgresql/17/bin/pg_dump" {
			return path, nil
		}
		return "", errors.New("not executable")
	}, func(pattern string) ([]string, error) {
		return []string{"/opt/postgresql/16/bin/pg_dump", "/opt/postgresql/17/bin/pg_dump"}, nil
	})
	if err != nil || tool != "/opt/postgresql/17/bin/pg_dump" {
		t.Fatalf("tool=%q err=%v", tool, err)
	}
}
