package app

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"strings"
)

func (s *sandboxRouter) checkFullAccessCapability(ctx context.Context, p core.Principal, r resource.Resource) error {
	if r.Kind != resource.Compute {
		return core.Invalid("full_access", "Full Access is available only for compute resources")
	}
	if err := groupFullAccess(ctx, s.adapters.store, p.OwnerID, sandboxOf(r)); err != nil {
		return err
	}
	info, err := s.info(ctx, p, sandboxOf(r))
	if err != nil {
		return err
	}
	if !info.Enabled || !info.Capabilities.FullAccess {
		return core.Invalid("full_access", "deployment does not allow Full Access")
	}
	return nil
}
func computeAccess(ctx context.Context, auth policy.Authorizer, p core.Principal, id core.ID, caps platform.Capabilities) bool {
	for _, full := range []bool{false, true} {
		if (!full && !caps.Shell) || (full && !caps.FullAccess) {
			continue
		}
		if auth.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Execute, FullAccess: full, Destination: "/"}) == nil {
			return true
		}
	}
	return false
}

// Full access still requires permission to use the execution node. That one
// permission covers commands, all filesystem paths and the environment network.
func (s *sandboxRouter) requireFullAccess(ctx context.Context, e core.Execution, id core.ID) error {
	allowed := s.adapters.storageConfig.Enabled && s.adapters.storageConfig.AllowFullAccess
	if id != "local" {
		info, err := s.info(ctx, e.Principal, id)
		if err != nil {
			return err
		}
		allowed = info.Enabled && info.Capabilities.FullAccess
	}
	if !allowed {
		return core.WithReason(core.NewError(core.Forbidden, "Full Access is unavailable on this node"), "full_access_unavailable")
	}
	if err := groupFullAccess(ctx, s.adapters.store, e.Principal.OwnerID, id); err != nil {
		return err
	}
	rows, err := catalog.Select[resource.Resource](ctx, s.adapters.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Compute), persistence.Eq("State", resource.Available)}})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.OwnerID == catalog.Scope && r.Kind == resource.Compute && sandboxOf(r) == id && r.State == resource.Available && s.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: r.ID}, ResourceID: r.ID, Action: policy.Execute, FullAccess: true, Destination: "/"}) == nil {
			return nil
		}
	}
	return denied()
}
func directFilePath(data, work string, e core.Execution, t tools.FileTarget) (string, string, error) {
	root := platform.AssistantWorkspace(data, work, e.Principal.OwnerID, e.AssistantID)
	if err := platform.EnsureWorkspace(root); err != nil {
		return "", "", err
	}
	name := t.Path
	if name == "" || name == "." || name == "/work" {
		return root, ".", nil
	}
	if strings.HasPrefix(name, "/work/") {
		name = filepath.Join(root, strings.TrimPrefix(name, "/work/"))
	} else if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	if strings.ContainsRune(name, 0) {
		return "", "", core.Invalid("path", "invalid path")
	}
	if actual, err := filepath.EvalSymlinks(name); err == nil {
		name = actual
	}
	if volume := filepath.VolumeName(name); volume != "" {
		relative := strings.TrimLeft(strings.TrimPrefix(filepath.Clean(name), volume), `/\`)
		if relative == "" {
			relative = "."
		}
		return volume + string(filepath.Separator), relative, nil
	}
	name = strings.TrimPrefix(filepath.Clean(name), "/")
	if name == "" {
		name = "."
	}
	return "/", name, nil
}
