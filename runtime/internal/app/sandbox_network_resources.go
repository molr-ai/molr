package app

import (
	"context"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Deployment network choices must also appear in the requestable catalog.
// Registering a resource never grants an assistant permission to use it.
func (s *shellService) ensureNetworks(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}
	repo := resource.NewRepository(s.store)
	resources, err := catalog.Select[resource.Resource](ctx, s.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Provider", "sandbox-network")}})
	if err != nil {
		return err
	}
	registered := map[string]bool{}
	for _, r := range resources {
		binding, err := repo.Binding(ctx, catalog.Scope, r.ID)
		if err != nil {
			return err
		}
		registered[binding.SandboxTarget] = true
	}
	for _, network := range s.config.NetworkTargets(s.ownPort) {
		if registered[network.Name] {
			continue
		}
		id := core.ID("network_" + digestBytes([]byte(string(catalog.Scope) + "/local/" + network.Name))[:24])
		name := map[string]string{"preset:internet": "Internet", "preset:lan": "Local network", "preset:host": "Host services", "preset:network": "Network access"}[network.Name]
		if name == "" {
			name = network.Name
		}
		r := resource.Resource{ID: id, OwnerID: catalog.Scope, SandboxID: "local", Kind: resource.Network, Provider: "sandbox-network", Name: name, State: resource.Available, Actions: []string{"execute"}, Version: 1}
		if err := repo.Insert(ctx, r, resource.Binding{ResourceID: id, SandboxID: "local", SandboxTarget: network.Name}); err != nil {
			return err
		}
	}
	return nil
}
