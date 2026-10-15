package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
	"molr.ai/runtime/internal/resourcequota"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"time"
)

type quotaUsage struct {
	Network     *quota.NetworkUsage           `json:",omitempty"`
	Compute     *sandbox.ComputeUsage         `json:",omitempty"`
	Storage     *resourcequota.WorkspaceUsage `json:",omitempty"`
	Constraints *policy.Constraints           `json:",omitempty"`
}

// Keep the existing first-by-key choice while loading only its current grant.
func quotaGrantQuery(owner, assistant, resource core.ID, now time.Time) persistence.Query {
	query := persistence.Query{Where: []persistence.Filter{
		persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistant),
		persistence.Eq("ResourceID", resource), persistence.Eq("RevokedAt", nil),
	}, Limit: 1}
	// FullAccess=false is normally omitted in stored Scope JSON. Accept explicit
	// false too; either representation must satisfy the expiry condition.
	for _, fullAccess := range []any{nil, false} {
		for _, expiry := range []persistence.Filter{
			persistence.Eq("ExpiresAtKey", ""),
			{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(now)},
		} {
			query.Any = append(query.Any, []persistence.Filter{persistence.Eq("Scope.FullAccess", fullAccess), expiry})
		}
	}
	return query
}

func (s *sandboxRouter) quotaUsage(ctx context.Context, p core.Principal, assistantID, resourceID core.ID) (quotaUsage, error) {
	var out quotaUsage
	if err := (access{s.adapters.store}).assistant(ctx, p, assistantID); err != nil {
		return out, err
	}
	r, err := resource.NewRepository(s.adapters.store).Get(ctx, p.OwnerID, resourceID)
	if err != nil {
		return out, err
	}
	now := time.Now()
	rows, err := persistence.Select[policy.Grant](ctx, s.adapters.store, "security.grants", quotaGrantQuery(p.OwnerID, assistantID, resourceID, now))
	if err != nil {
		return out, err
	}
	var grant *policy.Grant
	for i := range rows {
		g := &rows[i]
		if g.OwnerID == p.OwnerID && g.AssistantID == assistantID && g.ResourceID == resourceID && !g.Scope.FullAccess && g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(now)) {
			grant = g
			break
		}
	}
	if grant != nil {
		out.Constraints = grant.EffectiveConstraints(now)
	}
	q := sm.Request{Identity: sm.Identity{OwnerID: p.OwnerID, AssistantID: assistantID, ExecutionID: randomOperation(), OperationID: randomOperation()}, Method: "quota_usage", Arguments: json.RawMessage(`{}`), Deadline: time.Now().Add(time.Minute)}
	q.Arguments, _ = json.Marshal(struct{ Workspace bool }{r.Provider == "managed-workspace"})
	if out.Constraints != nil {
		q.ComputeConstraints = out.Constraints.Compute
	}
	if r.Kind == resource.Network && out.Constraints != nil && out.Constraints.Network != nil {
		b, err := resource.NewRepository(s.adapters.store).Binding(ctx, p.OwnerID, r.ID)
		if err != nil {
			return out, err
		}
		source := b.SandboxResource
		if sandboxOf(r) == "local" {
			source = b.SandboxTarget
		}
		q.NetworkAuthorizations = []platform.NetworkAuthorization{{Source: source, Constraints: out.Constraints.Network}}
	}
	if sandboxOf(r) == "local" {
		if s.shell == nil {
			return out, core.NewError(core.Unavailable, "sandbox disabled")
		}
		switch r.Kind {
		case resource.Network:
			if len(q.NetworkAuthorizations) > 0 {
				n := q.NetworkAuthorizations[0]
				v, err := s.shell.runner.NetworkUsage(ctx, p.OwnerID, assistantID, n.Source, *n.Constraints)
				out.Network = &v
				return out, err
			}
		case resource.Compute:
			if !platform.NativeSandbox() && out.Constraints != nil && out.Constraints.Compute != nil {
				v := s.shell.runner.ComputeUsage(ctx, p.OwnerID, assistantID)
				out.Compute = &v
			}
		case resource.Storage:
			if r.Provider == "managed-workspace" {
				v, _ := resourcequota.WorkspaceStatus(platform.AssistantWorkspace(s.adapters.dataDir, s.adapters.storageConfig.WorkRoot, p.OwnerID, assistantID))
				out.Storage = &v
			}
		}
		return out, nil
	}
	c, err := s.connection(ctx, p, sandboxOf(r))
	if err != nil {
		return out, err
	}
	client, err := s.client(ctx, p, c)
	if err != nil {
		return out, err
	}
	q.Epoch, err = s.epoch(ctx, p.OwnerID)
	if err != nil {
		return out, err
	}
	raw, err := client.QuotaUsage(ctx, q)
	if err != nil {
		return out, err
	}
	var usage quotaUsage
	if err = json.Unmarshal(raw, &usage); err != nil {
		return out, err
	}
	usage.Constraints = out.Constraints
	return usage, nil
}
func (b *remoteSandboxBackend) quotaUsage(ctx context.Context, q sm.Request) (json.RawMessage, error) {
	out := quotaUsage{}
	if len(q.NetworkAuthorizations) > 0 {
		n := q.NetworkAuthorizations[0]
		if n.Constraints == nil {
			return nil, denied()
		}
		v, err := b.runner.NetworkUsage(ctx, q.Identity.OwnerID, q.Identity.AssistantID, n.Source, *n.Constraints)
		if err != nil {
			return nil, err
		}
		out.Network = &v
	}
	if q.ComputeConstraints != nil {
		v := b.runner.ComputeUsage(ctx, q.Identity.OwnerID, q.Identity.AssistantID)
		out.Compute = &v
	}
	var target struct{ Workspace bool }
	if err := json.Unmarshal(q.Arguments, &target); err != nil {
		return nil, err
	}
	if target.Workspace {
		return nil, core.Invalid("workspace", "remote computers have no assistant workspace")
	}
	return json.Marshal(out)
}
