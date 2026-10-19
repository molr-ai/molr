package plugins

import (
	"context"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/catalog"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type capacityReadStore struct {
	persistence.Store
	rows int
}
type capacityReadTx struct {
	persistence.Tx
	counts *capacityReadStore
}

func (s *capacityReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &capacityReadTx{tx, s}) })
}
func (tx *capacityReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == "plugins.installations" || bucket == "plugins.connections" {
		tx.counts.rows += len(rows)
	}
	return rows, err
}

func TestRemovedPluginsReleaseInstallationCapacity(t *testing.T) {
	s, p, pkg := fixture(t)
	for i := 0; i < 32; i++ {
		pkg.Manifest.ID = fmt.Sprintf("fixture.removed%02d", i)
		v, err := s.Install(t.Context(), p, pkg)
		must(t, err)
		must(t, s.Remove(t.Context(), p, v.ID, v.Version))
	}
	counts := &capacityReadStore{Store: s.store}
	s.store = counts
	visible, err := s.List(t.Context(), p)
	must(t, err)
	if counts.rows != 0 {
		t.Fatalf("loaded %d deleted installations", counts.rows)
	}
	if len(visible) != 0 {
		t.Fatalf("removed installations remain visible: %d", len(visible))
	}
	pkg.Manifest.ID = "fixture.replacement"
	if _, err := s.Install(t.Context(), p, pkg); err != nil {
		t.Fatalf("cannot install with zero active plugins after 32 removals: %v", err)
	}
}

func TestRemovedConnectionsReleaseProviderCapacity(t *testing.T) {
	s, p, v, _, _ := setupHTTP(t)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("temporary-%02d", i)
		c, err := s.SaveConnection(t.Context(), p, SaveConnection{
			PluginID: v.ID, Digest: v.Digest, Name: key, ProviderID: "a", Enabled: true,
			APIKey: core.NewSecret("synthetic-connection-key"), Mutation: core.Mutation{IdempotencyKey: key},
			CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10,
		})
		must(t, err)
		must(t, s.RemoveConnection(t.Context(), p, ConnectionRequest{PluginID: v.ID, ID: c.ID, Mutation: core.Mutation{IdempotencyKey: "remove-" + key, ExpectedVersion: c.Version}}))
	}
	counts := &capacityReadStore{Store: s.store}
	s.store = counts
	visible, err := s.Connections(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	if counts.rows != 2 {
		t.Fatalf("loaded %d connection rows for two live connections", counts.rows)
	}
	if len(visible) != 2 {
		t.Fatalf("wrong active connection count: %d", len(visible))
	}
	_, err = s.SaveConnection(t.Context(), p, SaveConnection{
		PluginID: v.ID, Digest: v.Digest, Name: "Replacement", ProviderID: "a", Enabled: true,
		APIKey: core.NewSecret("synthetic-replacement-key"), Mutation: core.Mutation{IdempotencyKey: "replacement"},
		CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("cannot create with only two active connections after 30 removals: %v", err)
	}
}

func TestInstallationCapacityIncludesDisabledAndChecksRestore(t *testing.T) {
	s, p, pkg := fixture(t)
	removed, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	must(t, s.Remove(t.Context(), p, removed.ID, removed.Version))
	restore := pkg
	var first Installation
	for i := 0; i < 32; i++ {
		pkg.Manifest.ID = fmt.Sprintf("fixture.live%02d", i)
		v, err := s.Install(t.Context(), p, pkg)
		must(t, err)
		if v.Enabled {
			t.Fatal("fixture unexpectedly enabled")
		}
		if i == 0 {
			first = v
		}
	}
	if _, err := s.Install(t.Context(), p, restore); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("restored plugin exceeded capacity", err)
	}
	pkg.Manifest.ID = "fixture.extra"
	if _, err := s.Install(t.Context(), p, pkg); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("disabled installations not counted", err)
	}
	pkg.Manifest.ID = first.ID
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	if v.Version != first.Version {
		t.Fatal("same release retry changed version")
	}
	pkg.Manifest.Version = "2.0.0"
	updated, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	must(t, s.Remove(t.Context(), p, updated.ID, updated.Version))
	s.store.(*sqlitestore.Store).FailNextCommit(errors.New("synthetic commit failure"))
	if _, err := s.Install(t.Context(), p, restore); err == nil {
		t.Fatal("restore failure hidden")
	}
	tombstone, err := s.load(t.Context(), p.OwnerID, restore.Manifest.ID)
	must(t, err)
	if !tombstone.Deleted {
		t.Fatal("failed restore consumed a slot")
	}
	restored, err := s.Install(t.Context(), p, restore)
	must(t, err)
	if restored.Deleted || restored.Enabled || len(restored.Grants) != 0 || len(restored.Bindings) != 0 {
		t.Fatal("restore retained active capabilities")
	}
	visible, err := s.List(t.Context(), p)
	must(t, err)
	if len(visible) != 32 {
		t.Fatalf("installation count %d", len(visible))
	}
}

func TestInstallationCapacityConcurrentLastSlotAndPrivateNamespaceIsolation(t *testing.T) {
	s, p, pkg := fixture(t)
	must(t, s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, owner := range []core.ID{catalog.Scope, "other"} {
			n := 32
			if owner == catalog.Scope {
				n = 31
			}
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("fixture.existing%02d", i)
				if err := tx.Put(ctx, "plugins.installations", persistence.Key(owner, Key(id)), Installation{ID: id, OwnerID: owner}); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			<-start
			candidate := pkg
			candidate.Manifest.ID = fmt.Sprintf("fixture.contender%d", i)
			_, err := s.Install(t.Context(), p, candidate)
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	successes, denials := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if core.CodeOf(err) == core.InvalidArgument {
			denials++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || denials != 1 {
		t.Fatalf("success=%d denied=%d", successes, denials)
	}
}

func TestConnectionCapacityIncludesDisabledAndPreservesOtherScopes(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	must(t, s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, scope := range []struct {
			owner  core.ID
			plugin string
		}{{"other", v.ID}, {p.OwnerID, "fixture.other"}} {
			for i := 0; i < 32; i++ {
				id := core.ID(fmt.Sprintf("%s-%02d", scope.plugin, i))
				if err := tx.Put(ctx, "plugins.connections", persistence.Key(scope.owner, id), Connection{ID: id, OwnerID: scope.owner, PluginID: scope.plugin}); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	var last ConnectionView
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("disabled-%02d", i)
		var err error
		last, err = s.SaveConnection(t.Context(), p, SaveConnection{PluginID: v.ID, Digest: v.Digest, Name: key, ProviderID: "a", Enabled: false, APIKey: core.NewSecret("synthetic-key"), Mutation: core.Mutation{IdempotencyKey: key}, CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10})
		must(t, err)
	}
	request := SaveConnection{PluginID: v.ID, Digest: v.Digest, Name: "Replacement", ProviderID: "a", Enabled: true, APIKey: core.NewSecret("synthetic-key"), Mutation: core.Mutation{IdempotencyKey: "capacity-replacement"}, CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10}
	if _, err := s.SaveConnection(t.Context(), p, request); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("disabled connections not counted", err)
	}
	update := request
	update.ID = a.ID
	update.APIKey = core.Secret{}
	update.Name = "Renamed at capacity"
	update.Mutation = core.Mutation{IdempotencyKey: "update-at-cap", ExpectedVersion: a.Version}
	if _, err := s.SaveConnection(t.Context(), p, update); err != nil {
		t.Fatal("existing connection update rejected", err)
	}
	must(t, s.RemoveConnection(t.Context(), p, ConnectionRequest{PluginID: v.ID, ID: last.ID, Mutation: core.Mutation{IdempotencyKey: "remove-last", ExpectedVersion: last.Version}}))
	s.store.(*sqlitestore.Store).FailNextCommit(errors.New("synthetic commit failure"))
	if _, err := s.SaveConnection(t.Context(), p, request); err == nil {
		t.Fatal("save failure hidden")
	}
	created, err := s.SaveConnection(t.Context(), p, request)
	must(t, err)
	retry, err := s.SaveConnection(t.Context(), p, request)
	must(t, err)
	if retry.ID != created.ID {
		t.Fatal("retry consumed a second slot")
	}
	rows, err := s.Connections(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	if len(rows) != 32 {
		t.Fatalf("connection count %d", len(rows))
	}
	deleted, err := s.connection(t.Context(), p.OwnerID, last.ID)
	must(t, err)
	if !deleted.Deleted || deleted.CredentialID != "" {
		t.Fatal("deletion state or credential reference lost")
	}
}
