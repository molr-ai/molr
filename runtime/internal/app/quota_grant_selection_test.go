package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
)

type quotaGrantReadStore struct {
	persistence.Store
	loaded    int
	failQuery bool
}
type quotaGrantReadTx struct {
	persistence.Tx
	parent *quotaGrantReadStore
}

func (s *quotaGrantReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &quotaGrantReadTx{tx, s}) })
}
func (t *quotaGrantReadTx) Store() persistence.Store { return t.parent }
func (t *quotaGrantReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == "security.grants" && t.parent.failQuery {
		return nil, core.NewError(core.Unavailable, "query unavailable")
	}
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "security.grants" {
		t.parent.loaded += len(rows)
	}
	return rows, err
}
func quotaGrantFixture(tb testing.TB) (*sandboxRouter, core.Principal, *quotaGrantReadStore) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "quota.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key("owner", "assistant"), assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		if err := resource.NewRepository(tx.Store()).Insert(ctx, resource.Resource{ID: "compute", OwnerID: "owner", Kind: resource.Compute, Provider: "fixture-compute", State: resource.Available, Version: 1}, resource.Binding{ResourceID: "compute"}); err != nil {
			return err
		}
		for i := range 1003 {
			id := core.ID(fmt.Sprintf("history-%04d", i))
			g := policy.Grant{ID: id, OwnerID: "owner", AssistantID: "assistant", ResourceID: "compute", Actions: []policy.Action{policy.Execute}, Version: 1, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{MemoryBytes: 16 << 20}}, CreatedAt: past}
			switch {
			case i < 1000:
				if i%2 == 0 {
					g.RevokedAt = &past
				} else {
					g.ExpiresAt = &past
				}
			case i == 1000:
				g.ID = "aaa-full-access"
				g.Scope.FullAccess = true
			case i == 1001:
				g.ID = "zzz-first"
				g.ExpiresAt = &future
				g.Temporary = &policy.TemporaryConstraints{Until: future, Limits: policy.Constraints{Compute: &policy.ComputeConstraints{MemoryBytes: 64 << 20}}}
			default:
				g.ID = "zzz-second"
				g.Constraints.Compute.MemoryBytes = 128 << 20
			}
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &quotaGrantReadStore{Store: db}
	runner := sandbox.New(sandbox.Options{CgroupRoot: tb.TempDir()})
	return &sandboxRouter{adapters: adapters{store: observed}, shell: &shellService{runner: runner}}, core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, observed
}
func TestQuotaUsageLoadsOnlySelectedActiveGrant(t *testing.T) {
	router, p, store := quotaGrantFixture(t)
	usage, err := router.quotaUsage(t.Context(), p, "assistant", "compute")
	need(t, err)
	if usage.Constraints == nil || usage.Constraints.Compute == nil || usage.Constraints.Compute.MemoryBytes != 64<<20 || usage.Compute == nil {
		t.Fatal("selected grant or temporary constraints changed", usage)
	}
	t.Logf("1000 revoked/expired grants,1FullAccess,2ordinary active: loaded%drecords", store.loaded)
	if store.loaded != 1 {
		t.Fatalf("quota usage loaded %d grants to select one current grant", store.loaded)
	}
}
func BenchmarkQuotaUsageGrantHistory(b *testing.B) {
	router, p, store := quotaGrantFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		usage, err := router.quotaUsage(b.Context(), p, "assistant", "compute")
		if err != nil || usage.Constraints == nil {
			b.Fatal(usage, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(store.loaded)/float64(b.N), "grant-records/op")
}

func TestQuotaGrantQueryExpiryAndFullAccessRepresentations(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "boundary.db"))
	need(t, err)
	defer db.Close()
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	before, after, zero := now.Add(-time.Nanosecond), now.Add(time.Nanosecond).In(time.FixedZone("offset", 8*3600)), time.Time{}
	expiries := []struct {
		name  string
		at    *time.Time
		valid bool
	}{{"permanent", nil, true}, {"equal", &now, false}, {"before", &before, false}, {"after_offset", &after, true}, {"zero", &zero, false}}
	for _, expiry := range expiries {
		for _, full := range []string{"omitted", "null", "false", "true"} {
			t.Run(expiry.name+"/"+full, func(t *testing.T) {
				need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
					if _, err := tx.DeleteMatching(ctx, "security.grants", persistence.Query{}); err != nil {
						return err
					}
					raw, err := json.Marshal(policy.Grant{ID: "grant", OwnerID: "owner", AssistantID: "assistant", ResourceID: "compute", ExpiresAt: expiry.at})
					if err != nil {
						return err
					}
					var row map[string]any
					if err = json.Unmarshal(raw, &row); err != nil {
						return err
					}
					scope := row["Scope"].(map[string]any)
					switch full {
					case "null":
						scope["FullAccess"] = nil
					case "false":
						scope["FullAccess"] = false
					case "true":
						scope["FullAccess"] = true
					}
					return tx.Put(ctx, "security.grants", "owner/grant", row)
				}))
				rows, err := persistence.Select[policy.Grant](t.Context(), db, "security.grants", quotaGrantQuery("owner", "assistant", "compute", now))
				need(t, err)
				want := 0
				if expiry.valid && full != "true" {
					want = 1
				}
				if len(rows) != want {
					t.Fatalf("got%d records, want%d", len(rows), want)
				}
			})
		}
	}
}

func TestQuotaUsageObservesGrantChangesAndFailsClosed(t *testing.T) {
	router, p, store := quotaGrantFixture(t)
	update := func(id core.ID, change func(*policy.Grant)) {
		t.Helper()
		need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			var g policy.Grant
			if err := tx.Get(ctx, "security.grants", persistence.Key(p.OwnerID, id), &g); err != nil {
				return err
			}
			change(&g)
			return tx.Put(ctx, "security.grants", persistence.Key(p.OwnerID, id), g)
		}))
	}
	past := time.Now().Add(-time.Hour)
	update("zzz-first", func(g *policy.Grant) { g.Temporary.Until = past })
	usage, err := router.quotaUsage(t.Context(), p, "assistant", "compute")
	need(t, err)
	if usage.Constraints.Compute.MemoryBytes != 16<<20 {
		t.Fatal("expired temporary limits did not restore base")
	}
	update("zzz-first", func(g *policy.Grant) { g.RevokedAt = &past })
	usage, err = router.quotaUsage(t.Context(), p, "assistant", "compute")
	need(t, err)
	if usage.Constraints.Compute.MemoryBytes != 128<<20 {
		t.Fatal("revoked first grant still selected")
	}
	update("zzz-second", func(g *policy.Grant) { g.ExpiresAt = &past })
	store.loaded = 0
	usage, err = router.quotaUsage(t.Context(), p, "assistant", "compute")
	need(t, err)
	if usage.Constraints != nil || usage.Compute != nil || store.loaded != 0 {
		t.Fatal("history or FullAccess became an ordinary quota", usage, store.loaded)
	}
	store.failQuery = true
	if _, err = router.quotaUsage(t.Context(), p, "assistant", "compute"); core.CodeOf(err) != core.Unavailable {
		t.Fatal("query error swallowed", err)
	}
	store.failQuery = false
	foreign := p
	foreign.OwnerID = "foreign"
	foreign.SubjectID = "foreign"
	store.loaded = 0
	if _, err = router.quotaUsage(t.Context(), foreign, "assistant", "compute"); core.CodeOf(err) != core.NotFound || store.loaded != 0 {
		t.Fatal("foreign owner read grants", err, store.loaded)
	}
}

func TestQuotaGrantQueryKeepsOwnerAssistantResourceScope(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scope.db"))
	need(t, err)
	defer db.Close()
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i, scope := range [][3]core.ID{{"other", "assistant", "compute"}, {"owner", "other", "compute"}, {"owner", "assistant", "other"}, {"owner", "assistant", "compute"}} {
			g := policy.Grant{ID: core.ID(fmt.Sprint(i)), OwnerID: scope[0], AssistantID: scope[1], ResourceID: scope[2]}
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	}))
	rows, err := persistence.Select[policy.Grant](t.Context(), db, "security.grants", quotaGrantQuery("owner", "assistant", "compute", time.Now()))
	need(t, err)
	if len(rows) != 1 || rows[0].ID != "3" {
		t.Fatal("scope selection changed", rows)
	}
}
