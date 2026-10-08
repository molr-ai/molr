package app

import (
	"context"
	"sort"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

// Requestable descriptors advertise choices, never permission to use them.
// No provider credential, host path or management endpoint enters this view.
// Network scope is populated from the sandbox deployment by the catalog.
type requestablePermission struct {
	FullAccessAvailable     bool             `json:",omitempty"`
	Workspace               bool             `json:",omitempty"`
	AuthorizationCeilings   *platform.Limits `json:",omitempty"`
	WorkspaceQuotaSupported bool
	Network                 *networkPermissionScope `json:",omitempty"`
	ResourceID              core.ID
	Name                    string
	Kind                    resource.Kind
	SandboxID               core.ID `json:",omitempty"`
	ModelID                 core.ID `json:",omitempty"`
	ModelName               string  `json:",omitempty"`
	Actions                 []policy.Action
	PathPrefixes            []string `json:",omitempty"`
	State                   resource.State
}

func requestablePermissions(ctx context.Context, store persistence.Store, owner core.ID) ([]requestablePermission, bool, error) {
	resources, err := persistence.SelectCatalog[resource.Resource](ctx, store, "business.resources", owner, persistence.Query{})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	out := []requestablePermission{}
	for _, r := range resources {
		if !catalogOwned(ctx, store, r.OwnerID, owner) || r.State == resource.Disconnected {
			continue
		}
		actions := []policy.Action{}
		for _, action := range r.Actions {
			if action == "read" || action == "write" || action == "execute" {
				actions = append(actions, policy.Action(action))
			}
		}
		if len(actions) == 0 {
			continue
		}
		item := requestablePermission{Workspace: r.Provider == "managed-workspace", ResourceID: r.ID, Name: r.Name, Kind: r.Kind, Actions: actions, State: r.State}
		if r.Kind == resource.Storage || r.Kind == resource.Compute || r.Kind == resource.Network {
			item.SandboxID = sandboxOf(r)
		}
		if r.Kind == resource.Storage {
			item.PathPrefixes = []string{"/"}
		}
		if r.Kind == resource.Model {
			binding, err := resource.NewRepository(store).Binding(ctx, owner, r.ID)
			if err != nil {
				return nil, false, err
			}
			for _, entry := range modelEntries(r, binding) {
				if len(out) == 200 {
					return out, true, nil
				}
				if _, err := groupReference(ctx, store, owner, "model", entry.id); err != nil {
					if core.CodeOf(err) == core.Forbidden {
						continue
					}
					return nil, false, err
				}
				model := item
				model.ModelID = entry.id
				model.ModelName = entry.config.Name
				out = append(out, model)
			}
		} else {
			if _, err := groupReference(ctx, store, owner, "sandbox", sandboxOf(r)); err != nil {
				if core.CodeOf(err) == core.Forbidden {
					continue
				}
				return nil, false, err
			}
			if len(out) == 200 {
				return out, true, nil
			}
			out = append(out, item)
		}
	}
	return out, false, nil
}
