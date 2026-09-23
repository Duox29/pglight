package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandChildHelper(t *testing.T) {
	switch os.Getenv("PGLIGHT_COMMAND_HELPER") {
	case "failure":
		fmt.Fprintln(os.Stderr, "connection failed password=secret-child-value")
		os.Exit(17)
	case "wait":
		if err := os.WriteFile(os.Getenv("PGLIGHT_COMMAND_READY"), []byte("ready"), 0600); err != nil {
			os.Exit(18)
		}
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}

func TestRunCommandRedactsPasswordsFromFailure(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestCommandChildHelper$", "--"}
	err = RunCommand(context.Background(), binary, args, []string{"PGLIGHT_COMMAND_HELPER=failure", "PGPASSWORD=secret-child-value"})
	if err == nil {
		t.Fatal("expected command failure")
	}
	if strings.Contains(err.Error(), "secret-child-value") {
		t.Fatalf("password leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected sanitized stderr in error: %v", err)
	}
}

func TestRunCommandCancellationStopsProcess(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunCommand(ctx, binary, []string{"-test.run=^TestCommandChildHelper$", "--"}, []string{"PGLIGHT_COMMAND_HELPER=wait", "PGLIGHT_COMMAND_READY=" + ready})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled process unexpectedly succeeded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("process did not stop promptly after cancellation")
	}
}
