package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

type shellLease struct {
	request tools.ShellRequest
	plan    platform.Command
}
type shellService struct {
	audit   audit.Recorder
	runner  *sandbox.Runner
	store   persistence.Store
	policy  policy.Authorizer
	config  config.Sandbox
	ownPort uint16
	dataDir string
	gate    sync.Mutex
	leases  sync.Map
}

func newShellService(store persistence.Store, p policy.Authorizer, c config.Config) (*shellService, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	s := &shellService{store: store, policy: p, config: c.Sandbox, ownPort: c.OwnListenPort(), dataDir: c.DataDir}
	s.runner = sandbox.New(sandbox.Options{ProtectedRoots: c.Sandbox.ProtectedRoots, ToolDirectories: c.Sandbox.ToolDirectories, QuotaStore: store, GPU: c.Sandbox.GPU, AllowFullAccess: c.Sandbox.AllowFullAccess, HostTools: c.Sandbox.UsesHostTools(), Bwrap: c.Sandbox.Bwrap, RootFS: c.Sandbox.RootFS, Helper: exe, CgroupRoot: c.Sandbox.CgroupRoot, WorkRoot: c.Sandbox.WorkRoot, PrivateRoot: c.DataDir, MinFreeDiskBytes: c.Sandbox.MinFreeDiskBytes, NetworkBytes: c.Sandbox.NetworkBytes, NetworkConnections: c.Sandbox.NetworkConnections, NetworkAuthorization: s.networkAuthorization, Authorize: s.authorize})
	return s, nil
}
func shellKey(e core.Execution) string {
	return string(e.Principal.OwnerID) + "/" + string(e.OperationID)
}
func (s *shellService) authorize(ctx context.Context, e core.Execution, c platform.Command) error {
	v, ok := s.leases.Load(shellKey(e))
	if !ok {
		return denied()
	}
	lease := v.(shellLease)
	request := lease.request
	if !platform.NativeSandbox() {
		request.NetworkIDs = nil
	}
	plan, err := s.plan(ctx, e, request)
	if err != nil {
		return err
	}
	if c.StdoutSink != lease.plan.StdoutSink || c.StderrSink != lease.plan.StderrSink {
		return denied()
	}
	c.StdoutSink, c.StderrSink = nil, nil
	if !platform.NativeSandbox() {
		c.NetworkTargets = nil
		c.NetworkAuthorizations = nil
	}
	if !reflect.DeepEqual(plan, c) {
		return denied()
	}
	return nil
}
func (s *shellService) plan(ctx context.Context, e core.Execution, req tools.ShellRequest) (platform.Command, error) {
	c := platform.Command{FullAccess: req.FullAccess, ForwardPorts: req.ForwardPorts, Script: req.Script, Directory: req.Directory, RequireIsolation: true, Timeout: s.config.Timeout.Value(), MaxOutputBytes: s.config.MaxOutputBytes, Limits: platform.Limits{MemoryBytes: s.config.MemoryBytes, Processes: s.config.Processes, CPUPercent: s.config.CPUPercent, TempBytes: s.config.TempBytes, MaxFileBytes: s.config.MaxFileBytes}}
	if managedShellFor(ctx, e) != nil {
		c.Managed = true
		c.TTY = req.TTY
		c.Timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	if req.Browser != nil {
		c.BrowserAssets = true
		c.BrowserProxy = &req.Browser.Proxy
		if req.FullAccess || req.Script != "exec /molr/helper sandbox-helper browser" {
			return c, denied()
		}
		c.Mounts = append(c.Mounts, platform.Mount{Source: req.Browser.AgentDirectory, Destination: "/resources/browser-agent", ReadOnly: true}, platform.Mount{Source: req.Browser.ChromiumDirectory, Destination: "/resources/browser-chromium", ReadOnly: true})
	}
	if c.Directory == "" {
		c.Directory = "/work"
	}
	err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := (access{s.store}).execution(ctx, e); err != nil {
			return err
		}
		if managedShellFor(ctx, e) == nil {
			if op, err := persistence.Load[operation.Operation](ctx, s.store, "agent.operations", persistence.Key(e.Principal.OwnerID, e.OperationID)); err == nil && op.State != operation.Running {
				return denied()
			} else if err != nil && !persistence.Missing(err) {
				return err
			}
			if status, err := persistence.Load[agent.Status](ctx, s.store, "agent.status", persistence.Key(e.Principal.OwnerID, e.AssistantID)); err == nil && (status.State == agent.Paused || status.State == agent.Stopped) {
				return denied()
			} else if err != nil && !persistence.Missing(err) {
				return err
			}
		}
		repo := resource.NewRepository(s.store)
		require := func(id core.ID, action policy.Action, destination string) error {
			return s.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: action, FullAccess: req.FullAccess, Destination: destination})
		}
		if req.FullAccess && !s.config.AllowFullAccess {
			return core.NewError(core.Forbidden, "Full Access disabled by deployment")
		}
		if err := require(req.ComputeID, policy.Execute, "/"); err != nil {
			return err
		}
		compute, err := repo.Get(ctx, e.Principal.OwnerID, req.ComputeID)
		if err != nil {
			return err
		}
		if compute.Provider != "local-sandbox" || compute.Kind != resource.Compute {
			return denied()
		}
		if !req.FullAccess {
			cg, err := executionGrant(ctx, s.store, e, req.ComputeID)
			if err != nil {
				return err
			}
			limits := cg.EffectiveConstraints(time.Now())
			if limits == nil || limits.Compute == nil {
				return denied()
			}
			if err := applyComputeConstraints(&c, limits.Compute, s.config.ComputeCeilings()); err != nil {
				return err
			}
			if err := applyGroupCommand(ctx, s.store, e, "local", &c); err != nil {
				return err
			}
			a := adapters{store: s.store, policy: s.policy, dataDir: s.dataDir, storageConfig: s.config, ownPort: s.ownPort}
			destinations := []string{}
			for _, f := range req.Files {
				if isWorkspaceResource(ctx, s.store, e.Principal.OwnerID, f.ResourceID, "local") {
					continue
				}
				name, err := resourcePath(f.Path)
				if err != nil {
					return err
				}
				b, err := a.storageBinding(ctx, e.Principal, f.ResourceID, name, policy.Read)
				if err != nil {
					return err
				}
				if f.Write {
					if _, err := a.storageBinding(ctx, e.Principal, f.ResourceID, name, policy.Write); err != nil {
						return err
					}
				}
				destination := resourceShellPath(f.ResourceID, name)
				for _, existing := range destinations {
					if destination == existing || strings.HasPrefix(destination, existing+"/") || strings.HasPrefix(existing, destination+"/") {
						return core.Invalid("files", "overlapping resource mounts; select one scope for each path")
					}
				}
				destinations = append(destinations, destination)
				c.Mounts = append(c.Mounts, platform.Mount{Source: filepath.Join(b.RootPath, strings.TrimPrefix(name, "/")), Destination: destination, ReadOnly: !f.Write})
			}
			for _, id := range req.NetworkIDs {
				if err := require(id, policy.Execute, "/"); err != nil {
					return err
				}
				r, err := repo.Get(ctx, e.Principal.OwnerID, id)
				if err != nil {
					return err
				}
				if r.Kind != resource.Network || r.Provider != "sandbox-network" {
					return denied()
				}
				b, err := repo.Binding(ctx, e.Principal.OwnerID, id)
				if err != nil {
					return err
				}
				found := false
				for _, n := range s.config.NetworkTargets(s.ownPort) {
					if n.Name == b.SandboxTarget {
						if platform.NativeSandbox() {
							if n.Name != "preset:network" {
								return denied()
							}
							c.NetworkAccess = true
						} else {
							c.NetworkTargets = append(c.NetworkTargets, n.Target())
						}
						ng, err := executionGrant(ctx, s.store, e, id)
						if err != nil {
							return err
						}
						na := platform.NetworkAuthorization{Source: n.Name, GrantID: ng.ID, ExpiresAt: ng.ExpiresAt}
						if limits := ng.EffectiveConstraints(time.Now()); limits != nil {
							na.Constraints = limits.Network
						}
						na, err = groupNetworkAuthorization(ctx, s.store, e.Principal.OwnerID, "local", na)
						if err != nil {
							return err
						}
						c.NetworkAuthorizations = append(c.NetworkAuthorizations, na)
						found = true
						break
					}
				}
				if !found {
					return denied()
				}
			}
		}
		grants, err := persistence.Select[policy.Grant](ctx, s.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(time.Now())}}}})
		if err != nil {
			return err
		}
		resources := map[core.ID]bool{req.ComputeID: true}
		if platform.NativeSandbox() {
			for _, id := range req.NetworkIDs {
				resources[id] = true
			}
		}
		for _, f := range req.Files {
			resources[f.ResourceID] = true
		}
		for _, g := range grants {
			if g.OwnerID == e.Principal.OwnerID && g.AssistantID == e.AssistantID && resources[g.ResourceID] && g.Scope.FullAccess == req.FullAccess && g.RevokedAt == nil && g.ExpiresAt != nil && g.ExpiresAt.After(time.Now()) && (c.AuthorizationDeadline.IsZero() || g.ExpiresAt.Before(c.AuthorizationDeadline)) {
				c.AuthorizationDeadline = *g.ExpiresAt
			}
		}
		return nil
	})
	if err == nil && !req.FullAccess && !platform.NativeSandbox() {
		c.WorkspaceConstraints, err = s.workspaceQuota(ctx, e)
		if c.WorkspaceConstraints != nil {
			c.Limits.MaxFileBytes = c.WorkspaceConstraints.MaxFileBytes
		}
	}
	if req.FullAccess {
		c.RequireIsolation = false
		c.Timeout = time.Duration(req.TimeoutSeconds) * time.Second
		c.Limits = platform.Limits{}
		if c.MaxOutputBytes < 1 || c.MaxOutputBytes > 128<<10 {
			c.MaxOutputBytes = 64 << 10
		}
	}
	return c, err
}
func (s *shellService) cancel(ctx context.Context, e core.Execution, id core.ID) error {
	if id != e.OperationID {
		return denied()
	}
	return s.runner.CancelOperation(ctx, e)
}
func (s *shellService) execute(ctx context.Context, e core.Execution, req tools.ShellRequest) (json.RawMessage, error) {
	s.gate.Lock()
	c, err := s.plan(ctx, e, req)
	if err != nil {
		s.gate.Unlock()
		return nil, err
	}
	k := shellKey(e)
	if _, loaded := s.leases.LoadOrStore(k, shellLease{req, c}); loaded {
		s.gate.Unlock()
		return nil, core.NewError(core.Conflict, "shell execution already registered")
	}
	defer s.leases.Delete(k)
	if err := s.record(ctx, e, "sandbox.start", audit.Succeeded, ""); err != nil {
		s.gate.Unlock()
		return nil, err
	}
	stdoutFile, stderrFile, stdoutPath, stderrPath, err := s.outputFiles(ctx, e, req.FullAccess)
	if err != nil {
		s.gate.Unlock()
		return nil, err
	}
	defer stdoutFile.close()
	defer stderrFile.close()
	c.StdoutSink, c.StderrSink = stdoutFile, stderrFile
	s.leases.Store(k, shellLease{req, c})
	captureCtx, captureCancel := context.WithCancel(ctx)
	defer captureCancel()
	stdoutFile.budget.cancel = captureCancel
	p, err := s.runner.Start(captureCtx, e, c)
	s.gate.Unlock()
	if err != nil {
		_ = s.record(ctx, e, "sandbox.finish", audit.Failed, string(core.ReasonOf(err)))
		return nil, err
	}
	if scope := managedShellFor(ctx, e); scope != nil {
		if observer, ok := p.(interface {
			NetworkDiagnostic() platform.NetworkDiagnostic
		}); ok && scope.observe != nil {
			scope.observe(observer.NetworkDiagnostic)
		}
		root := platform.AssistantWorkspace(s.dataDir, s.config.WorkRoot, e.Principal.OwnerID, e.AssistantID)
		err = scope.started(stdoutPath, stderrPath, c.GPUDeviceIDs, func(ctx context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
			if q.Action == "read" {
				return readShellOutput(root, stdoutPath, stderrPath, q)
			}
			data := q.Data
			if q.Action == "interrupt" {
				data = "\x03"
			}
			err := s.runner.Terminal(ctx, e, data, q.Rows, q.Cols)
			return json.RawMessage(`{"Accepted":true}`), err
		})
		if err != nil {
			_ = p.Cancel(context.Background())
			return nil, err
		}
	}
	// Readers drain concurrently; neither stream can block the other.
	var stdout, stderr []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); stdout, _ = io.ReadAll(p.Stdout()) }()
	go func() { defer wg.Done(); stderr, _ = io.ReadAll(p.Stderr()) }()
	exit, err := p.Wait(ctx)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanErr := p.Cancel(cleanup); cleanErr != nil {
			return nil, cleanErr
		}
		exit, err = p.Wait(cleanup)
	}
	wg.Wait()
	if err != nil {
		return nil, err
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	outcome := audit.Succeeded
	if exit.Code != 0 {
		outcome = audit.Failed
	}
	if err := s.record(persist, e, "sandbox.finish", outcome, exit.Reason); err != nil {
		return nil, err
	}
	stdoutFile.close()
	stderrFile.close()
	return shellOutputResult(stdoutFile, stderrFile, stdoutPath, stderrPath, exit, stdout, stderr)
}
func (s *shellService) record(ctx context.Context, e core.Execution, action string, outcome audit.Outcome, reason string) error {
	if s.audit == nil {
		return core.NewError(core.FailedPrecondition, "sandbox audit unavailable")
	}
	hash := sha256.Sum256([]byte(string(e.Principal.OwnerID) + "/" + string(e.OperationID) + "/" + action))
	return s.audit.Record(ctx, audit.Record{ID: core.ID(hex.EncodeToString(hash[:])), OwnerID: e.Principal.OwnerID, SubjectID: e.AssistantID, ActorKind: core.ActorAssistant, Action: action, Target: core.Object{Kind: "operation", ID: e.OperationID}, Outcome: outcome, ReasonCode: reason, At: time.Now()})
}

type sandboxGrants struct {
	policy.Service
	shell *shellService
}

func (s sandboxGrants) Revoke(ctx context.Context, p core.Principal, r policy.RevokeRequest) error {
	s.shell.gate.Lock()
	defer s.shell.gate.Unlock()
	if err := s.Service.Revoke(ctx, p, r); err != nil {
		return err
	}
	g, err := persistence.Load[policy.Grant](ctx, s.shell.store, "security.grants", persistence.Key(p.OwnerID, r.GrantID))
	if err == nil && g.Scope.FullAccess {
		return s.shell.runner.StopAccess(ctx, p.OwnerID, g.AssistantID, true)
	}
	if err == nil {
		res, err := resource.NewRepository(s.shell.store).Get(ctx, p.OwnerID, g.ResourceID)
		if err == nil && res.Kind == resource.Network {
			return nil
		}
	}
	return s.stop(p.OwnerID)
}
func (s sandboxGrants) UpdateGrant(ctx context.Context, p core.Principal, r policy.UpdateGrantRequest) (policy.Grant, error) {
	s.shell.gate.Lock()
	defer s.shell.gate.Unlock()
	previous, loadErr := persistence.Load[policy.Grant](ctx, s.shell.store, "security.grants", persistence.Key(p.OwnerID, r.GrantID))
	if loadErr != nil {
		return policy.Grant{}, loadErr
	}
	g, err := s.Service.UpdateGrant(ctx, p, r)
	if err != nil {
		return g, err
	}
	if batch, _ := ctx.Value(permissionBatchKey{}).(bool); batch {
		return g, nil
	}
	res, loadErr := resource.NewRepository(s.shell.store).Get(ctx, p.OwnerID, g.ResourceID)
	if loadErr == nil && res.Kind == resource.Network {
		return g, nil
	}
	return g, s.shell.runner.StopAccess(ctx, p.OwnerID, g.AssistantID, previous.Scope.FullAccess)
}
func (s sandboxGrants) stop(owner core.ID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.shell.runner.StopOwner(ctx, owner)
}

type sandboxResources struct {
	resource.Service
	shell *shellService
}

func (s sandboxResources) Disconnect(ctx context.Context, p core.Principal, r resource.DisconnectRequest) error {
	s.shell.gate.Lock()
	defer s.shell.gate.Unlock()
	if err := s.Service.Disconnect(ctx, p, r); err != nil {
		return err
	}
	return sandboxGrants{shell: s.shell}.stop(p.OwnerID)
}
func (s *shellService) inspect(ctx context.Context, c *agent.Capabilities) {
	caps, _ := s.runner.Capabilities(ctx)
	for i := range c.Tools {
		if c.Tools[i].Name == "shell_execute" && !caps.Shell && !caps.FullAccess {
			c.Tools[i].Available = false
			c.Tools[i].Reason = caps.Reason
		}
	}
}
func (s *shellService) available(ctx context.Context, p core.Principal) (bool, error) {
	caps, err := s.runner.Capabilities(ctx)
	if err != nil || (!caps.Shell && !caps.FullAccess) {
		return false, err
	}
	resources, err := catalog.Select[resource.Resource](ctx, s.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Compute), persistence.Eq("Provider", "local-sandbox"), persistence.Eq("State", resource.Available)}})
	if err != nil {
		return false, err
	}
	for _, r := range resources {
		if r.OwnerID == catalog.Scope && r.Kind == resource.Compute && r.Provider == "local-sandbox" && r.State == resource.Available {
			if computeAccess(ctx, s.policy, p, r.ID, caps) {
				return true, nil
			}
		}
	}
	return false, nil
}
func (s *shellService) startup(ctx context.Context) {
	budget := 20 * time.Second
	if s.config.GPU.Enabled {
		budget = 60 * time.Second
	}
	prepare, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := s.runner.Prepare(prepare); err != nil {
		log.Printf("Sandbox preparation: %s", core.ReasonOf(err))
	}
	if ctx.Err() != nil {
		return
	}
	if err := s.ensureCompute(ctx); err != nil {
		log.Printf("Sandbox resource initialization: %s", core.CodeOf(err))
	}
}

func (s *shellService) ensureCompute(ctx context.Context) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		account, err := auth.NewRepository(s.store).Account(ctx)
		if err != nil {
			return err
		}
		if err := ensureWorkspaceResource(ctx, s.store, account.Account.ID, "local"); err != nil {
			return err
		}
		if err := s.ensureNetworks(ctx); err != nil {
			return err
		}
		resources, err := catalog.Select[resource.Resource](ctx, s.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Provider", "local-sandbox")}, Limit: 1})
		if err != nil {
			return err
		}
		for _, r := range resources {
			if r.OwnerID == catalog.Scope && r.Provider == "local-sandbox" {
				return nil
			}
		}
		hash := sha256.Sum256([]byte(string(account.Account.ID) + "/local-sandbox"))
		r := resource.Resource{ID: core.ID(hex.EncodeToString(hash[:])), OwnerID: account.Account.ID, Kind: resource.Compute, Provider: "local-sandbox", Name: "Local compute", State: resource.Available, Actions: []string{"execute"}, Version: 1}
		return resource.NewRepository(s.store).Insert(ctx, r, resource.Binding{ResourceID: r.ID})
	})
}
func sandboxProviders(c config.Config) map[string]resource.Provider {
	s := c.Sandbox
	ownPort := c.OwnListenPort()
	empty := func(b resource.Binding) bool {
		return b.SandboxID == "" && b.SandboxResource == "" && b.Endpoint == "" && b.RootPath == "" && b.CredentialID == "" && len(b.Models) == 0 && len(b.AllowedOrigins) == 0 && b.PricingProvider == ""
	}
	check := func(context.Context, resource.Binding) (resource.Status, error) {
		return resource.Status{State: resource.Available, ReasonCode: "configured", CheckedAt: time.Now()}, nil
	}
	return map[string]resource.Provider{
		"managed-workspace": {Kind: resource.Storage, Validate: func(ctx context.Context, b resource.Binding) ([]string, error) {
			return nil, core.Invalid("binding", "workspace resources are managed by Runtime")
		}, Check: check},
		"local-sandbox": {Kind: resource.Compute, Validate: func(ctx context.Context, b resource.Binding) ([]string, error) {
			if !empty(b) || b.SandboxTarget != "" {
				return nil, core.Invalid("binding", "compute uses deployment configuration")
			}
			return []string{"execute"}, nil
		}, Check: check},
		"sandbox-network": {Kind: resource.Network, Validate: func(ctx context.Context, b resource.Binding) ([]string, error) {
			if !empty(b) {
				return nil, core.Invalid("binding", "network uses named deployment target")
			}
			for _, n := range s.NetworkTargets(ownPort) {
				if n.Name == b.SandboxTarget {
					return []string{"execute"}, nil
				}
			}
			return nil, core.Invalid("sandbox_target", "unknown deployment target")
		}, Check: check},
	}
}
