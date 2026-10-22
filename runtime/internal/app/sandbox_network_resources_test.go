package app

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

func TestLocalNetworkPresetsBecomeRequestableWithoutGrants(t *testing.T) {
	for _, name := range []string{"permissive", "least-privilege"} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			preset, err := config.SandboxPreset(name)
			need(t, err)
			cfg.Sandbox = preset
			b, err := Build(t.Context(), cfg)
			need(t, err)
			defer b.Close()
			owner, _ := ownerSession(t, b)
			// Use the startup initializer without depending on native sandbox probes.
			shell := &shellService{store: b.store, config: cfg.Sandbox, ownPort: cfg.OwnListenPort()}
			need(t, shell.ensureCompute(t.Context()))
			need(t, shell.ensureCompute(t.Context()))
			rows, _, err := requestablePermissions(t.Context(), b.store, owner.OwnerID)
			need(t, err)
			expected := 0
			if name == "permissive" {
				expected = 3
				if platform.NativeSandbox() {
					expected = 1
				}
			}
			count := 0
			for _, r := range rows {
				if r.Kind != resource.Network {
					continue
				}
				count++
				if r.SandboxID != "local" || r.State != resource.Available {
					t.Fatal("invalid network descriptor", r)
				}
			}
			if count != expected {
				t.Fatalf("networks=%d want %d", count, expected)
			}
			grants, err := persistence.Scan[policy.Grant](t.Context(), b.store, "security.grants")
			need(t, err)
			if len(grants) != 0 {
				t.Fatal("registration granted access")
			}
			if count == 0 {
				return
			}
			resources, err := catalog.Select[resource.Resource](t.Context(), b.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Network)}})
			need(t, err)
			removed := resources[0]
			removed.State = resource.Disconnected
			removed.Version++
			need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				return resource.NewRepository(b.store).Save(ctx, removed, removed.Version-1)
			}))
			need(t, shell.ensureCompute(t.Context()))
			after, err := catalog.Select[resource.Resource](t.Context(), b.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Network)}})
			need(t, err)
			if len(after) != expected {
				t.Fatal("startup duplicated networks")
			}
			stored, err := resource.NewRepository(b.store).Get(t.Context(), owner.OwnerID, removed.ID)
			need(t, err)
			if stored.State != resource.Disconnected {
				t.Fatal("startup reconnected removed resource")
			}
		})
	}
}
