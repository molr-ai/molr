//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func platformProbe() platform.Command {
	return platform.Command{Script: "exit 0", Directory: "/work", Timeout: 5 * time.Second, MaxOutputBytes: 1024, Limits: platform.Limits{MemoryBytes: 128 << 20, Processes: 32, CPUPercent: 100, TempBytes: 8 << 20, MaxFileBytes: 64 << 20}}
}
func probeExecution() core.Execution {
	return core.Execution{Principal: core.Principal{OwnerID: "sandbox-bootstrap", SubjectID: "probe", Kind: core.ActorAssistant, ExecutionID: "probe"}, AssistantID: "probe", OperationID: "probe"}
}

func (r *Runner) prepare(ctx context.Context) error {
	if r.options.HostTools {
		if err := checkHostToolsIdentity(); err != nil {
			return err
		}
	}
	if !filepath.IsAbs(r.options.PrivateRoot) || r.options.PrivateRoot == "/" {
		return unavailable("sandbox_not_configured")
	}
	private, err := pin(r.options.PrivateRoot, true)
	if err != nil {
		return unavailable("sandbox_directory_unavailable")
	}
	private.Close()
	if r.options.Helper == "" {
		r.options.Helper, err = os.Executable()
		if err != nil {
			return err
		}
	}
	// The packaged helper is static so the managed rootfs does not borrow host libraries.
	if !staticELF(r.options.Helper) {
		return unavailable("sandbox_package_incomplete")
	}
	if r.options.RootFS == "" {
		root, err := prepareRootFS(ctx, r.options.PrivateRoot, r.options.Helper)
		if err != nil {
			return err
		}
		r.options.RootFS = root
	}
	if r.options.WorkRoot == "" {
		name := "work"
		if r.options.NoWorkspace {
			name = "execution"
		}
		r.options.WorkRoot = filepath.Join(r.options.PrivateRoot, "sandbox", name)
		if err := os.MkdirAll(r.options.WorkRoot, 0700); err != nil {
			return err
		}
	}
	if r.options.CgroupRoot == "" {
		if root := ownDelegation(); root != "" {
			if err := prepareControllers(root); err == nil {
				r.options.CgroupRoot = root
			}
		}
		if r.options.CgroupRoot == "" {
			root, closeEnvironment, err := startSupervisor(ctx, r.options.Helper)
			if err != nil {
				return err
			}
			r.options.CgroupRoot = root
			r.shutdownEnvironment = closeEnvironment
		}
	}
	if err := r.check(); err != nil {
		return err
	}
	// Exercise a real isolated helper, rather than advertising readiness based on
	// paths existing. Its work directory is empty and no network is granted.
	probe := platformProbe()
	if r.options.NoWorkspace {
		probe.Directory = "/tmp"
	}
	p, err := r.startAuthorized(ctx, probeExecution(), probe, func(context.Context, core.Execution, platform.Command) error { return nil })
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, p.Stdout())
	_, _ = io.Copy(io.Discard, p.Stderr())
	result, err := p.Wait(ctx)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Cancel(cleanup)
		return unavailable("sandbox_startup_check_failed")
	}
	if result.Code != 0 {
		return unavailable("sandbox_startup_check_failed")
	}
	if err = r.probeNetwork(ctx); err != nil {
		return unavailable("sandbox_network_probe_failed")
	}
	return nil
}

func staticELF(path string) bool {
	file, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	for _, p := range file.Progs {
		if p.Type == elf.PT_INTERP {
			return false
		}
	}
	return (file.Machine == elf.EM_X86_64 || file.Machine == elf.EM_AARCH64) && file.Type == elf.ET_EXEC
}

func prepareRootFS(ctx context.Context, dataRoot, helper string) (string, error) {
	var source string
	for _, candidate := range []string{filepath.Join(filepath.Dir(helper), "libexec", "busybox"), "/usr/bin/busybox", "/bin/busybox"} {
		canonical, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(canonical)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0022 == 0 && staticELF(canonical) {
			source = canonical
			break
		}
	}
	if source == "" {
		return "", unavailable("sandbox_package_incomplete")
	}
	binary, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	ca, _ := os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
	digest := sha256.New()
	digest.Write([]byte("molr-rootfs-v8-local-global"))
	digest.Write(binary)
	digest.Write(ca)
	version := hex.EncodeToString(digest.Sum(nil))
	parent := filepath.Join(dataRoot, "sandbox", "runtime")
	root := filepath.Join(parent, version)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	if f, err := pin(root, true); err == nil {
		f.Close()
		if b, err := os.ReadFile(filepath.Join(root, ".ready")); err == nil && string(b) == version {
			return root, nil
		}
		return "", unavailable("sandbox_assets_invalid")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	temporary, err := os.MkdirTemp(parent, ".prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	for _, dir := range []string{"bin", "sbin", "lib", "lib64", "usr/bin", "etc/ssl/certs", "etc/alternatives", "etc/fonts", "etc/ImageMagick-6", "etc/ImageMagick-7", "work", "global", "tmp", "proc", "dev", "resources", "molr", "molr/bin", "molr/probe"} {
		if err = os.MkdirAll(filepath.Join(temporary, dir), 0755); err != nil {
			return "", err
		}
	}
	if err = os.WriteFile(filepath.Join(temporary, "bin/busybox"), binary, 0755); err != nil {
		return "", err
	}
	for _, name := range strings.Fields("sh ls cat cp mv rm mkdir rmdir touch echo printf pwd test [ head tail wc sort uniq cut tr sed awk grep find xargs chmod ln readlink realpath sleep date env timeout tar gzip gunzip unzip wget nc dd du df sha256sum") {
		if err = os.Symlink("busybox", filepath.Join(temporary, "bin", name)); err != nil {
			return "", err
		}
	}
	if err = prepareBusyBoxFallback(ctx, temporary); err != nil {
		return "", err
	}
	for _, name := range []string{"etc/resolv.conf", "etc/ssl/openssl.cnf", "etc/ld.so.cache"} {
		if err = os.WriteFile(filepath.Join(temporary, name), nil, 0644); err != nil {
			return "", err
		}
	}
	if len(ca) > 0 {
		if err = os.WriteFile(filepath.Join(temporary, "etc/ssl/certs/ca-certificates.crt"), ca, 0644); err != nil {
			return "", err
		}
	}
	if err = os.WriteFile(filepath.Join(temporary, ".ready"), []byte(version), 0600); err != nil {
		return "", err
	}
	if err = os.Rename(temporary, root); err != nil {
		if b, e := os.ReadFile(filepath.Join(root, ".ready")); e == nil && string(b) == version {
			return root, nil
		}
		return "", err
	}
	return root, nil
}

func ownDelegation() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "0::/") {
			continue
		}
		relative := strings.TrimPrefix(line, "0::")
		if filepath.Clean(relative) != relative || filepath.Base(relative) != "supervisor" {
			continue
		}
		parent := filepath.Dir(relative)
		unit := filepath.Base(parent)
		if strings.HasPrefix(unit, "molr") && strings.HasSuffix(unit, ".service") {
			return "/sys/fs/cgroup" + parent
		}
	}
	return ""
}
func prepareControllers(root string) error {
	processes, err := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil || strings.TrimSpace(string(processes)) != "" {
		return unavailable("sandbox_system_unavailable")
	}
	if err = os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0600); err != nil {
		return unavailable("sandbox_system_unavailable")
	}
	return nil
}

// SupervisorMain runs only as a Molr-owned delegated transient systemd unit.
// It holds an EOF lease; losing the Runtime connection stops the entire unit.
func SupervisorMain() int {
	root := ownDelegation()
	if root == "" || prepareControllers(root) != nil {
		return 125
	}
	if json.NewEncoder(os.Stdout).Encode(struct{ Root string }{root}) != nil {
		return 125
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}
func startSupervisor(ctx context.Context, helper string) (string, func(), error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", nil, err
	}
	unit := "molr-sandbox-" + hex.EncodeToString(random[:]) + ".service"
	cmd := exec.Command("/usr/bin/systemd-run", "--user", "--quiet", "--pipe", "--wait", "--collect", "--expand-environment=no", "--unit="+unit, "-p", "Type=exec", "-p", "Delegate=yes", "-p", "DelegateSubgroup=supervisor", "-p", "KillMode=control-group", "-p", "TimeoutStopSec=5s", helper, "sandbox-supervisor")
	runtimeDir := "/run/user/" + strconv.Itoa(os.Getuid())
	cmd.Env = []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + runtimeDir, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDir + "/bus"}
	input, err := cmd.StdinPipe()
	if err != nil {
		return "", nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return "", nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return "", nil, unavailable("sandbox_system_unavailable")
	}
	ready := make(chan string, 1)
	go func() {
		var reply struct{ Root string }
		if json.NewDecoder(io.LimitReader(output, 4096)).Decode(&reply) != nil {
			ready <- ""
			return
		}
		ready <- reply.Root
		_, _ = io.Copy(io.Discard, output)
	}()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	var once sync.Once
	closeEnvironment := func() {
		once.Do(func() {
			input.Close()
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				stop := exec.CommandContext(stopCtx, "/usr/bin/systemctl", "--user", "stop", unit)
				stop.Env = cmd.Env
				_ = stop.Run()
				_ = cmd.Process.Kill()
			}
		})
	}
	timeout := time.NewTimer(8 * time.Second)
	defer timeout.Stop()
	select {
	case root := <-ready:
		if strings.HasPrefix(root, "/sys/fs/cgroup/") && filepath.Clean(root) == root && filepath.Base(root) == unit {
			return root, closeEnvironment, nil
		}
	case <-done:
	case <-ctx.Done():
	case <-timeout.C:
	}
	closeEnvironment()
	return "", nil, unavailable("sandbox_system_unavailable")
}
