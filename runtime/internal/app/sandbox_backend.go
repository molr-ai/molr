package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/textfiles"
)

type sandboxRequestKey struct{}

// remoteSandboxBackend shares confined files and platform execution with Runtime.
// Requests contain resource references, never caller-controlled host paths.
type remoteSandboxBackend struct {
	cfg           config.Config
	files         *resourceFileTools
	runner        *sandbox.Runner
	spoolMu       sync.Mutex
	shellOutputs  sync.Map
	networkQuotas sync.Map
}

func newSandboxBackend(ctx context.Context, cfg config.Config, store persistence.Store) (*remoteSandboxBackend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := prepareDirectory(cfg.DataDir); err != nil {
		return nil, err
	}
	b := &remoteSandboxBackend{cfg: cfg}
	b.files = &resourceFileTools{adapters: adapters{dataDir: cfg.DataDir, storageConfig: cfg.Sandbox}, resolveOverride: b.resolve, providerOverride: func(ctx context.Context) model.Provider {
		q := ctx.Value(sandboxRequestKey{}).(sm.Request)
		p := model.Provider{Protocol: model.Protocol(q.Protocol)}
		p.Model.SupportsImages = q.Images
		p.Model.SupportsAudio = q.Audio
		if q.AudioPricing {
			x := int64(0)
			p.Pricing = &model.Pricing{AudioInputMicrosPerMillion: &x}
		}
		return p
	}}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	b.runner = sandbox.New(sandbox.Options{NoWorkspace: true, ProtectedRoots: cfg.Sandbox.ProtectedRoots, ToolDirectories: cfg.Sandbox.ToolDirectories, GPU: cfg.Sandbox.GPU, QuotaStore: store, AllowFullAccess: cfg.Sandbox.AllowFullAccess, HostTools: cfg.Sandbox.UsesHostTools(), Bwrap: cfg.Sandbox.Bwrap, RootFS: cfg.Sandbox.RootFS, Helper: exe, CgroupRoot: cfg.Sandbox.CgroupRoot, WorkRoot: cfg.Sandbox.WorkRoot, PrivateRoot: cfg.DataDir, MinFreeDiskBytes: cfg.Sandbox.MinFreeDiskBytes, NetworkBytes: cfg.Sandbox.NetworkBytes, NetworkConnections: cfg.Sandbox.NetworkConnections, NetworkAuthorization: b.networkAuthorization, Authorize: func(ctx context.Context, e core.Execution, c platform.Command) error {
		q, ok := ctx.Value(sandboxRequestKey{}).(sm.Request)
		if !ok || q.Identity.Execution() != e || !q.Compute || q.FullAccess != c.FullAccess {
			return denied()
		}
		return ctx.Err()
	}})
	if cfg.Sandbox.Enabled {
		budget := 20 * time.Second
		if cfg.Sandbox.GPU.Enabled {
			budget = 60 * time.Second
		}
		prepare, cancel := context.WithTimeout(ctx, budget)
		_ = b.runner.Prepare(prepare)
		cancel()
	}
	return b, nil
}
func directoryID(p string) string { return "dir_" + digestBytes([]byte(p))[:24] }
func (b *remoteSandboxBackend) Info(ctx context.Context) (sm.Info, error) {
	s := b.cfg.Sandbox
	out := sm.Info{Build: buildinfo.Current(), AllowDirectoryPaths: !s.RestrictDirectories, WorkspaceProjectQuotas: false, ResourceQuotas: true, UserGroupQuotas: true, AuthorizationCeilings: s.ComputeCeilings(), PortForwarding: true, ShellSessions: true, Protocol: sm.ProtocolVersion, Enabled: s.Enabled, Storage: s.Enabled && textfiles.Supported(), Directories: []sm.Directory{}, Networks: []sm.Network{}, Capabilities: platform.Capabilities{OS: runtime.GOOS, Reason: "sandbox_disabled"}, Limits: platform.Limits{MemoryBytes: s.MemoryBytes, Processes: s.Processes, CPUPercent: s.CPUPercent, TempBytes: s.TempBytes, MaxFileBytes: s.MaxFileBytes, NetworkBytes: s.NetworkBytes, NetworkConnections: s.NetworkConnections}, TimeoutSeconds: 0}
	if !s.Enabled {
		return out, nil
	}
	out.Capabilities, _ = b.runner.Capabilities(ctx)
	out.GPUs = b.runner.GPUs()
	if s.GPU.AllDevices {
		for _, g := range out.GPUs {
			out.AuthorizationCeilings.GPUDeviceIDs = append(out.AuthorizationCeilings.GPUDeviceIDs, g.ID)
		}
	}
	out.AuthorizationCeilings.AllGPUDevices = false
	for _, d := range s.Directories {
		out.Directories = append(out.Directories, sm.Directory{ID: directoryID(d.Path), Name: filepath.Base(d.Path), ReadOnly: d.ReadOnly})
	}
	for _, n := range s.NetworkTargets(b.cfg.OwnListenPort()) {
		out.Networks = append(out.Networks, sm.Network{ID: n.Name, Name: n.Name, Target: n.Target()})
	}
	return out, nil
}
func (b *remoteSandboxBackend) resolve(ctx context.Context, e core.Execution, t tools.FileTarget, actions ...policy.Action) (string, string, error) {
	if !b.cfg.Sandbox.Enabled || !textfiles.Supported() {
		return "", "", core.NewError(core.Unavailable, "sandbox storage unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	q, ok := ctx.Value(sandboxRequestKey{}).(sm.Request)
	if !ok || q.Identity.Execution() != e || (q.FullAccess && !b.cfg.Sandbox.AllowFullAccess) {
		return "", "", denied()
	}
	if q.FullAccess {
		if t.ResourceID != "" {
			found := false
			for _, scope := range q.Scopes {
				if scope.ResourceID != t.ResourceID {
					continue
				}
				if d, err := b.directory(ctx, scope.DirectoryID); err == nil {
					t.Path = filepath.Join(d.Path, strings.TrimPrefix(t.Path, "/"))
					found = true
					break
				}
			}
			if !found {
				return "", "", denied()
			}
		}
		if !filepath.IsAbs(t.Path) || t.Path == "/work" || strings.HasPrefix(t.Path, "/work/") {
			return "", "", core.Invalid("path", "remote Full Access requires an absolute host path; no /work alias")
		}
		name := t.Path
		if actual, err := filepath.EvalSymlinks(name); err == nil {
			name = actual
		}
		return "/", strings.TrimPrefix(filepath.Clean(name), "/"), nil
	}
	if t.ResourceID == "" {
		return "", "", core.Invalid("resource_id", "remote computers require an authorized directory; no assistant workspace")
	}
	name, err := resourcePath(t.Path)
	if err != nil {
		return "", "", err
	}
	for _, scope := range q.Scopes {
		if scope.ResourceID != t.ResourceID {
			continue
		}
		prefix, err := resourcePath(scope.Path)
		if err != nil {
			return "", "", err
		}
		if name != prefix && prefix != "/" && !strings.HasPrefix(name, prefix+"/") {
			continue
		}
		allowed := true
		for _, a := range actions {
			if a == policy.Read && !scope.Read || a == policy.Write && !scope.Write {
				allowed = false
			}
		}
		if !allowed {
			continue
		}
		d, err := b.directory(ctx, scope.DirectoryID)
		if err != nil {
			return "", "", err
		}
		if scope.Write {
			for _, entry := range b.cfg.Sandbox.Directories {
				if entry.ReadOnly && textfiles.Overlaps(entry.Path, filepath.Join(d.Path, strings.TrimPrefix(name, "/"))) {
					d.ReadOnly = true
				}
			}
			if d.ReadOnly {
				return "", "", denied()
			}
		}
		return d.Path, strings.TrimPrefix(name, "/"), nil
	}
	return "", "", denied()
}
func decodeSandboxArgs(raw json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return core.Invalid("arguments", "invalid sandbox arguments")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return core.Invalid("arguments", "trailing data")
	}
	return nil
}

type sandboxDownload struct {
	Target   tools.FileTarget
	MaxBytes int64
}
type sandboxBlob struct {
	Data    []byte
	Version string
}
type sandboxUpload struct {
	Overwrite       bool
	ExpectedVersion string
	Target          tools.FileTarget
	Data            []byte
}

func (b *remoteSandboxBackend) Call(ctx context.Context, q sm.Request) (json.RawMessage, error) {
	if err := q.Identity.Execution().Validate(); err != nil {
		return nil, err
	}
	if q.FullAccess && !b.cfg.Sandbox.AllowFullAccess {
		return nil, core.NewError(core.Forbidden, "Full Access disabled by deployment")
	}
	if !b.cfg.Sandbox.Enabled {
		return nil, core.NewError(core.Unavailable, "sandbox disabled")
	}
	ctx = context.WithValue(ctx, sandboxRequestKey{}, q)
	e := q.Identity.Execution()
	f := b.files
	switch q.Method {
	case "quota_usage":
		return b.quotaUsage(ctx, q)
	case "file_read":
		var r tools.FileReadRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		if r.MaxFrames < 0 || r.MaxFrames > 16 {
			return nil, core.Invalid("frames", "invalid frame count")
		}
		r.TextRange = q.TextRange
		return f.read(ctx, e, r)
	case "file_search":
		var r tools.FileSearchRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.search(ctx, e, r)
	case "file_list":
		var r tools.FileListRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.list(ctx, e, r)
	case "file_write":
		var r tools.FileWriteRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.write(ctx, e, r)
	case "file_edit":
		var r tools.FileEditRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.edit(ctx, e, r)
	case "file_move":
		var r tools.FileMoveRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.move(ctx, e, r)
	case "file_delete":
		var r tools.FileDeleteRequest
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		return f.delete(ctx, e, r)
	case "download":
		var r sandboxDownload
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		if r.MaxBytes < 1 || r.MaxBytes > mediaFileLimit {
			return nil, core.Invalid("limit", "invalid file limit")
		}
		root, name, err := f.resolve(ctx, e, r.Target, policy.Read)
		if err != nil {
			return nil, err
		}
		file, err := openResourceFile(root, name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		version, err := resourceFileVersion(file)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, r.MaxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > r.MaxBytes {
			return nil, core.Invalid("file", "file exceeds limit")
		}
		after, err := resourceFileVersion(file)
		if err != nil {
			return nil, err
		}
		if after != version {
			return nil, fileChanged()
		}
		return json.Marshal(sandboxBlob{data, version})
	case "upload":
		var r sandboxUpload
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		if len(r.Data) > 20<<20 {
			return nil, core.Invalid("file", "file exceeds import limit")
		}
		root, name, err := f.resolve(ctx, e, r.Target, policy.Write)
		if err != nil {
			return nil, err
		}
		unlock, err := f.lock(ctx, filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		defer unlock()
		if err := f.checkWorkspaceFileSize(ctx, e, r.Target, int64(len(r.Data))); err != nil {
			return nil, err
		}
		version, err := replaceResourceFile(ctx, root, name, string(r.Data), r.Overwrite, r.ExpectedVersion, func() error { _, _, err := f.resolve(ctx, e, r.Target, policy.Write); return err })
		if err != nil {
			return nil, err
		}
		return json.Marshal(sandboxBlob{Version: version})
	case "snapshot":
		var r media.Part
		if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
			return nil, err
		}
		root, name, err := f.resolve(ctx, e, tools.FileTarget{ResourceID: r.ResourceID, Path: r.Path}, policy.Read)
		if err != nil {
			return nil, err
		}
		if r.Scope != digestBytes([]byte(root)) || len(r.Snapshot) != 64 {
			return nil, denied()
		}
		source, err := openResourceFile(root, name)
		if err != nil {
			return nil, err
		}
		source.Close()
		file, err := openResourceFile(f.mediaRoot(e), r.Snapshot)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, mediaPartLimit+1))
		if err != nil {
			return nil, err
		}
		if len(data) > mediaPartLimit || digestBytes(data) != r.Snapshot {
			return nil, denied()
		}
		return json.Marshal(sandboxBlob{Data: data})
	case "quota_update":
		if platform.NativeSandbox() {
			return nil, core.Invalid("method", "native network scopes are immutable; revoke the execution")
		}
		if current, ok := b.networkQuotas.Load(shellKey(q.Identity.Execution())); ok {
			old := current.([]platform.NetworkAuthorization)
			if len(q.NetworkAuthorizations) > 32 {
				return nil, denied()
			}
			for _, n := range q.NetworkAuthorizations {
				if n.Constraints == nil {
					return nil, denied()
				}
				if err := (&policy.Constraints{Network: n.Constraints}).Validate(); err != nil {
					return nil, err
				}
				if n.Constraints.Bytes > b.cfg.Sandbox.ComputeCeilings().NetworkBytes || n.Constraints.Connections > b.cfg.Sandbox.ComputeCeilings().NetworkConnections {
					return nil, denied()
				}
			}
			if len(old) != len(q.NetworkAuthorizations) {
				return nil, denied()
			}
			for i, n := range q.NetworkAuthorizations {
				if n.GrantID != old[i].GrantID || n.Source != old[i].Source {
					return nil, denied()
				}
			}
			b.networkQuotas.Store(shellKey(q.Identity.Execution()), q.NetworkAuthorizations)
		}
		return json.RawMessage(`{"Updated":true}`), nil
	case "shell_output":
		return b.shellLog(ctx, q)
	case "shell_control", "shell_read":
		return b.shellControl(ctx, q)
	case "shell_execute":
		return b.shell(ctx, q)
	}
	return nil, core.Invalid("method", "unsupported sandbox method")
}
func (b *remoteSandboxBackend) shell(ctx context.Context, q sm.Request) (json.RawMessage, error) {
	if !q.Compute || (!q.FullAccess && q.ComputeConstraints == nil) {
		return nil, denied()
	}
	var req tools.ShellRequest
	if err := decodeSandboxArgs(q.Arguments, &req); err != nil {
		return nil, err
	}
	if req.Script == "" || len(req.Script) > 65536 || len(req.Files) > 32 || len(req.NetworkIDs) > 32 {
		return nil, core.Invalid("script", "invalid shell request")
	}
	s := b.cfg.Sandbox
	if q.FullAccess && req.NetworkMode == "none" {
		return nil, core.Invalid("network_mode", "Full Access cannot provide network isolation")
	}
	c := platform.Command{FullAccess: q.FullAccess, ForwardPorts: req.ForwardPorts, Managed: true, TTY: req.TTY, Script: req.Script, Directory: req.Directory, RequireIsolation: true, Timeout: time.Duration(req.TimeoutSeconds) * time.Second, MaxOutputBytes: s.MaxOutputBytes, AuthorizationDeadline: q.Deadline, Limits: platform.Limits{MemoryBytes: s.MemoryBytes, Processes: s.Processes, CPUPercent: s.CPUPercent, TempBytes: s.TempBytes, MaxFileBytes: s.MaxFileBytes, NetworkBytes: s.NetworkBytes, NetworkConnections: s.NetworkConnections}}
	if !q.FullAccess {
		c.GroupComputeConstraints = q.GroupComputeConstraints
		c.NetworkAuthorizations = q.NetworkAuthorizations
		if len(q.NetworkAuthorizations) != len(q.Networks) {
			return nil, core.Invalid("network", "authorization mapping required")
		}
		for i, n := range q.NetworkAuthorizations {
			if n.Source != q.Networks[i] {
				return nil, denied()
			}
			if platform.NativeSandbox() {
				if n.Source != "preset:network" || n.Constraints != nil || n.GroupConstraints != nil {
					return nil, denied()
				}
				if req.NetworkMode == "none" {
					return nil, denied()
				}
				if n.ExpiresAt != nil {
					if !n.ExpiresAt.After(time.Now()) {
						return nil, denied()
					}
					if c.AuthorizationDeadline.IsZero() || n.ExpiresAt.Before(c.AuthorizationDeadline) {
						c.AuthorizationDeadline = *n.ExpiresAt
					}
				}
			} else if n.Constraints == nil {
				return nil, denied()
			} else if n.Constraints.Bytes > s.ComputeCeilings().NetworkBytes || n.Constraints.Connections > s.ComputeCeilings().NetworkConnections {
				return nil, core.Invalid("network", "quota exceeds deployment ceiling")
			}
		}
		b.networkQuotas.Store(shellKey(q.Identity.Execution()), q.NetworkAuthorizations)
		defer b.networkQuotas.Delete(shellKey(q.Identity.Execution()))
		if q.GroupComputeConstraints != nil {
			if err := applyComputeConstraints(&platform.Command{}, q.GroupComputeConstraints, s.ComputeCeilings()); err != nil {
				return nil, err
			}
		}
		if err := applyComputeConstraints(&c, q.ComputeConstraints, s.ComputeCeilings()); err != nil {
			return nil, err
		}
		if v := q.GroupComputeConstraints; v != nil {
			if err := core.CheckGPUDevices(c.GPUDeviceIDs, v.GPUDevices()); err != nil {
				return nil, err
			}
			c.Limits.CPUPercent = min(c.Limits.CPUPercent, v.CPUPercent)
			c.Limits.MemoryBytes = min(c.Limits.MemoryBytes, v.MemoryBytes)
			c.Limits.Processes = min(c.Limits.Processes, v.Processes)
			c.Limits.TempBytes = min(c.Limits.TempBytes, v.TempBytes)
			if !v.UnlimitedDuration {
				d := time.Duration(v.MaxDurationSeconds) * time.Second
				if c.Timeout == 0 || c.Timeout > d {
					c.Timeout = d
				}
			}
		}
		if c.Directory == "" {
			c.Directory = "/tmp"
		}
		for _, mount := range req.Files {
			actions := []policy.Action{policy.Read}
			if mount.Write {
				actions = append(actions, policy.Write)
			}
			root, name, err := b.resolve(ctx, q.Identity.Execution(), tools.FileTarget{ResourceID: mount.ResourceID, Path: mount.Path}, actions...)
			if err != nil {
				return nil, err
			}
			dest := resourceShellPath(mount.ResourceID, mount.Path)
			for _, old := range c.Mounts {
				if dest == old.Destination || strings.HasPrefix(dest, old.Destination+"/") || strings.HasPrefix(old.Destination, dest+"/") {
					return nil, core.Invalid("mounts", "overlapping scopes")
				}
			}
			c.Mounts = append(c.Mounts, platform.Mount{Source: filepath.Join(root, name), Destination: dest, ReadOnly: !mount.Write})
		}
		for _, name := range q.Networks {
			found := false
			for _, n := range s.NetworkTargets(b.cfg.OwnListenPort()) {
				if n.Name == name {
					if platform.NativeSandbox() {
						c.NetworkAccess = true
					} else {
						c.NetworkTargets = append(c.NetworkTargets, n.Target())
					}
					found = true
				}
			}
			if !found {
				return nil, denied()
			}
		}
	} else {
		c.RequireIsolation = false
		c.Limits = platform.Limits{}
		if c.MaxOutputBytes < 1 || c.MaxOutputBytes > 128<<10 {
			c.MaxOutputBytes = 64 << 10
		}
	}
	root, err := b.prepareOperationSpool(q.Identity)
	if err != nil {
		return nil, err
	}
	limit := s.MaxFileBytes
	if q.FullAccess || platform.NativeSandbox() {
		limit = 64 << 20
	}
	stdoutFile, stderrFile, stdoutPath, stderrPath, err := createShellTaskOutputs(root, limit, q.Identity.Execution())
	if err != nil {
		return nil, err
	}
	defer stdoutFile.close()
	defer stderrFile.close()
	c.StdoutSink, c.StderrSink = stdoutFile, stderrFile
	captureCtx, captureCancel := context.WithCancel(ctx)
	defer captureCancel()
	stdoutFile.budget.cancel = captureCancel
	p, err := b.runner.Start(captureCtx, q.Identity.Execution(), c)
	if err != nil {
		return nil, err
	}
	defer b.shellOutputs.Delete(shellKey(q.Identity.Execution()))
	b.shellOutputs.Store(shellKey(q.Identity.Execution()), shellRemoteOutput{Identity: q.Identity, StdoutPath: stdoutPath, StderrPath: stderrPath})
	var stdout, stderr []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); stdout, _ = io.ReadAll(p.Stdout()) }()
	go func() { defer wg.Done(); stderr, _ = io.ReadAll(p.Stderr()) }()
	exit, err := p.Wait(ctx)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = p.Cancel(cleanup)
		exit, err = p.Wait(cleanup)
		cancel()
	}
	wg.Wait()
	if err != nil {
		return nil, err
	}
	stdoutFile.close()
	stderrFile.close()
	return shellOutputResult(stdoutFile, stderrFile, stdoutPath, stderrPath, exit, stdout, stderr)
}
func (b *remoteSandboxBackend) Cancel(ctx context.Context, i sm.Identity) error {
	return b.runner.CancelOperation(ctx, i.Execution())
}
func (b *remoteSandboxBackend) Close() error { return b.runner.Close() }

func (b *remoteSandboxBackend) DialForward(ctx context.Context, id sm.Identity, port int) (net.Conn, error) {
	return b.runner.DialForward(ctx, id.Execution(), port)
}
