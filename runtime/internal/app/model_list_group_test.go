package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type modelGroupReads struct {
	persistence.Store
	groups, accounts, transactions, depth int
}
type modelGroupReadTx struct {
	persistence.Tx
	parent *modelGroupReads
}

func (s *modelGroupReads) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	if s.depth == 0 {
		s.transactions++
	}
	s.depth++
	defer func() { s.depth-- }()
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &modelGroupReadTx{tx, s}) })
}
func (t *modelGroupReadTx) Store() persistence.Store { return t.parent }
func (t *modelGroupReadTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == "auth.groups" {
		t.parent.groups++
	}
	if bucket == "auth.accounts" {
		t.parent.accounts++
	}
	return t.Tx.Get(ctx, bucket, key, out)
}
func modelGroupFixture(tb testing.TB) (adapters, core.Principal, *modelGroupReads) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "models.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "auth.bootstrap", "owner", struct{ AccountID core.ID }{"catalog"}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "auth.accounts", "member", struct {
			Account auth.Account
			Version core.Version
		}{auth.Account{ID: "member", GroupID: "members"}, 1}); err != nil {
			return err
		}
		group := auth.UserGroup{ID: "members", Name: "Model readers", Version: 1}
		for provider := range 2 {
			id := core.ID(fmt.Sprintf("provider-%d", provider))
			r := resource.Resource{ID: id, OwnerID: "catalog", Name: string(id), Kind: resource.Model, Provider: "openai-compatible", State: resource.Available, Version: 1}
			binding := resource.Binding{ResourceID: id, Endpoint: "https://models.example.invalid/v1"}
			for i := range 50 {
				name := fmt.Sprintf("model-%03d", provider*50+i)
				binding.Models = append(binding.Models, resource.ModelConfig{Name: name, SupportsTools: true})
				if i%2 == 0 {
					group.Resources = append(group.Resources, auth.ResourceReference{Kind: "model", ID: resource.ModelID(id, name)})
				}
			}
			if err := resource.NewRepository(tx.Store()).Insert(ctx, r, binding); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "auth.groups", "members", group)
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &modelGroupReads{Store: db}
	return adapters{store: observed}, core.Principal{OwnerID: "member", SubjectID: "member", Kind: core.ActorOwner, SessionID: "session"}, observed
}
func TestModelListReusesGroupWithinOneSnapshot(t *testing.T) {
	a, p, observed := modelGroupFixture(t)
	models, err := a.models(t.Context(), p)
	need(t, err)
	if len(models) != 50 {
		t.Fatal("group model filtering changed", len(models))
	}
	for i, m := range models {
		if m.Name != fmt.Sprintf("model-%03d", i*2) {
			t.Fatal("unexpected model or ordering", m.Name)
		}
	}
	t.Logf("100 candidates: account reads=%d, group reads=%d, read transactions=%d", observed.accounts, observed.groups, observed.transactions)
	if observed.groups != 1 || observed.accounts != 1 || observed.transactions != 1 {
		t.Fatal("model list repeatedly loaded the same group outside one snapshot")
	}
}
func BenchmarkModelListSharedGroup(b *testing.B) {
	a, p, observed := modelGroupFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		models, err := a.models(b.Context(), p)
		if err != nil || len(models) != 50 {
			b.Fatal(len(models), err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(observed.groups)/float64(b.N), "group-reads/op")
}

func TestModelListReloadsGroupAndAccountForEachRequest(t *testing.T) {
	a, p, s := modelGroupFixture(t)
	first, err := a.models(t.Context(), p)
	need(t, err)
	chosen := resource.ModelID("provider-1", "model-051")
	need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "auth.groups", "members", auth.UserGroup{ID: "members", Resources: []auth.ResourceReference{{Kind: "model", ID: chosen}, {Kind: "tool", ID: first[0].ID}}, Version: 2})
	}))
	next, err := a.models(t.Context(), p)
	need(t, err)
	if len(next) != 1 || next[0].ID != chosen {
		t.Fatal("stale or wrong-kind group permission", next)
	}
	need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "auth.groups", "other", auth.UserGroup{ID: "other", Resources: []auth.ResourceReference{}}); err != nil {
			return err
		}
		return tx.Put(ctx, "auth.accounts", "member", struct {
			Account auth.Account
			Version core.Version
		}{auth.Account{ID: "member", GroupID: "other"}, 2})
	}))
	next, err = a.models(t.Context(), p)
	need(t, err)
	if len(next) != 0 {
		t.Fatal("account group reassignment was not observed", next)
	}
	need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "auth.accounts", "member", struct {
			Account auth.Account
			Version core.Version
		}{auth.Account{ID: "member", GroupID: "members", Disabled: true}, 3})
	}))
	next, err = a.models(t.Context(), p)
	if core.CodeOf(err) != core.Unauthenticated || next != nil {
		t.Fatal("disabled account retained catalog access", next, err)
	}
}

func TestModelListAdministratorStandaloneAndMissingPolicy(t *testing.T) {
	for _, mode := range []string{"administrator", "standalone", "missing_account", "missing_group"} {
		t.Run(mode, func(t *testing.T) {
			a, p, s := modelGroupFixture(t)
			need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				switch mode {
				case "administrator":
					return tx.Put(ctx, "auth.groups", "members", auth.UserGroup{ID: "members", Administrator: true})
				case "standalone":
					return tx.Delete(ctx, "auth.bootstrap", "owner")
				case "missing_account":
					return tx.Delete(ctx, "auth.accounts", "member")
				default:
					return tx.Delete(ctx, "auth.groups", "members")
				}
			}))
			if mode == "standalone" {
				p.OwnerID = "catalog"
				p.SubjectID = "catalog"
			}
			rows, err := a.models(t.Context(), p)
			if mode == "missing_account" || mode == "missing_group" {
				if core.CodeOf(err) != core.NotFound || rows != nil {
					t.Fatal("missing policy did not fail closed", rows, err)
				}
			} else {
				need(t, err)
				if len(rows) != 100 {
					t.Fatal("administrator/standalone models changed", len(rows))
				}
			}
		})
	}
}
