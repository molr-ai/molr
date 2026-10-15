package sandbox

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (r *Runner) check() error {
	if !r.prepared {
		return unavailable("sandbox_not_prepared")
	}
	if r.prepareErr != nil {
		return r.prepareErr
	}
	return nil
}
func (r *Runner) recover() error { return nil }
func HelperMain([]string) int    { return 125 }
func SupervisorMain() int        { return 125 }

// Native paths are canonicalized before becoming policy data. A tool directory or
// external resource must never grant a parent of Runtime private data or workspaces.
func (r *Runner) nativeExternal(p string) (string, error) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if real != filepath.Clean(textfiles.SystemPath(p)) {
		return "", core.Invalid("sandbox", "native scopes must not contain symbolic links")
	}
	if _, err = seatbeltPath(real); err != nil {
		return "", err
	}
	for _, protected := range append([]string{r.options.PrivateRoot, r.options.WorkRoot}, r.options.ProtectedRoots...) {
		if protected == "" {
			continue
		}
		canonical, err := filepath.EvalSymlinks(protected)
		if err != nil {
			canonical = filepath.Clean(protected)
		}
		if textfiles.Overlaps(real, canonical) {
			return "", core.Invalid("sandbox", "native scope overlaps protected data")
		}
	}
	return real, nil
}
func (r *Runner) nativeTools() ([]string, error) {
	var out []string
	for _, p := range r.options.ToolDirectories {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		real, err := r.nativeExternal(p)
		if err != nil {
			return nil, err
		}
		out = append(out, real)
	}
	return out, nil
}
func (r *Runner) prepare(ctx context.Context) error {
	if !filepath.IsAbs(r.options.PrivateRoot) {
		return unavailable("sandbox_private_directory_required")
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return unavailable("sandbox_seatbelt_unavailable")
	}
	// Fixed system read scopes must not accidentally expose a private deployment.
	for _, base := range []string{"/System/Library", "/usr/lib", "/usr/share", "/bin", "/usr/bin", "/sbin", "/usr/sbin", "/private/etc/ssl", "/private/var/db/mds"} {
		for _, protected := range append([]string{r.options.PrivateRoot, r.options.WorkRoot}, r.options.ProtectedRoots...) {
			if textfiles.Overlaps(base, protected) {
				return unavailable("sandbox_seatbelt_protected_root")
			}
		}
	}
	read, err := r.nativeTools()
	if err != nil {
		return err
	}
	// Probe enforcement rather than reporting availability merely because the binary exists.
	dir, err := os.MkdirTemp(r.options.PrivateRoot, "seatbelt-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	work := filepath.Join(dir, "work")
	if err = os.Mkdir(work, 0700); err != nil {
		return err
	}
	secret := filepath.Join(dir, "private")
	if err = os.WriteFile(secret, []byte("probe"), 0600); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "probe") })}
	defer server.Close()
	go server.Serve(listener)
	for _, network := range []bool{false, true} {
		profile, err := seatbeltProfile(read, []string{work}, "", network)
		if err != nil {
			return err
		}
		script := `echo test > "$1/result" || exit 10; if cat "$2" 2>/dev/null; then exit 11; fi; if echo test > "$2" 2>/dev/null; then exit 12; fi; /usr/bin/curl --noproxy '*' --silent --max-time 2 "$3" > /dev/null 2>&1`
		if !network {
			script += "; test $? -ne 0"
		}
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", seatbeltCloseDescriptors, "molr-seatbelt", "/usr/bin/sandbox-exec", "-p", profile, "/bin/sh", "-c", script, "probe", work, secret, "http://"+listener.Addr().String())
		cmd.Dir = work
		cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + work, "TMPDIR=" + work}
		if err = cmd.Run(); err != nil {
			return unavailable("sandbox_seatbelt_probe_failed")
		}
	}
	return nil
}
func (r *Runner) start(ctx context.Context, e core.Execution, c platform.Command) (*process, error) {
	return r.startHost(ctx, e, c, func(cmd *exec.Cmd, work, tty string) (func(), error) {
		if !r.options.NoWorkspace {
			if err := platform.EnsureWorkspace(work); err != nil {
				return nil, err
			}
		}
		work, err := filepath.EvalSymlinks(work)
		if err != nil {
			return nil, err
		}
		temp, err := os.MkdirTemp(r.options.PrivateRoot, "seatbelt-tmp-")
		if err != nil {
			return nil, err
		}
		cleanup := func() { os.RemoveAll(temp) }
		fail := func(err error) (func(), error) { cleanup(); return nil, err }
		temp, err = filepath.EvalSymlinks(temp)
		if err != nil {
			return fail(err)
		}
		read, err := r.nativeTools()
		if err != nil {
			return fail(err)
		}
		global := platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID)
		if _, err := os.Stat(global); !r.options.NoWorkspace && err == nil {
			global, err = filepath.EvalSymlinks(global)
			if err != nil {
				return fail(err)
			}
			read = append(read, global)
		}
		write := []string{work, temp}
		var resourceEnv []string
		for _, m := range c.Mounts {
			source, err := r.nativeExternal(m.Source)
			if err != nil {
				return fail(err)
			}
			if r.options.NoWorkspace && m.Destination != "" {
				resourceEnv = append(resourceEnv, platform.ResourceEnvironment(m.Destination)+"="+source)
			}
			if m.ReadOnly {
				read = append(read, source)
			} else {
				write = append(write, source)
			}
			if m.Destination != "" && (cmd.Dir == m.Destination || strings.HasPrefix(cmd.Dir, m.Destination+"/")) {
				cmd.Dir = source + strings.TrimPrefix(cmd.Dir, m.Destination)
			}
		}
		if cmd.Dir == "/tmp" || strings.HasPrefix(cmd.Dir, "/tmp/") {
			cmd.Dir = temp + strings.TrimPrefix(cmd.Dir, "/tmp")
		}
		profile, err := seatbeltProfile(read, write, tty, c.NetworkAccess)
		if err != nil {
			return fail(err)
		}
		// Go's exec preserves foreign descriptors that lack FD_CLOEXEC. Close them
		// in a fixed, non-interactive launcher BEFORE installing Seatbelt: an
		// already-open descriptor can bypass a path policy. No user script runs
		// in this launcher; all sandbox arguments are passed through "$@".
		args := append([]string{"/bin/sh", "-c", seatbeltCloseDescriptors, "molr-seatbelt", "/usr/bin/sandbox-exec", "-p", profile}, cmd.Args...)
		cmd.Path = "/bin/sh"
		cmd.Args = args
		// Do not inherit Runtime credentials, proxy settings, SSH sockets or DYLD hooks.
		cmd.Env = []string{"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + work, "TMPDIR=" + temp, "LANG=en_US.UTF-8", "TERM=xterm-256color"}
		if r.options.NoWorkspace {
			cmd.Env = append(cmd.Env, resourceEnv...)
		} else {
			cmd.Env = append(cmd.Env, "MOLR_WORKSPACE="+work, "MOLR_GLOBAL="+global)
		}
		return cleanup, nil
	})
}

const seatbeltCloseDescriptors = `seen=0
for entry in /dev/fd/*; do
 fd=${entry##*/}
 case "$fd" in ''|*[!0-9]*) continue;; esac
 seen=1
 case "$fd" in 0|1|2) continue;; esac
 eval "exec $fd>&-" || exit 125
done
[ "$seen" = 1 ] || exit 125
exec "$@"`
