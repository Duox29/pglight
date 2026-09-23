package jobs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const maxCommandErrorBytes = 8192

// RunCommand executes an external PostgreSQL utility without exposing its
// arguments or credential environment in errors. Stdout is discarded; callers
// should direct backup data to a file and use progress counters for size.
func RunCommand(ctx context.Context, executable string, args, env []string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append([]string(nil), env...)
	cmd.Stdout = io.Discard
	stderr := &boundedOutput{limit: maxCommandErrorBytes}
	cmd.Stderr = stderr
	cmd.WaitDelay = 3 * time.Second
	configureProcessTree(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	message := stderr.String()
	for _, line := range env {
		key, value, ok := strings.Cut(line, "=")
		if ok && value != "" && (strings.Contains(strings.ToUpper(key), "PASSWORD") || strings.Contains(strings.ToUpper(key), "SECRET")) {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	if stderr.truncated {
		message += "\n[stderr truncated]"
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("%s: %w", executableName(executable), err)
	}
	return fmt.Errorf("%s: %w: %s", executableName(executable), err, strings.TrimSpace(message))
}

func executableName(path string) string {
	if i := strings.LastIndexAny(path, `/\\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

type boundedOutput struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, err := b.Buffer.Write(p)
	return original, err
}
