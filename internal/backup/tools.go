package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func DiscoverTool(name string) (string, error) {
	key := "PGLIGHT_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	return resolveTool(name, os.Getenv(key), exec.LookPath, filepath.Glob)
}

func resolveTool(name, configured string, lookPath func(string) (string, error), glob func(string) ([]string, error)) (string, error) {
	if configured != "" {
		path, err := lookPath(configured)
		if err != nil {
			return "", fmt.Errorf("configured %s at %q is unavailable: %w", name, configured, err)
		}
		return path, nil
	}
	if path, err := lookPath(name); err == nil {
		return path, nil
	}
	for _, pattern := range installationPatterns(name) {
		matches, err := glob(pattern)
		if err != nil {
			continue
		}
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, candidate := range matches {
			if path, err := lookPath(candidate); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%s was not found on PATH or in standard PostgreSQL installation directories; set %s", name, "PGLIGHT_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
}

func installationPatterns(name string) []string {
	if runtime.GOOS == "windows" {
		binary := name + ".exe"
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		patterns := []string{filepath.Join(programFiles, "PostgreSQL", "*", "bin", binary), filepath.Join(`C:\Program Files (x86)`, "PostgreSQL", "*", "bin", binary)}
		return patterns
	}
	if runtime.GOOS == "darwin" {
		return []string{filepath.Join("/Library/PostgreSQL", "*", "bin", name), filepath.Join("/usr/local", "opt", "postgresql@*", "bin", name)}
	}
	return []string{filepath.Join("/usr/lib/postgresql", "*", "bin", name), filepath.Join("/usr/pgsql-*", "bin", name), filepath.Join("/usr/local/pgsql", "bin", name)}
}
