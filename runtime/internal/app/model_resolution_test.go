package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

type modelReads struct {
	bindings, resources int
	cancelAfterResource context.CancelFunc
}
type modelReadStore struct {
	persistence.Store
	reads *modelReads
}
type modelReadTx struct {
	persistence.Tx
	reads *modelReads
}

func (s modelReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, modelReadTx{tx, s.reads}) })
}
func (tx modelReadTx) Store() persistence.Store { return modelReadStore{tx.Tx.Store(), tx.reads} }
func (tx modelReadTx) Get(ctx context.Context, bucket, key string, out any) error {
	err := tx.Tx.Get(ctx, bucket, key, out)
	if err == nil {
		if bucket == "business.bindings" {
			tx.reads.bindings++
		}
		if bucket == "business.resources" {
			tx.reads.resources++
			if tx.reads.cancelAfterResource != nil {
				tx.reads.cancelAfterResource()
			}
		}
	}
	return err
}
func (tx modelReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == "business.bindings" {
		tx.reads.bindings += len(rows)
	}
	if bucket == "business.resources" {
		tx.reads.resources += len(rows)
	}
	return rows, err
}

func TestModelResolutionReadsOnlySelectedProvider(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "models.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := resource.NewRepository(db)
	var target core.ID
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range 200 {
			id := core.ID(fmt.Sprintf("provider-%04d", i))
			r := resource.Resource{ID: id, OwnerID: "owner", Kind: resource.Model, Provider: "openai-compatible", State: resource.Available, Version: 1}
			b := resource.Binding{ResourceID: id, Endpoint: "https://example.invalid", Models: []resource.ModelConfig{{Name: "model", ContextLimit: 32768, Pricing: &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 2000000}}}}
			if err := repo.Insert(ctx, r, b); err != nil {
				return err
			}
			target = resource.ModelID(id, "model")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reads := &modelReads{}
	a := adapters{reserveModelCost: true, store: modelReadStore{db, reads}}
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	got, err := a.provider(t.Context(), p, target)
	if err != nil || got.Model.ID != target || reads.bindings != 1 || reads.resources != 1 {
		t.Fatal(got, err, *reads)
	}
	*reads = modelReads{}
	estimate, err := a.estimate(t.Context(), target, nil, 10)
	if err != nil || estimate != (core.Money{Currency: "USD", Micros: 32788}) || reads.bindings != 1 || reads.resources != 1 {
		t.Fatal(estimate, err, *reads)
	}
	*reads = modelReads{}
	if _, err := a.estimate(t.Context(), "missing-model", nil, 10); core.CodeOf(err) != core.FailedPrecondition || core.ReasonOf(err) != core.ModelUnavailable || reads.bindings != 0 || reads.resources != 0 {
		t.Fatal(err, *reads)
	}
	*reads = modelReads{}
	if _, err := a.provider(t.Context(), p, "missing-model"); core.CodeOf(err) != core.NotFound || reads.bindings != 0 || reads.resources != 0 {
		t.Fatal(err, *reads)
	}
	ctx, cancel := context.WithCancel(t.Context())
	reads.cancelAfterResource = cancel
	if _, err := a.provider(ctx, p, target); core.CodeOf(err) != core.Canceled {
		t.Fatal("cancellation hidden by owner check", err)
	}
	reads.cancelAfterResource = nil
	// An unrelated unreadable binding cannot affect this model's resolution.
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "business.bindings", persistence.Key(catalog.Scope, "provider-0000"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.provider(t.Context(), p, target); err != nil {
		t.Fatal(err)
	}
	*reads = modelReads{}
	if _, err = a.estimate(t.Context(), target, nil, 10); err != nil || reads.bindings != 1 || reads.resources != 1 {
		t.Fatal(err, *reads)
	}
	// Binding changes take effect on the next estimate without a cache.
	binding, err := repo.Binding(t.Context(), catalog.Scope, "provider-0199")
	need(t, err)
	binding.Models[0].Pricing.InputMicrosPerMillion = 2000000
	need(t, repo.SaveBinding(t.Context(), binding))
	estimate, err = a.estimate(t.Context(), target, nil, 10)
	if err != nil || estimate.Micros != 65556 {
		t.Fatal(estimate, err)
	}
	binding.Models = nil
	need(t, repo.SaveBinding(t.Context(), binding))
	if _, err = a.estimate(t.Context(), target, nil, 10); core.ReasonOf(err) != core.ModelUnavailable {
		t.Fatal(err)
	}
}

func TestModelResolutionKeepsLiveGroupAndProviderChecks(t *testing.T) {
	f := newMultiUserFixture(t)
	a := adapters{reserveModelCost: true, store: f.b.store, pricing: f.b.Pricing}
	provider, err := a.provider(t.Context(), f.alice, f.model)
	need(t, err)
	binding, err := resource.NewRepository(f.b.store).Binding(t.Context(), f.admin.OwnerID, provider.Model.ResourceID)
	need(t, err)
	sibling := resource.ModelID(binding.ResourceID, binding.Models[1].Name)
	if _, err = a.provider(t.Context(), f.alice, sibling); core.CodeOf(err) != core.Forbidden {
		t.Fatal("sibling model escaped group", err)
	}
	group := f.group
	group.Resources = nil
	_, err = f.b.Services.Auth.(*auth.Manager).SaveGroup(t.Context(), f.admin, auth.SaveGroupRequest{Group: group, Mutation: core.Mutation{IdempotencyKey: "remove-model", ExpectedVersion: group.Version}})
	need(t, err)
	if _, err = a.provider(t.Context(), f.alice, f.model); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked model remained usable", err)
	}
	// Estimation is not a user authorization and must not restore revoked access.
	if _, err = a.estimate(t.Context(), f.model, nil, 10); err != nil {
		t.Fatal(err)
	}
	if _, err = a.provider(t.Context(), f.alice, f.model); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	_, err = a.provider(t.Context(), f.admin, f.model)
	need(t, err)
	repo := resource.NewRepository(f.b.store)
	r, err := repo.Get(t.Context(), f.admin.OwnerID, provider.Model.ResourceID)
	need(t, err)
	version := r.Version
	r.Version++
	r.State = resource.Offline
	need(t, repo.Save(t.Context(), r, version))
	if _, err = a.provider(t.Context(), f.admin, f.model); core.CodeOf(err) != core.NotFound {
		t.Fatal("offline model resolved", err)
	}
	if _, err = a.estimate(t.Context(), f.model, nil, 10); core.ReasonOf(err) != core.ModelUnavailable {
		t.Fatal("offline model estimated", err)
	}
}
