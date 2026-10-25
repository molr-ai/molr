package modelcatalog_test

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/pricing"
	"path/filepath"
	"testing"
	"time"
)

func TestModelCatalogResolutionAndRevocation(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := resource.Resource{ID: "provider", OwnerID: "admin", Kind: resource.Model, Provider: "openai-compatible", State: resource.Available, Version: 1}
	b := resource.Binding{ResourceID: r.ID, Endpoint: "https://example.invalid", Models: []resource.ModelConfig{{Name: "one", ProviderContextLimit: 128000, ContextLimit: 64000, SupportsTools: true}, {Name: "two"}}}
	if err := resource.NewRepository(store).Insert(t.Context(), r, b); err != nil {
		t.Fatal(err)
	}
	entries := modelcatalog.Entries(r, b)
	group := auth.UserGroup{ID: "members", Resources: []auth.ResourceReference{{Kind: "model", ID: entries[0].ID}}}
	save := func() {
		t.Helper()
		if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "auth.groups", "members", group)
		}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	s := modelcatalog.Service{Store: store, Group: func(ctx context.Context, store persistence.Store, owner core.ID) (auth.UserGroup, error) {
		return persistence.Load[auth.UserGroup](ctx, store, "auth.groups", "members")
	}}
	// Internal estimation resolves current catalog membership without a user grant.
	estimated, err := s.ForEstimate(t.Context(), entries[1].ID)
	if err != nil || estimated.Model.ID != entries[1].ID {
		t.Fatal(estimated, err)
	}
	if _, err := s.ForEstimate(t.Context(), "missing"); core.CodeOf(err) != core.NotFound {
		t.Fatal(err)
	}
	p := core.Principal{OwnerID: "member", SubjectID: "member", Kind: core.ActorOwner, SessionID: "session"}
	got, err := s.Resolve(t.Context(), p, entries[0].ID)
	if err != nil || got.Model.ContextWindow() != 64000 || got.Model.ContextLimitSource != "manual" {
		t.Fatal(got, err)
	}
	if _, err := s.Resolve(t.Context(), p, entries[1].ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	rows, err := s.List(t.Context(), p)
	if err != nil || len(rows) != 1 || rows[0].ID != got.Model.ID {
		t.Fatal(rows, err)
	}
	group.Resources = nil
	save()
	if _, err := s.Resolve(t.Context(), p, entries[0].ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revocation ignored", err)
	}
	rows, err = s.List(t.Context(), p)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestAutomaticPriceTiersCurrencyAndManualOverride(t *testing.T) {
	catalog, err := pricing.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := modelcatalog.Service{Pricing: catalog, Now: func() time.Time { return time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) }}
	r := resource.Resource{ID: "provider"}
	b := resource.Binding{PricingProvider: "zhipu", Endpoint: "https://open.bigmodel.cn/api/paas/v4/chat/completions"}
	entry := modelcatalog.Entry{ID: "model", Config: resource.ModelConfig{Name: "glm-4.7"}}
	p := s.Describe(r, b, entry)
	if p.Pricing.Currency != "CNY" || len(p.Pricing.Tiers) != 2 || p.ReservePricing.InputMicrosPerMillion != 4000000 || p.Model.PricingInfo.Source != "builtin" {
		t.Fatal(p)
	}
	entry.Config.Pricing = &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 3}
	p = s.Describe(r, b, entry)
	if p.Pricing.Currency != "USD" || len(p.Pricing.Tiers) != 0 || p.ReservePricing.InputMicrosPerMillion != 3 || p.Model.PricingInfo.Automatic.Currency != "CNY" || p.Model.PricingInfo.Source != "manual" {
		t.Fatal(p)
	}
	entry.Config.Pricing = nil
	p = s.Describe(r, b, entry)
	if p.Pricing.Currency != "CNY" || len(p.Pricing.Tiers) != 2 {
		t.Fatal("automatic pricing not restored", p)
	}
	b.PricingProvider = "custom"
	if p = s.Describe(r, b, entry); p.Pricing != nil {
		t.Fatal("custom price inferred", p)
	}
}
