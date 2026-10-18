// Package sandbox implements the mandatory OS boundary for untrusted commands.
// Backend selection is compiled in; an unavailable backend never runs on the host.
package sandbox

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/quota"
	"net"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

type Options struct {
	ProtectedRoots  []string
	ToolDirectories []string
	GPU             platform.GPUConfig
	// NoWorkspace executes remote commands without an assistant home.
	NoWorkspace                                              bool
	QuotaStore                                               persistence.Store
	AllowFullAccess                                          bool
	NetworkAuthorization                                     func(context.Context, core.Execution, platform.NetworkAuthorization) (platform.NetworkAuthorization, error)
	HostTools                                                bool
	Bwrap, RootFS, Helper, CgroupRoot, WorkRoot, PrivateRoot string
	MinFreeDiskBytes                                         int64
	NetworkBytes                                             int64
	NetworkConnections                                       int
	Authorize                                                func(context.Context, core.Execution, platform.Command) error
}
type Runner struct {
	// Selected once by startup probes; business commands never retry another profile.
	bwrapProfile        string
	gpuDevices          []platform.GPUDevice
	gpuNodes            map[string]gpuNode
	quotas              *quota.Ledger
	options             Options
	mu                  sync.Mutex
	active              map[string]*process
	closed              bool
	release             func()
	recovered           bool
	prepareMu           sync.Mutex
	prepareOnce         sync.Once
	prepareErr          error
	prepared            bool
	shutdownEnvironment func()
}

// Prepare creates managed assets and obtains OS isolation resources. It never
// installs packages, modifies global settings, or falls back to host execution.
func (r *Runner) Prepare(ctx context.Context) error {
	r.prepareMu.Lock()
	defer r.prepareMu.Unlock()
	r.prepareOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed {
			r.prepareErr = unavailable("sandbox_stopped")
			return
		}
		r.prepareErr = r.prepare(ctx)
		r.prepared = true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if runtime.GOOS == "windows" && core.ReasonOf(r.prepareErr) == "sandbox_file_scope_busy" {
		r.prepareOnce = sync.Once{}
		r.prepared = false
	}
	return r.prepareErr
}

func New(o Options) *Runner {
	if o.NetworkBytes == 0 {
		o.NetworkBytes = 64 << 20
	}
	if o.NetworkConnections == 0 {
		o.NetworkConnections = 16
	}
	if o.Bwrap == "" && runtime.GOOS == "linux" {
		o.Bwrap = "/usr/bin/bwrap"
	}
	return &Runner{quotas: &quota.Ledger{Store: o.QuotaStore}, options: o, active: map[string]*process{}}
}
func unavailable(reason string) error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "sandbox unavailable"), core.Reason(reason))
}
func key(e core.Execution) string {
	return string(e.Principal.OwnerID) + "/" + string(e.AssistantID) + "/" + string(e.OperationID)
}
func (r *Runner) Capabilities(ctx context.Context) (platform.Capabilities, error) {
	if runtime.GOOS == "windows" {
		r.mu.Lock()
		retry := core.ReasonOf(r.prepareErr) == "sandbox_file_scope_busy"
		r.mu.Unlock()
		if retry {
			_ = r.Prepare(ctx)
		}
	}

	c := platform.Capabilities{OS: runtime.GOOS, Reason: "sandbox_not_configured", ShellType: "posix-sh"}
	if runtime.GOOS == "windows" {
		c.ShellType = "powershell"
	}
	if platform.NativeSandbox() {
		c.NetworkBackend = platform.NativeNetworkBackend()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	closed := r.closed
	if closed {
		c.Reason = "sandbox_stopped"
		return c, nil
	}
	if ctx.Err() != nil {
		return c, ctx.Err()
	}
	c.FullAccess = r.options.AllowFullAccess && r.checkDirect() == nil
	if r.prepareErr != nil {
		c.Reason = string(core.ReasonOf(r.prepareErr))
		if c.Reason == "" {
			c.Reason = "sandbox_setup_failed"
		}
		return c, nil
	}
	if err := r.check(); err != nil {
		c.Reason = string(core.ReasonOf(err))
		if c.Reason == "" {
			c.Reason = "sandbox_unavailable"
		}
		return c, nil
	}
	c.Shell, c.FileIsolation, c.ProcessIsolation, c.NetworkIsolation, c.ResourceLimits = true, true, true, true, true
	c.Reason = "available"
	c.NetworkBackend = "transparent"
	c.NetworkProtocols = []string{"tcp", "udp"}
	c.DiskProtection = "file_limit_and_free_space_guard"
	if platform.NativeSandbox() {
		c.ProcessIsolation, c.ResourceLimits = false, false
		c.NetworkBackend = platform.NativeNetworkBackend()
		c.DiskProtection = "none"
	}
	return c, nil
}
func validate(c platform.Command) error {
	if c.NetworkAccess {
		return core.Invalid("network", "native network access is unsupported by this backend")
	}
	if c.BrowserProxy != nil {
		if !c.BrowserAssets || !c.Managed || c.FullAccess || c.Script != "exec /molr/helper sandbox-helper browser" {
			return core.Invalid("browser", "trusted browser launch required")
		}
		proxy := *c.BrowserProxy
		if err := proxy.Validate(); err != nil {
			return core.Invalid("proxy", err.Error())
		}
	}

	if err := core.ValidateGPUDevices(c.GPUDeviceIDs); err != nil {
		return err
	}
	if err := platform.ValidateForwardPorts(c.ForwardPorts); err != nil {
		return err
	}
	if c.Script == "" || len(c.Script) > 65536 || strings.ContainsRune(c.Script, 0) || c.Timeout < 0 || (!c.Managed && (c.Timeout == 0 || c.Timeout > time.Hour)) || c.MaxOutputBytes < 1 || c.MaxOutputBytes > 128<<10 || c.Limits.MemoryBytes < 32<<20 || c.Limits.Processes < 8 || c.Limits.CPUPercent < 1 || c.Limits.TempBytes < 1 || c.Limits.TempBytes > c.Limits.MemoryBytes || c.Limits.MaxFileBytes <= 0 || len(c.Mounts) > 32 || len(c.NetworkTargets) > 32 {
		return core.Invalid("sandbox", "invalid bounded execution plan")
	}
	if c.Executable != "" || len(c.Args) > 0 || len(c.Environment) > 0 || len(c.ReadRoots) > 0 || len(c.WriteRoots) > 0 || len(c.AllowedOrigins) > 0 {
		return core.Invalid("sandbox", "host command options prohibited")
	}
	if c.Directory != "/work" && !strings.HasPrefix(c.Directory, "/work/") && c.Directory != "/tmp" && !strings.HasPrefix(c.Directory, "/tmp/") && !strings.HasPrefix(c.Directory, "/resources/") {
		return core.Invalid("directory", "sandbox directory required")
	}
	if path.Clean(c.Directory) != c.Directory || strings.ContainsRune(c.Directory, 0) {
		return core.Invalid("directory", "invalid directory")
	}
	seen := map[string]bool{}
	for _, m := range c.Mounts {
		if !filepath.IsAbs(m.Source) || filepath.Clean(m.Source) != m.Source || m.Source == "/" || !strings.HasPrefix(m.Destination, "/resources/") || path.Clean(m.Destination) != m.Destination || strings.ContainsRune(m.Destination, 0) || seen[m.Destination] {
			return core.Invalid("mount", "invalid mount")
		}
		for existing := range seen {
			if strings.HasPrefix(existing, m.Destination+"/") || strings.HasPrefix(m.Destination, existing+"/") {
				return core.Invalid("mount", "overlapping mounts")
			}
		}
		seen[m.Destination] = true
	}
	for _, n := range c.NetworkTargets {
		if _, err := platform.CompileNetworkTarget(n); err != nil {
			return err
		}
	}
	return nil
}
func (r *Runner) Start(ctx context.Context, e core.Execution, c platform.Command) (platform.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if err := r.validateMode(c); err != nil {
		return nil, err
	}
	if r.options.Authorize == nil {
		return nil, unavailable("sandbox_authorization_missing")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !c.FullAccess && r.prepareErr != nil {
		return nil, r.prepareErr
	}
	if err := r.checkMode(c.FullAccess); err != nil {
		return nil, err
	}
	if err := r.checkGPUAccess(c); err != nil {
		return nil, err
	}
	if r.closed {
		return nil, unavailable("sandbox_stopped")
	}
	if previous := r.active[key(e)]; previous != nil {
		select {
		case <-previous.done:
			delete(r.active, key(e))
		default:
			return nil, core.NewError(core.Conflict, "shell operation already running")
		}
	}
	if !c.FullAccess && !r.recovered {
		if err := r.recover(); err != nil {
			return nil, err
		}
		r.recovered = true
	}
	if err := r.options.Authorize(ctx, e, c); err != nil {
		return nil, err
	}
	p, err := r.startMode(ctx, e, c)
	if err != nil {
		if reason := string(core.ReasonOf(err)); strings.HasPrefix(reason, "gpu_") {
			for i := range r.gpuDevices {
				if slices.Contains(c.GPUDeviceIDs, r.gpuDevices[i].ID) {
					r.gpuDevices[i].Available = false
					r.gpuDevices[i].Reason = reason
				}
			}
		}
		return nil, err
	}
	p.fullAccess = c.FullAccess
	r.active[key(e)] = p
	go func() {
		<-p.done
		r.mu.Lock()
		if p.err != nil {
			r.closed = true
		}
		if r.active[key(e)] == p {
			delete(r.active, key(e))
		}
		r.mu.Unlock()
	}()
	return p, nil
}

// StopOwner waits for every matching execution to be gone; it does not call
// authorization while waiting, so grant mutation transactions cannot deadlock.
func (r *Runner) StopAssistant(ctx context.Context, owner, assistant core.ID) error {
	r.mu.Lock()
	var processes []*process
	for k, p := range r.active {
		if strings.HasPrefix(k, string(owner)+"/"+string(assistant)+"/") {
			processes = append(processes, p)
		}
	}
	r.mu.Unlock()
	for _, p := range processes {
		if err := p.Cancel(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (r *Runner) StopOwner(ctx context.Context, owner core.ID) error {
	r.mu.Lock()
	var active []*process
	for k, p := range r.active {
		if strings.HasPrefix(k, string(owner)+"/") {
			active = append(active, p)
		}
	}
	r.mu.Unlock()
	for _, p := range active {
		if err := p.Cancel(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (r *Runner) CancelOperation(ctx context.Context, e core.Execution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	p := r.active[key(e)]
	r.mu.Unlock()
	if p == nil {
		return nil
	}
	if p.operation != string(e.Principal.ExecutionID)+"/"+string(e.OperationID) {
		return core.NewError(core.Forbidden, "sandbox cancellation identity mismatch")
	}
	return p.Cancel(ctx)
}
func (r *Runner) Close() error {
	r.mu.Lock()
	r.closed = true
	var active []*process
	for _, p := range r.active {
		active = append(active, p)
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, p := range active {
		if err := p.Cancel(ctx); err != nil {
			return fmt.Errorf("sandbox cleanup: %w", err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.release != nil {
		r.release()
		r.release = nil
	}
	if r.shutdownEnvironment != nil {
		r.shutdownEnvironment()
		r.shutdownEnvironment = nil
	}
	return nil
}

var _ platform.Runner = (*Runner)(nil)

func (r *Runner) DialForward(ctx context.Context, e core.Execution, port int) (net.Conn, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	p := r.active[key(e)]
	r.mu.Unlock()
	if p == nil || p.operation != string(e.Principal.ExecutionID)+"/"+string(e.OperationID) || p.forward == nil {
		return nil, core.NewError(core.Forbidden, "forward task unavailable")
	}
	select {
	case <-p.done:
		return nil, core.NewError(core.Forbidden, "forward task finished")
	default:
	}
	return p.forward(ctx, port)
}

// StopAccess revokes only the matching permission's executions on this node.
func (r *Runner) StopAccess(ctx context.Context, owner, assistant core.ID, full bool) error {
	r.mu.Lock()
	var processes []*process
	for k, p := range r.active {
		if strings.HasPrefix(k, string(owner)+"/"+string(assistant)+"/") && p.fullAccess == full {
			processes = append(processes, p)
		}
	}
	r.mu.Unlock()
	for _, p := range processes {
		if err := p.Cancel(ctx); err != nil {
			return err
		}
	}
	return nil
}
