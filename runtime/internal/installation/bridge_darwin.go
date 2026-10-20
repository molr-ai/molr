package installation

import (
	"context"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Component workers need no launchd registration or system settings. The worker
// inherits the already-owned installation lock and survives Runtime restarts.
// Runtime upgrades retain the existing macOS CLI/Desktop update path.
func runnerAvailable(_ context.Context, update bool) bool { return !update }
func startMaintenanceWorker(_ context.Context, r *installrecord.Record, key string, args []string, lock *os.File) error {
	cmd := exec.Command(filepath.Join(r.Paths.Program, "bin/molr-setup"), append(args, "--lock-fd", "3")...)
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + os.Getenv("HOME")}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	// Use real files, not exec's copy pipes, so parent exit cannot SIGPIPE workers.
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
