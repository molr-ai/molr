package installation

import (
	"context"
	"io"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitName(r *installrecord.Record, id string) string {
	return "molr-maintenance-" + r.Name + "-" + id + ".service"
}
func unitActive(ctx context.Context, r *installrecord.Record, id string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show", unitName(r, id), "--property=ActiveState", "--value")
	b, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	return s == "active" || s == "activating" || s == "deactivating"
}
func runnerAvailable(ctx context.Context, update bool) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show-environment")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func startMaintenanceWorker(ctx context.Context, r *installrecord.Record, key string, workerArgs []string, lock *os.File) error {
	args := []string{"--user", "--quiet", "--collect", "--unit=" + unitName(r, key), "--property=Type=exec", "--property=UMask=0077", "--property=RuntimeMaxSec=1800", "--expand-environment=no", filepath.Join(r.Paths.Program, "bin/molr-setup")}
	// Forward only administrator-provided networking settings. Values are inherited
	// by systemd-run, never put into the command line or HTTP task receipts.
	var environment []string
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if os.Getenv(name) != "" {
			environment = append(environment, "--setenv="+name)
		}
	}
	args = append(environment, args...)

	args = append(args, workerArgs...)
	cmd := exec.CommandContext(ctx, "systemd-run", args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run()
}
