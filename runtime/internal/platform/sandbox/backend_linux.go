//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (r *Runner) check() error {
	if r.options.HostTools {
		if err := checkHostToolsIdentity(); err != nil {
			return err
		}
	}
	o := r.options
	for _, p := range []string{o.Bwrap, o.RootFS, o.Helper, o.CgroupRoot, o.WorkRoot, o.PrivateRoot} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
			return unavailable("sandbox_not_configured")
		}
	}
	if !strings.HasPrefix(o.CgroupRoot, "/sys/fs/cgroup/") || o.MinFreeDiskBytes <= 0 {
		return unavailable("sandbox_not_configured")
	}
	executables := []string{o.Bwrap, o.Helper, "/usr/bin/nsenter"}
	if r.bwrapProfile != "" {
		executables = append(executables, "/usr/bin/aa-exec")
	}
	for _, p := range executables {
		f, e := pin(p, false)
		if e != nil {
			return unavailable("sandbox_helper_unavailable")
		}
		i, e := f.Stat()
		f.Close()
		if e != nil || !i.Mode().IsRegular() || i.Mode()&0111 == 0 || i.Mode()&0022 != 0 {
			return unavailable("sandbox_helper_unavailable")
		}
	}
	for _, p := range []string{o.RootFS, o.WorkRoot, o.CgroupRoot} {
		f, e := pin(p, true)
		if e != nil {
			return unavailable("sandbox_directory_unavailable")
		}
		f.Close()
	}
	if e := freeDiskSpace(o.WorkRoot, o.MinFreeDiskBytes); e != nil {
		return e
	}
	data, e := os.ReadFile(filepath.Join(o.CgroupRoot, "cgroup.subtree_control"))
	if e != nil {
		return unavailable("sandbox_cgroup_unavailable")
	}
	for _, controller := range []string{"cpu", "memory", "pids"} {
		if !strings.Contains(" "+strings.TrimSpace(string(data))+" ", " "+controller+" ") {
			return unavailable("sandbox_cgroup_not_delegated")
		}
	}
	return nil
}
func (r *Runner) recover() error {
	// Cgroup root must be dedicated to this runtime. Lock it as well as the work
	// volume before killing stale children; distinct instances must not share it.
	root, err := os.Open(r.options.CgroupRoot)
	if err != nil {
		return err
	}
	if err = unix.Flock(int(root.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		root.Close()
		return unavailable("sandbox_cgroup_in_use")
	}
	f, err := os.OpenFile(filepath.Join(r.options.WorkRoot, ".molr-sandbox.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		root.Close()
		return err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		root.Close()
		return unavailable("sandbox_workspace_in_use")
	}
	release := func() { f.Close(); root.Close() }
	children, err := os.ReadDir(r.options.CgroupRoot)
	if err != nil {
		release()
		return err
	}
	var clean func(string) error
	clean = func(root string) error {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				path := filepath.Join(root, entry.Name())
				if err := clean(path); err != nil {
					return err
				}
			}
		}
		return (&cgroup{path: root}).cleanup()
	}
	for _, child := range children {
		if child.IsDir() && (strings.HasPrefix(child.Name(), "quota-") || strings.HasPrefix(child.Name(), "account-") || strings.HasPrefix(child.Name(), "molr-")) {
			if err := clean(filepath.Join(r.options.CgroupRoot, child.Name())); err != nil {
				release()
				return err
			}
		}
	}
	r.release = release
	return nil
}
func pin(p string, dir bool) (*os.File, error) {
	flags := uint64(unix.O_PATH | unix.O_CLOEXEC)
	if dir {
		flags |= unix.O_DIRECTORY
	}
	fd, e := unix.Openat2(unix.AT_FDCWD, p, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), p), nil
}
func freeDiskSpace(p string, minimum int64) error {
	var s unix.Statfs_t
	if e := unix.Statfs(p, &s); e != nil {
		return unavailable("sandbox_volume_unavailable")
	}
	if minimum <= 0 || s.Bsize <= 0 || s.Bavail < (uint64(minimum)+uint64(s.Bsize)-1)/uint64(s.Bsize) {
		return unavailable("sandbox_disk_space_low")
	}
	return nil
}
func (r *Runner) checkWritableSpace(c platform.Command) error {
	if err := freeDiskSpace(r.options.WorkRoot, r.options.MinFreeDiskBytes); err != nil {
		return err
	}
	for _, m := range c.Mounts {
		if !m.ReadOnly {
			if err := freeDiskSpace(m.Source, r.options.MinFreeDiskBytes); err != nil {
				return err
			}
		}
	}
	return nil
}
func beneath(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
func (r *Runner) protected(p string) bool {
	for _, s := range []string{r.options.PrivateRoot, r.options.RootFS, r.options.Helper, r.options.Bwrap, r.options.CgroupRoot, r.options.WorkRoot} {
		if beneath(p, s) || beneath(s, p) {
			return true
		}
	}
	return false
}
func rejectSubmounts(root string) error {
	f, e := os.Open("/proc/self/mountinfo")
	if e != nil {
		return e
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		parts := strings.Fields(scan.Text())
		if len(parts) < 5 {
			continue
		}
		p := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(parts[4])
		if p != root && beneath(p, root) {
			return unavailable("sandbox_nested_mount_unsupported")
		}
	}
	return scan.Err()
}
func memoryFile(data []byte) (*os.File, error) {
	fd, e := unix.MemfdCreate("molr-sandbox", unix.MFD_CLOEXEC)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "sandbox-data")
	if _, e = f.Write(data); e != nil {
		f.Close()
		return nil, e
	}
	_, e = f.Seek(0, 0)
	return f, e
}
func (r *Runner) start(parent context.Context, e core.Execution, c platform.Command) (*process, error) {
	return r.startAuthorized(parent, e, c, r.options.Authorize)
}
func (r *Runner) startAuthorized(parent context.Context, e core.Execution, c platform.Command, authorize func(context.Context, core.Execution, platform.Command) error) (*process, error) {
	deadline := c.AuthorizationDeadline
	if c.Timeout > 0 {
		deadline = time.Now().Add(c.Timeout)
	}
	if !c.AuthorizationDeadline.IsZero() && (deadline.IsZero() || c.AuthorizationDeadline.Before(deadline)) {
		deadline = c.AuthorizationDeadline
	}
	ctx, cancel := context.WithCancel(parent)
	if !deadline.IsZero() {
		cancel()
		ctx, cancel = context.WithDeadline(parent, deadline)
	}
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	if err := r.checkWritableSpace(c); err != nil {
		return nil, err
	}
	groupRoot := r.options.CgroupRoot
	if v := c.GroupComputeConstraints; v != nil {
		if err := (&policy.Constraints{Compute: v}).Validate(); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(string(e.Principal.OwnerID)))
		groupRoot = filepath.Join(groupRoot, "account-"+hex.EncodeToString(sum[:16]))
		if err := os.Mkdir(groupRoot, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		for name, value := range map[string]string{"memory.max": strconv.FormatInt(v.MemoryBytes, 10), "memory.swap.max": "0", "pids.max": strconv.Itoa(v.Processes), "cpu.max": fmt.Sprintf("%d 100000", v.CPUPercent*1000), "cgroup.subtree_control": "+cpu +memory +pids"} {
			if err := os.WriteFile(filepath.Join(groupRoot, name), []byte(value), 0600); err != nil {
				return nil, unavailable("sandbox_user_quota_unavailable")
			}
		}
	}
	if c.ComputeQuota {
		sum := sha256.Sum256([]byte(string(e.Principal.OwnerID) + "/" + string(e.AssistantID)))
		groupRoot = filepath.Join(groupRoot, "quota-"+hex.EncodeToString(sum[:16]))
		if err := os.Mkdir(groupRoot, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		for name, value := range map[string]string{"memory.max": strconv.FormatInt(c.Limits.MemoryBytes, 10), "memory.swap.max": "0", "pids.max": strconv.Itoa(c.Limits.Processes), "cpu.max": fmt.Sprintf("%d 100000", c.Limits.CPUPercent*1000), "cgroup.subtree_control": "+cpu +memory +pids"} {
			if err := os.WriteFile(filepath.Join(groupRoot, name), []byte(value), 0600); err != nil {
				return nil, unavailable("sandbox_shared_quota_unavailable")
			}
		}
	}
	group, err := newGroup(groupRoot, c.Limits)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !ok {
			_ = group.cleanup()
		}
	}()
	work := ""
	if !r.options.NoWorkspace {
		work = platform.AssistantWorkspace(r.options.PrivateRoot, r.options.WorkRoot, e.Principal.OwnerID, e.AssistantID)
		if err = os.Mkdir(work, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	add := func(f *os.File) string { files = append(files, f); return strconv.Itoa(2 + len(files)) }
	// Network initialization maps the service UID to 0 in its private userns.
	// Map it back to the service's numeric identity in the final sandbox. The
	// underlying host UID/GID and file permissions are unchanged.
	args := []string{"--unshare-all", "--unshare-user", "--unshare-pid", "--unshare-net", "--unshare-ipc", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--uid", strconv.Itoa(os.Getuid()), "--gid", strconv.Itoa(os.Getgid())}
	if c.TTY {
		for i, arg := range args {
			if arg == "--new-session" {
				args = append(args[:i], args[i+1:]...)
				break
			}
		}
	}
	bind := func(src, dst string, ro bool) error {
		f, e := pin(src, true)
		if e != nil {
			return e
		}
		flag := "--bind-fd"
		if ro {
			flag = "--ro-bind-fd"
		}
		args = append(args, flag, add(f), dst)
		return nil
	}
	if err = rejectSubmounts(r.options.RootFS); err != nil {
		return nil, err
	}
	if err = bind(r.options.RootFS, "/", true); err != nil {
		return nil, err
	}
	if r.options.HostTools {
		if err = appendHostToolsMounts(&args, add, append([]string{r.options.PrivateRoot, r.options.WorkRoot}, r.options.ProtectedRoots...)...); err != nil {
			return nil, err
		}
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--size", strconv.FormatInt(c.Limits.TempBytes, 10), "--tmpfs", "/tmp", "--size", "1048576", "--tmpfs", "/resources", "--size", "1048576", "--tmpfs", "/molr")
	if err = r.appendGPUMounts(ctx, &args, add, c.GPUDeviceIDs); err != nil {
		return nil, err
	}
	if work != "" {
		if err = bind(work, "/work", false); err != nil {
			return nil, err
		}
	}
	if !r.options.NoWorkspace {
		global := platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID)
		if err = os.MkdirAll(global, 0700); err != nil {
			return nil, err
		}
		if err = bind(global, "/global", true); err != nil {
			return nil, err
		}
	}
	for _, m := range c.Mounts {
		protected := false
		for _, root := range r.options.ProtectedRoots {
			if beneath(m.Source, root) || beneath(root, m.Source) {
				protected = true
			}
		}
		browserAsset := c.BrowserAssets && c.Managed && m.ReadOnly && (m.Destination == "/resources/browser-agent" || m.Destination == "/resources/browser-chromium")
		if r.protected(m.Source) || protected && !browserAsset {
			return nil, core.NewError(core.Forbidden, "private sandbox path")
		}
		var filesystem unix.Statfs_t
		if unix.Statfs(m.Source, &filesystem) != nil {
			return nil, unavailable("sandbox_volume_unavailable")
		}
		switch filesystem.Type {
		case unix.PROC_SUPER_MAGIC, unix.SYSFS_MAGIC, unix.CGROUP_SUPER_MAGIC, unix.CGROUP2_SUPER_MAGIC, unix.DEBUGFS_MAGIC, unix.TRACEFS_MAGIC, unix.SECURITYFS_MAGIC, unix.BPF_FS_MAGIC, unix.DEVPTS_SUPER_MAGIC:
			return nil, core.NewError(core.Forbidden, "kernel filesystem cannot be a sandbox resource")
		}
		if err = rejectSubmounts(m.Source); err != nil {
			return nil, err
		}
		if err = bind(m.Source, m.Destination, m.ReadOnly); err != nil {
			return nil, err
		}
	}
	helper, err := pin(r.options.Helper, false)
	if err != nil {
		return nil, err
	}
	args = append(args, "--ro-bind-fd", add(helper), "/molr/helper")
	if r.options.HostTools {
		// Host commands precede this read-only applet directory. The helper
		// selects and probes a shell inside the finished sandbox view.
		shell, e := pin(filepath.Join(r.options.RootFS, "bin/busybox"), false)
		if e != nil {
			return nil, unavailable("sandbox_package_incomplete")
		}
		args = append(args, "--ro-bind-fd", add(shell), "/molr/busybox")
		for _, directory := range []string{"bin", "probe"} {
			if err = bind(filepath.Join(r.options.RootFS, "molr", directory), "/molr/"+directory, true); err != nil {
				return nil, unavailable("sandbox_package_incomplete")
			}
		}
	}
	launch := helperConfig{ProtocolInput: c.ProtocolInput, NoWorkspace: r.options.NoWorkspace, TTY: c.TTY, HostTools: r.options.HostTools, Script: c.Script, Directory: c.Directory, MaxFileBytes: c.Limits.MaxFileBytes, CPUPercent: c.Limits.CPUPercent}
	var network *networkSession
	if len(c.NetworkTargets) > 0 || len(c.ForwardPorts) > 0 {
		network, err = r.startNetworkWithQuotas(ctx, e, c, func(check context.Context) error { return authorize(check, e, c) })
		if err != nil {
			return nil, err
		}
		defer func() {
			if !ok {
				_ = network.close()
			}
		}()
		for i, arg := range args {
			if arg == "--unshare-net" {
				args[i] = "--share-net"
			}
		}
		resolver, err := memoryFile([]byte(resolverFile))
		if err != nil {
			return nil, err
		}
		args = append(args, "--ro-bind-data", add(resolver), "/etc/resolv.conf")
	}
	if c.BrowserAssets {
		proxy := browser.Proxy{ProxySummary: browser.ProxySummary{Mode: "direct"}}
		if c.BrowserProxy != nil {
			proxy = *c.BrowserProxy
		}
		data, err := proxy.LaunchConfig()
		if err != nil {
			return nil, err
		}
		config, err := memoryFile(data)
		clear(data)
		if err != nil {
			return nil, err
		}
		args = append(args, "--ro-bind-data", add(config), "/molr/browser.json")
	}
	payload, _ := json.Marshal(launch)
	f, err := memoryFile(payload)
	if err != nil {
		return nil, err
	}
	args = append(args, "--ro-bind-data", add(f), "/molr/launch.json")
	f, err = memoryFile(filterBytes())
	if err != nil {
		return nil, err
	}
	args = append(args, "--seccomp", add(f))
	home := "/work"
	if r.options.NoWorkspace {
		home = "/tmp"
	}
	// Bound Go initialization before HelperMain runs on high-core-count hosts.
	args = append(args, "--remount-ro", "/molr", "--remount-ro", "/resources", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", home, "--setenv", "TMPDIR", "/tmp", "--setenv", "LANG", "C.UTF-8", "--setenv", "GOMAXPROCS", "2", "--chdir", c.Directory, "--", "/molr/helper", "sandbox-helper", "supervise")
	if err = authorize(ctx, e, c); err != nil {
		return nil, err
	}
	budget := &outputBudget{left: c.MaxOutputBytes}
	p := &process{stdout: newStream(budget), stderr: newStream(budget), done: make(chan struct{}), cancel: cancel, operation: string(e.Principal.ExecutionID) + "/" + string(e.OperationID)}
	launcher := r.bwrapCommand(args...)
	cmd := exec.Command(launcher[0], launcher[1:]...)
	if network != nil {
		p.networkDiagnostic = network.lastDiagnostic
	}
	var networkFailure <-chan struct{}
	if network != nil {
		user, netns, dupErr := network.duplicateNamespaces()
		if dupErr != nil {
			return nil, dupErr
		}
		userFD, netFD := add(user), add(netns)
		wrapped := []string{"--user=/proc/self/fd/" + userFD, "--net=/proc/self/fd/" + netFD, "--preserve-credentials", "--"}
		cmd = exec.Command("/usr/bin/nsenter", append(wrapped, launcher...)...)
		networkFailure = network.failure
	}

	if network != nil && len(c.ForwardPorts) > 0 {
		slots := make(chan struct{}, 32)
		p.forward = func(call context.Context, port int) (net.Conn, error) {
			if ctx.Err() != nil || !slices.Contains(c.ForwardPorts, port) {
				return nil, core.NewError(core.Forbidden, "port not forwarded by this task")
			}
			if err := authorize(ctx, e, c); err != nil {
				return nil, err
			}
			select {
			case slots <- struct{}{}:
			default:
				return nil, core.NewError(core.RateLimited, "forward connection limit")
			}
			conn, err := network.dialForward(call, r.options.Helper, port)
			if err != nil {
				<-slots
				return nil, err
			}
			tracked := &forwardConn{Conn: conn, release: func() { <-slots }}
			stop := context.AfterFunc(ctx, func() { tracked.Close() })
			tracked.setStop(stop)
			if ctx.Err() != nil {
				tracked.Close()
				return nil, ctx.Err()
			}
			return tracked, nil
		}
	}
	cmd.Env = []string{}
	cmd.ExtraFiles = files
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	if c.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(c.StdoutSink, p.stdout)
	}
	if c.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(c.StderrSink, p.stderr)
	}
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(group.file.Fd()), Pdeathsig: syscall.SIGKILL}
	if c.ProtocolInput {
		if c.TTY {
			return nil, core.Invalid("watcher", "watcher cannot use a terminal")
		}
		input, output, pipeErr := os.Pipe()
		if pipeErr != nil {
			return nil, pipeErr
		}
		defer input.Close()
		p.input = output
		cmd.Stdin = input
		defer func() {
			if !ok {
				output.Close()
			}
		}()
	}
	var ttyDone chan struct{}
	if c.TTY {
		master, slave, ttyErr := openPTY()
		if ttyErr != nil {
			return nil, ttyErr
		}
		defer slave.Close()
		p.terminal = master
		defer func() {
			if !ok {
				master.Close()
			}
		}()
		destination := cmd.Stdout
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		cmd.SysProcAttr.Setsid, cmd.SysProcAttr.Setctty, cmd.SysProcAttr.Ctty = true, true, 0
		ttyDone = make(chan struct{})
		go func() { io.Copy(destination, master); close(ttyDone) }()
	}
	if err = cmd.Start(); err != nil {
		return nil, core.Wrap(core.Unavailable, "sandbox process could not start", err)
	}
	ok = true
	go func() {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var waitErr, errorState error
	loop:
		for {
			select {
			case waitErr = <-done:
				break loop
			case <-networkFailure:
				errorState = unavailable("sandbox_network_gateway_failed")
				if ctx.Err() != nil {
					errorState = ctx.Err()
				}
				_ = group.kill()
				waitErr = <-done
				break loop
			case <-ctx.Done():
				errorState = ctx.Err()
				_ = group.kill()
				waitErr = <-done
				break loop
			case <-ticker.C:
				if diskErr := r.checkWritableSpace(c); diskErr != nil {
					errorState = diskErr
					_ = group.kill()
					waitErr = <-done
					break loop
				}
				authCtx, stop := context.WithTimeout(ctx, 2*time.Second)
				authErr := authorize(authCtx, e, c)
				stop()
				if authErr != nil {
					errorState = authErr
					_ = group.kill()
					waitErr = <-done
					break loop
				}
			}
		}
		var networkErr error
		if network != nil {
			networkErr = network.close()
		}
		memoryEvents, _ := os.ReadFile(filepath.Join(group.path, "memory.events"))
		pidsEvents, _ := os.ReadFile(filepath.Join(group.path, "pids.events"))
		cleanErr := group.cleanup()
		if cleanErr == nil {
			cleanErr = networkErr
		}
		if ttyDone != nil {
			select {
			case <-ttyDone:
			case <-time.After(time.Second):
			}
			p.terminal.Close()
			<-ttyDone
		}
		cancel()
		p.exit.Code = cmd.ProcessState.ExitCode()
		if network != nil {
			p.exit.NetworkReason = network.lastReason()
		}
		p.exit.TimedOut = errorState == context.DeadlineExceeded
		if errorState != nil {
			p.exit.Reason = "authorization_changed"
			if core.ReasonOf(errorState) == "sandbox_network_gateway_failed" {
				p.exit.Reason = "network_gateway_failed"
				if network != nil && network.lastReason() != "" {
					p.exit.Reason = network.lastReason()
				}
			}
			if core.ReasonOf(errorState) == "sandbox_disk_space_low" {
				p.exit.Reason = "disk_space_low"
			}
			if errorState == context.Canceled {
				p.exit.Reason = "canceled"
			}
			if p.exit.TimedOut {
				p.exit.Reason = "timeout"
			}
		}
		p.exit.Reason = resourceExitReason(p.exit, memoryEvents, pidsEvents)
		budget.mu.Lock()
		p.exit.OutputTruncated = budget.truncated
		budget.mu.Unlock()
		// Ordinary shell failures are results, not permission to replay side effects.
		if errorState != nil {
			p.exit.Code = 125
		}
		if waitErr != nil && p.exit.Code == 0 {
			p.exit.Code = 125
		}
		p.err = cleanErr
		p.stdout.finish()
		p.stderr.finish()
		if p.input != nil {
			p.input.Close()
		}
		close(p.done)
	}()
	return p, nil
}

type cgroup struct {
	path string
	file *os.File
}

func newGroup(root string, l platform.Limits) (*cgroup, error) {
	var random [12]byte
	if _, e := rand.Read(random[:]); e != nil {
		return nil, e
	}
	p := filepath.Join(root, "molr-"+hex.EncodeToString(random[:]))
	if e := os.Mkdir(p, 0700); e != nil {
		return nil, unavailable("sandbox_cgroup_not_delegated")
	}
	g := &cgroup{path: p}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(p)
		}
	}()
	for k, v := range map[string]string{"memory.max": strconv.FormatInt(l.MemoryBytes, 10), "memory.swap.max": "0", "memory.oom.group": "1", "pids.max": strconv.Itoa(l.Processes), "cpu.max": fmt.Sprintf("%d 100000", l.CPUPercent*1000)} {
		if e := os.WriteFile(filepath.Join(p, k), []byte(v), 0600); e != nil {
			return nil, unavailable("sandbox_cgroup_limit_unavailable")
		}
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	g.file = f
	ok = true
	return g, nil
}
func (g *cgroup) kill() error {
	return os.WriteFile(filepath.Join(g.path, "cgroup.kill"), []byte("1"), 0600)
}
func (g *cgroup) cleanup() error {
	defer func() {
		if g.file != nil {
			g.file.Close()
		}
	}()
	if err := g.kill(); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(g.path, "cgroup.events"))
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "populated 0") {
			return os.Remove(g.path)
		}
		if time.Now().After(deadline) {
			return unavailable("sandbox_cleanup_incomplete")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Per-execution counters are read before the cgroup is removed. A denied fork
// does not itself terminate a successful command or override cancellation.
func resourceExitReason(exit platform.Exit, memoryEvents, pidsEvents []byte) string {
	hit := func(events []byte, key string) bool {
		for _, line := range strings.Split(string(events), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == key {
				n, err := strconv.ParseUint(fields[1], 10, 64)
				return err == nil && n > 0
			}
		}
		return false
	}
	if hit(memoryEvents, "oom_kill") {
		return "memory_limit"
	}
	if exit.Code != 0 && exit.Reason == "" && hit(pidsEvents, "max") {
		return "process_limit"
	}
	return exit.Reason
}
