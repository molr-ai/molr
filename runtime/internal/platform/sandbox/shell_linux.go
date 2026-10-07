//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const hostToolPath = "/usr/bin:/bin:/usr/sbin:/sbin:/molr/bin"

// A private external 'cat' returns a marker instead of reading /dev/null.
// Standalone BusyBox shells run their own cat and fail this PATH check.
const shellPathProbe = `test "$(PATH=/molr/probe command -v busybox)" = /molr/probe/busybox && test "$(PATH=/molr/probe cat /dev/null)" = molr-shell-path-ok`

func selectHostShell(env []string) (string, bool, error) {
	for _, name := range []string{"cat", "busybox"} {
		info, err := os.Stat("/molr/probe/" + name)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return "", false, fmt.Errorf("sandbox shell probe asset unavailable")
		}
	}
	for _, shell := range []string{"/bin/sh", "/bin/dash", "/bin/bash", "/molr/busybox"} {
		if probeHostShell(shell, shellPathProbe, env) {
			return shell, false, nil
		}
	}
	// Preserve Shell availability on hosts with no PATH-respecting shell.
	// This is a startup-only probe, never a retry of the user's command.
	if probeHostShell("/molr/busybox", `test "$((2 + 3))" = 5`, env) {
		return "/molr/busybox", true, nil
	}
	return "", false, fmt.Errorf("sandbox shell unavailable: neither a compatible host shell nor BusyBox could start")
}

func probeHostShell(shell, script string, env []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", script)
	cmd.Args[0] = "sh" // BusyBox dispatch; Bash uses POSIX sh startup rules.
	cmd.Env = env
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Run() == nil
}

func prepareBusyBoxFallback(ctx context.Context, root string) error {
	// Query only our freshly copied, validated static asset, never a program
	// from the user's PATH. The binary digest versions this command list too.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "bin/busybox"), "--list")
	cmd.Env = []string{}
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil || len(output) == 0 || len(output) > 32768 {
		return unavailable("sandbox_package_incomplete")
	}
	seen := make(map[string]bool)
	for _, name := range strings.Fields(string(output)) {
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || seen[name] {
			return unavailable("sandbox_package_incomplete")
		}
		seen[name] = true
		if err := os.Symlink("../busybox", filepath.Join(root, "molr/bin", name)); err != nil {
			return err
		}
	}
	if !seen["sh"] || !seen["cat"] {
		return unavailable("sandbox_package_incomplete")
	}
	// The probe's interpreter is fixed and its only command is a shell builtin.
	// It works even when there is no usable host /bin/sh or dynamic loader.
	if err := os.WriteFile(filepath.Join(root, "molr/probe/cat"), []byte("#!/molr/busybox sh\nprintf 'molr-shell-path-ok\\n'\n"), 0755); err != nil {
		return err
	}
	return os.Symlink("cat", filepath.Join(root, "molr/probe/busybox"))
}
