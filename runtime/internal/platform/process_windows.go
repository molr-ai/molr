//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func configureProcess(cmd *exec.Cmd) {}
func terminateProcess(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	tool := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	_ = exec.Command(tool, "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
	_ = cmd.Process.Kill()
}
