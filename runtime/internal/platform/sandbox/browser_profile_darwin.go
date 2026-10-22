package sandbox

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (r *Runner) browserProfile(e core.Execution, c platform.Command, work, temp string, port int) (string, error) {
	if c.FullAccess {
		return "", nil
	}
	read, err := r.nativeTools()
	if err != nil {
		return "", err
	}
	write := []string{work, temp}
	global := platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID)
	if _, statErr := os.Stat(global); statErr == nil {
		real, err := filepath.EvalSymlinks(global)
		if err != nil {
			return "", err
		}
		read = append(read, real)
	}
	for _, m := range c.Mounts {
		source := m.Source
		if m.Destination != "/resources/browser-agent" && m.Destination != "/resources/browser-chromium" {
			source, err = r.nativeExternal(source)
			if err != nil {
				return "", err
			}
		}
		if m.ReadOnly {
			read = append(read, source)
		} else {
			write = append(write, source)
		}
	}
	return seatbeltBrowserProfile(read, write, temp, c.NetworkAccess, []int{port, port + 1})
}

// Never trust a successfully connected port alone: another Runtime or local
// program might have won the bind race. Both listeners must belong to our child.
func browserOwnsPort(ctx context.Context, pid, port int) bool {
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp")
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "p") {
			if line != "p"+strconv.Itoa(pid) {
				return false
			}
			found = true
		}
	}
	return found
}

// JXA provides the system C bridge without adding a CGO dependency to Runtime.
// sandbox_check(pid, NULL, 0) reports whether the actual renderer has Seatbelt.
func browserRendererSandboxed(ctx context.Context, pid int) bool {
	const script = `ObjC.import('Foundation');
ObjC.bindFunction('dlopen', ['void *', ['char *', 'int']]);
$.dlopen('/usr/lib/libsandbox.1.dylib', 2);
ObjC.bindFunction('sandbox_check', ['int', ['int', 'void *', 'int']]);
function run(argv) { return String($.sandbox_check(Number(argv[0]), null, 0)); }`
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script, strconv.Itoa(pid))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

func browserAppArmorAvailable(context.Context) bool { return false }
