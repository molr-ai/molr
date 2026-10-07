package app

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func (s *sandboxRouter) info(ctx context.Context, p core.Principal, id core.ID) (sm.Info, error) {
	if id == "local" {
		return s.localInfo(ctx), nil
	}
	c, err := s.connection(ctx, p, id)
	if err != nil {
		return sm.Info{}, err
	}
	client, err := s.client(ctx, p, c)
	if err != nil {
		return sm.Info{}, err
	}
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.Info(probe)
}
func (s *sandboxRouter) list(ctx context.Context, p core.Principal) (json.RawMessage, error) {
	catalog := resourceCatalog{adapters: s.adapters, shell: s.shell}
	raw, err := catalog.list(ctx, p)
	if err != nil {
		return nil, err
	}
	var out resourceListing
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	infos := map[core.ID]sm.Info{}
	names := map[core.ID]string{"local": "Local sandbox"}
	load := func(id core.ID) sm.Info {
		if v, ok := infos[id]; ok {
			return v
		}
		v, err := s.info(ctx, p, id)
		if err != nil {
			v = sm.Info{Capabilities: platform.Capabilities{Reason: "sandbox_offline"}}
		}
		if groupFullAccess(ctx, s.adapters.store, p.OwnerID, id) != nil {
			v.Capabilities.FullAccess = false
		}
		infos[id] = v
		if id != "local" {
			c, err := s.connection(ctx, p, id)
			if err == nil {
				names[id] = c.View.Name
			}
		}
		return v
	}
	load("local")
	for i := range out.Requestable {
		item := &out.Requestable[i]
		if item.Kind == resource.Compute || item.Kind == resource.Network || item.Kind == resource.Storage {
			info := load(item.SandboxID)
			item.FullAccessAvailable = item.Kind == resource.Compute && info.Capabilities.FullAccess
			item.AuthorizationCeilings = &info.AuthorizationCeilings
			item.WorkspaceQuotaSupported = false
			if item.Workspace {
				usage, err := s.quotaUsage(ctx, p, p.SubjectID, item.ResourceID)
				item.WorkspaceQuotaSupported = err == nil && info.WorkspaceProjectQuotas && usage.Storage != nil && usage.Storage.Supported
			}
		}
		if item.Kind != resource.Network {
			continue
		}
		binding, err := resource.NewRepository(s.adapters.store).Binding(ctx, p.OwnerID, item.ResourceID)
		if err != nil {
			return nil, err
		}
		targetID := binding.SandboxResource
		if item.SandboxID == "local" {
			targetID = binding.SandboxTarget
		}
		item.Network = describeNetwork(load(item.SandboxID), targetID)
	}
	compute := map[core.ID][]core.ID{}
	fullCompute := map[core.ID][]core.ID{}
	for i := range out.Resources {
		item := &out.Resources[i]
		if item.Kind == resource.Model || item.Kind == resource.Tool {
			continue
		}
		r, err := resource.NewRepository(s.adapters.store).Get(ctx, p.OwnerID, item.ResourceID)
		if err != nil {
			return nil, err
		}
		item.SandboxID = sandboxOf(r)
		info := load(item.SandboxID)
		usage, usageErr := s.quotaUsage(ctx, p, p.SubjectID, item.ResourceID)
		if usageErr == nil {
			item.Usage = &usage
		}
		if item.Kind == resource.Compute && info.Enabled && info.Capabilities.FullAccess && r.State == resource.Available {
			item.FullAccess = s.adapters.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: r.ID}, ResourceID: r.ID, Action: policy.Execute, FullAccess: true, Destination: "/"}) == nil
			if item.FullAccess {
				fullCompute[item.SandboxID] = append(fullCompute[item.SandboxID], r.ID)
			}
		}
		if item.Kind == resource.Compute {
			allowed, err := catalog.allowed(ctx, p, *item, policy.Execute)
			if err != nil {
				return nil, err
			}
			if allowed && r.State == resource.Available && info.Enabled && info.Capabilities.Shell {
				compute[item.SandboxID] = append(compute[item.SandboxID], r.ID)
			}
		}
	}
	for i := range out.Resources {
		item := &out.Resources[i]
		if item.SandboxID == "" {
			continue
		}
		info := infos[item.SandboxID]
		r, err := resource.NewRepository(s.adapters.store).Get(ctx, p.OwnerID, item.ResourceID)
		if err != nil {
			return nil, err
		}
		binding, err := resource.NewRepository(s.adapters.store).Binding(ctx, p.OwnerID, item.ResourceID)
		if err != nil {
			return nil, err
		}
		if item.Kind == resource.Network {
			targetID := binding.SandboxResource
			if item.SandboxID == "local" {
				targetID = binding.SandboxTarget
			}
			item.Network = describeNetwork(info, targetID)
		}
		for j := range item.Operations {
			op := &item.Operations[j]
			if item.SandboxID == "local" {
				if !info.Enabled {
					op.Available = false
					op.Reason = "sandbox_disabled"
				}
				continue
			}
			reason := ""
			allowed, err := catalog.allowed(ctx, p, *item, op.Action)
			if err != nil {
				return nil, err
			}
			if !allowed {
				reason = "permission_required"
			} else if r.State != resource.Available {
				reason = "resource_unavailable"
			} else if !info.Enabled {
				reason = "sandbox_offline"
			} else if item.Kind == resource.Storage {
				found := r.Provider == "managed-workspace"
				for _, d := range info.Directories {
					if d.ID == binding.SandboxResource {
						found = true
						if d.ReadOnly && op.Action == policy.Write {
							reason = "storage_read_only"
						}
					}
				}
				if !found || !info.Storage {
					reason = "storage_unavailable"
				}
			} else if item.Kind == resource.Network {
				found := false
				for _, n := range info.Networks {
					if n.ID == binding.SandboxResource {
						found = true
					}
				}
				if !found {
					reason = "network_target_unavailable"
				}
			}
			if reason == "" && op.Via == "shell_execute" {
				if !info.Capabilities.Shell {
					reason = info.Capabilities.Reason
				} else if len(compute[item.SandboxID]) == 0 {
					reason = "compute_permission_required"
				}
			}
			if reason == "" && (op.Via == "file_edit" || (op.Via == "shell_execute" && item.Kind == resource.Storage && op.Action == policy.Write)) {
				allowed, err := catalog.allowed(ctx, p, *item, policy.Read)
				if err != nil {
					return nil, err
				}
				if !allowed {
					reason = "read_permission_required"
				}
			}
			op.Reason = reason
			op.Available = reason == ""
		}
	}
	out.BuiltInStorage = []builtinStorage{}
	out.Sandboxes = []sandboxDescriptor{}
	// Stable ordering matters to automatic context and model cache reuse.
	ids := []core.ID{"local"}
	for _, item := range out.Resources {
		if item.SandboxID == "" {
			continue
		}
		seen := false
		for _, id := range ids {
			if id == item.SandboxID {
				seen = true
			}
		}
		if !seen {
			ids = append(ids, item.SandboxID)
		}
	}
	for _, id := range ids {
		info := infos[id]
		out.Sandboxes = append(out.Sandboxes, sandboxDescriptor{FullAccessComputeIDs: fullCompute[id], Description: "Ordinary calls remain isolated. FullAccessComputeIDs require explicit full_access:true for unrestricted shell/files; no resource limits apply to those calls. Full Access scripts use actual host paths; only local execution provides $MOLR_WORKSPACE. A Full Access grant never authorizes an ordinary compute call without its separate quota grant.", ID: id, Name: names[id], Local: id == "local", Capabilities: info.Capabilities, Storage: info.Storage})

		reason := ""
		if !info.Enabled {
			reason = "sandbox_offline"
		}
		if !info.Storage {
			reason = "storage_unavailable"
		}
		shellReason := reason
		if shellReason == "" && !info.Capabilities.Shell {
			shellReason = info.Capabilities.Reason
		}
		if shellReason == "" && len(compute[id]) == 0 {
			shellReason = "compute_permission_required"
		}
		ops := []resourceOperation{{policy.Read, "file_search", reason == "", reason}, {policy.Read, "file_read", reason == "", reason}, {policy.Read, "file_list", reason == "", reason}, {policy.Write, "file_write", reason == "", reason}, {policy.Write, "file_edit", reason == "", reason}, {policy.Execute, "shell_execute", shellReason == "", shellReason}}
		if id != "local" {
			continue
		}
		out.BuiltInStorage = append(out.BuiltInStorage, builtinStorage{SandboxID: id, Kind: "workspace", Source: "sandbox", Operations: ops, Path: "/work", Lifetime: "assistant", Available: reason == "", Reason: reason, Actions: []policy.Action{policy.Read, policy.Write}, ShellComputeIDs: compute[id]}, builtinStorage{SandboxID: id, Kind: "temporary", Source: "sandbox", Path: "/tmp", Lifetime: "execution", Available: shellReason == "", Reason: shellReason, ShellComputeIDs: compute[id]})
		if id == "local" {
			globalOps := []resourceOperation{}
			for _, op := range ops {
				if op.Action == policy.Read {
					globalOps = append(globalOps, op)
				}
			}
			out.BuiltInStorage = append(out.BuiltInStorage, builtinStorage{SandboxID: id, Kind: "global", Source: "runtime", Operations: globalOps, Path: "/global", Lifetime: "owner", Available: reason == "", Reason: reason, Actions: []policy.Action{policy.Read}, ShellComputeIDs: compute[id]})
		}
	}
	return json.Marshal(out)
}
func (s *sandboxRouter) inspect(ctx context.Context, p core.Principal, id core.ID, out *agent.Capabilities) error {
	info, infoErr := s.info(ctx, p, "local")
	for i := range out.Tools {
		tool := &out.Tools[i]
		switch tool.Name {
		case "shell_execute", "file_search", "file_read", "file_list", "file_write", "file_edit":
			available := false
			ids := []core.ID{}
			for _, resourceID := range tool.ResourceIDs {
				r, err := resource.NewRepository(s.adapters.store).Get(ctx, p.OwnerID, resourceID)
				if err != nil {
					continue
				}
				caps, err := s.info(ctx, p, sandboxOf(r))
				if err != nil {
					continue
				}
				if tool.Name == "shell_execute" && (caps.Capabilities.Shell || caps.Capabilities.FullAccess) || tool.Name != "shell_execute" && caps.Storage {
					available = true
					ids = append(ids, resourceID)
				}
			}
			tool.ResourceIDs = ids
			if tool.Name != "shell_execute" && infoErr == nil && info.Storage {
				available = true
			}
			if !available {
				tool.Available = false
				if infoErr != nil {
					tool.Reason = "sandbox_offline"
				} else if tool.Name == "shell_execute" && !info.Capabilities.Shell && !info.Capabilities.FullAccess {
					tool.Reason = info.Capabilities.Reason
				} else {
					tool.Reason = "permission_required"
				}
			}
		}
	}
	if !info.Storage {
		out.Skills.Items = []agent.SkillInfo{}
		out.Skills.Supported = false
	}
	return nil
}
