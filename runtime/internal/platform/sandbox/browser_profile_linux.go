//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (r *Runner) browserProfile(_ core.Execution, c platform.Command, _, _ string, _ int) (string, error) {
	if !c.FullAccess {
		return "", core.Invalid("browser", "native Linux browser requires Full Access")
	}
	return "", nil
}

// Check the kernel socket inode against the child, before sending control data.
func browserOwnsPort(ctx context.Context, pid, port int) bool {
	if ctx.Err() != nil {
		return false
	}
	files, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return false
	}
	owned := map[string]bool{}
	for _, file := range files {
		target, _ := os.Readlink(filepath.Join(fmt.Sprintf("/proc/%d/fd", pid), file.Name()))
		owned[target] = true
	}
	found := false
	for _, table := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile("/proc/net/" + table)
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			address := strings.Split(fields[1], ":")
			if len(address) != 2 {
				continue
			}
			p, _ := strconv.ParseInt(address[1], 16, 32)
			if int(p) != port {
				continue
			}
			if !owned["socket:["+fields[9]+"]"] {
				return false
			}
			found = true
		}
	}
	return found
}

func browserRendererSandboxed(ctx context.Context, pid int) bool {
	if ctx.Err() != nil {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return false
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	child, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		return false
	}
	parent, err := os.Readlink("/proc/self/ns/pid")
	return err == nil && child != parent && fields["Seccomp"] == "2" && fields["NoNewPrivs"] == "1"
}

// Reuse the distribution's already-loaded Chrome profile when available. This
// neither installs policy nor disables Chromium's own namespace sandbox.
func browserAppArmorAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "/usr/bin/aa-exec", "-p", "chrome", "--", "/usr/bin/true")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	return cmd.Run() == nil
}
