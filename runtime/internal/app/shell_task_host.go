package app

import (
	"context"
	"crypto/cipher"
	"molr.ai/runtime/internal/persistence"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"runtime"

	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/portforward"
)

// Session controllers depend on capabilities, not the router's leases, storage
// configuration or local backend. This adapter is the composition boundary.
type shellTaskEnvironment struct{ router *sandboxRouter }

func (h shellTaskEnvironment) DialForward(ctx context.Context, e core.Execution, id core.ID, port int) (portforward.Stream, error) {
	return h.router.dialForward(ctx, e, id, port)
}
func (h shellTaskEnvironment) CaptureLogs(ctx context.Context, p core.Principal, task, node core.ID) error {
	target, err := persistence.Load[struct {
		SandboxID core.ID
		Identity  sm.Identity
	}](ctx, h.router.adapters.store, "sandbox.operation_targets", persistence.Key(p.OwnerID, task))
	if err != nil {
		return err
	}
	return h.router.mirrorShellLogs(ctx, target.Identity.Execution(), node)
}
func (h shellTaskEnvironment) Workspace(owner, assistant core.ID) string {
	return platform.AssistantWorkspace(h.router.adapters.dataDir, h.router.adapters.storageConfig.WorkRoot, owner, assistant)
}
func (h shellTaskEnvironment) BrowserSandboxAvailable() bool {
	return h.router != nil && h.router.shell != nil && (h.router.shell.config.UsesHostTools() || runtime.GOOS == "darwin")
}
func (h shellTaskEnvironment) BrowserSupported(fullAccess bool) bool {
	return h.router.shell != nil && (fullAccess || h.BrowserSandboxAvailable()) && (runtime.GOOS == "linux" || runtime.GOOS == "darwin")
}
func (h shellTaskEnvironment) AuthorizeSession(ctx context.Context, e core.Execution) error {
	lease, ok := h.router.shell.leases.Load(shellKey(e))
	if !ok {
		return denied()
	}
	return h.router.shell.authorize(ctx, e, lease.(shellLease).plan)
}
func (h shellTaskEnvironment) RecordAudit(ctx context.Context, record audit.Record) error {
	return h.router.shell.audit.Record(ctx, record)
}
func (h shellTaskEnvironment) ProxyCipher() cipher.AEAD { return h.router.cipher }
func (h shellTaskEnvironment) VerifyProxy(ctx context.Context, p core.Principal, verification auth.Verification) error {
	if h.router.verify == nil {
		return nil
	}
	return h.router.verify(ctx, p, verification, "credential.put")
}

func (h shellTaskEnvironment) Execute(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
	return h.router.shellExecute(ctx, e, q)
}
func (h shellTaskEnvironment) CheckExecution(ctx context.Context, e core.Execution) error {
	return (access{h.router.adapters.store}).execution(ctx, e)
}
func (h shellTaskEnvironment) CheckAssistant(ctx context.Context, p core.Principal, id core.ID) error {
	return (access{h.router.adapters.store}).assistant(ctx, p, id)
}
func (h shellTaskEnvironment) AuthorizeForward(ctx context.Context, p core.Principal) error {
	g, err := groupPolicy(ctx, h.router.adapters.store, p.OwnerID)
	if err != nil {
		return err
	}
	if !g.Administrator {
		return core.WithReason(core.NewError(core.Forbidden, "public port forwarding requires administrator"), "group_port_forward_unavailable")
	}
	return nil
}
func (h shellTaskEnvironment) ResolveCompute(ctx context.Context, p core.Principal, compute, node core.ID) (core.ID, error) {
	r, err := resource.NewRepository(h.router.adapters.store).Get(ctx, p.OwnerID, compute)
	if err != nil {
		return "", err
	}
	id := targetSandbox(node)
	if sandboxOf(r) != id {
		return "", core.Invalid("sandbox_id", "explicit sandbox_id must match the compute resource node")
	}
	return id, nil
}
func (h shellTaskEnvironment) ConcurrencyLimit(ctx context.Context, owner, node core.ID) (int, error) {
	ref, err := groupReference(ctx, h.router.adapters.store, owner, "sandbox", node)
	if err != nil || ref == nil {
		return 0, err
	}
	return ref.Quota.Concurrent, nil
}
