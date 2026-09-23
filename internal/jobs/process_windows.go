//go:build windows

package jobs

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func configureProcessTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killer := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		killer.Env = safeSystemEnvironment()
		if err := killer.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

func safeSystemEnvironment() []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if upper == "PATH" || upper == "SYSTEMROOT" || upper == "WINDIR" || upper == "TEMP" || upper == "TMP" {
			env = append(env, item)
		}
	}
	return env
}
