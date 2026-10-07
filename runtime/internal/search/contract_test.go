// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package search

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func owner() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
}
func otherOwner() core.Principal {
	return core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
}
func agent() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
}
func mutation(key string, v core.Version) core.Mutation {
	return core.Mutation{IdempotencyKey: key, ExpectedVersion: v}
}
func wantCode(t *testing.T, err error, code core.Code) {
	t.Helper()
	if core.CodeOf(err) != code {
		t.Fatalf("error = %v (%s), want %s", err, core.CodeOf(err), code)
	}
}
func wantDenied(t *testing.T, err error) {
	t.Helper()
	c := core.CodeOf(err)
	if c != core.Forbidden && c != core.NotFound {
		t.Fatalf("cross-owner access = %v, want forbidden or not found", err)
	}
}

// Adapter seeds authoritative documents with matching text "budget" in every
// source. VisibleIDs excludes another owner's secret and an archived message.
// SetAccess changes current policy without rewriting the search index.
type contractFixture struct {
	Service          Service
	Maintenance      Maintenance
	Indexer          Indexer
	VisibleIDs       map[Source]core.ID
	SecretID         core.ID
	ArchivedID       core.ID
	SetAccess        func(core.ID, bool)
	SetSourceVersion func(core.Object, core.Version, bool)
	// DrainIndex waits for queued indexing/rebuild jobs using a bounded test context.
	DrainIndex func(context.Context) error
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDSearchCombinesAuthorizedSourcesAndCounts(t *testing.T) {
	f := testkit.RequireFactory(t, "search", newContractFixture)
	ctx := testkit.Context(t)
	results, err := f.Service.Search(ctx, owner(), Query{Text: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	found := map[Source]bool{}
	for _, hit := range results.Hits.Items {
		if hit.Object.ID == f.SecretID || hit.Object.ID == f.ArchivedID {
			t.Fatalf("unauthorized/default archived hit: %+v", hit)
		}
		if id, ok := f.VisibleIDs[hit.Source]; !ok || id != func() core.ID {
			if hit.Memory != nil {
				return core.ID(hit.Memory.Path)
			}
			return hit.Object.ID
		}() {
			t.Fatalf("unexpected hit: %+v", hit)
		}
		found[hit.Source] = true
	}
	for source := range f.VisibleIDs {
		if !found[source] || results.Counts[source] != 1 {
			t.Fatalf("source %s omitted or leaked count: %+v", source, results)
		}
	}
	filtered, err := f.Service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Assistants}})
	if err != nil || len(filtered.Hits.Items) != 1 || filtered.Hits.Items[0].Source != Assistants {
		t.Fatalf("filter: %+v %v", filtered, err)
	}
	for source, count := range filtered.Counts {
		if source != Assistants && count != 0 {
			t.Fatalf("excluded source counted: %s=%d", source, count)
		}
	}
	archived, err := f.Service.Search(ctx, owner(), Query{Text: "budget", IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, hit := range archived.Hits.Items {
		if hit.Object.ID == f.ArchivedID {
			seen = true
		}
		if hit.Object.ID == f.SecretID {
			t.Fatal("include archived bypassed ownership")
		}
	}
	if !seen {
		t.Fatal("archived search omitted authorized archive")
	}
}
func TestTDDSearchRechecksRevokedAccessAndCursorScope(t *testing.T) {
	f := testkit.RequireFactory(t, "search", newContractFixture)
	ctx := testkit.Context(t)
	first, err := f.Service.Search(ctx, owner(), Query{Text: "budget", Page: core.PageRequest{Limit: 1}})
	if err != nil || len(first.Hits.Items) != 1 || first.Hits.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	_, err = f.Service.Search(ctx, otherOwner(), Query{Text: "budget", Page: core.PageRequest{Limit: 1, Cursor: first.Hits.NextCursor}})
	if err == nil {
		t.Fatal("cross-owner cursor accepted")
	}
	_, err = f.Service.Search(ctx, owner(), Query{Text: "different", Page: core.PageRequest{Limit: 1, Cursor: first.Hits.NextCursor}})
	if err == nil {
		t.Fatal("cross-query cursor accepted")
	}
	id := f.VisibleIDs[Resources]
	f.SetAccess(id, false)
	results, err := f.Service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Resources}})
	if err != nil || len(results.Hits.Items) != 0 || results.Counts[Resources] != 0 {
		t.Fatalf("stale index leaked revoked resource: %+v %v", results, err)
	}
	_, err = f.Service.Search(ctx, core.Principal{}, Query{Text: "budget"})
	wantCode(t, err, core.Unauthenticated)
	_, err = f.Service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{"unknown"}})
	wantCode(t, err, core.InvalidArgument)
}
func TestTDDIndexApplyHonorsTombstonesAndSourceVersion(t *testing.T) {
	f := testkit.RequireFactory(t, "search", newContractFixture)
	ctx := testkit.Context(t)
	object := core.Object{Kind: "resource", ID: f.VisibleIDs[Resources]}
	f.SetSourceVersion(object, 2, true)
	deletion := Change{OwnerID: "owner", Object: object, Version: 2, Deleted: true}
	if err := f.Indexer.Apply(ctx, deletion); err != nil {
		t.Fatal(err)
	}
	if err := f.Indexer.Apply(ctx, deletion); err != nil {
		t.Fatalf("delete replay: %v", err)
	}
	// A delayed pre-delete job may be ignored or rejected, but cannot resurrect data.
	_ = f.Indexer.Apply(ctx, Change{OwnerID: "owner", Object: object, Version: 1})
	results, err := f.Service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Resources}})
	if err != nil || len(results.Hits.Items) != 0 || results.Counts[Resources] != 0 {
		t.Fatalf("stale index resurrected source: %+v %v", results, err)
	}
	f.SetSourceVersion(object, 3, false)
	if err = f.Indexer.Apply(ctx, Change{OwnerID: "owner", Object: object, Version: 3}); err != nil {
		t.Fatal(err)
	}
	results, err = f.Service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Resources}})
	if err != nil || len(results.Hits.Items) != 1 || results.Hits.Items[0].Object.ID != object.ID {
		t.Fatalf("new source version not indexed: %+v %v", results, err)
	}
}
func TestTDDRebuildOwnerOnlyAndStatusIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "search", newContractFixture)
	ctx := testkit.Context(t)
	_, err := f.Maintenance.Rebuild(ctx, agent())
	wantCode(t, err, core.Forbidden)
	id, err := f.Maintenance.Rebuild(ctx, owner())
	if err != nil || id == "" {
		t.Fatalf("rebuild: %q %v", id, err)
	}
	status, err := f.Maintenance.Status(ctx, owner(), id)
	if err != nil || status.ID != id || status.State == "" {
		t.Fatalf("status: %+v %v", status, err)
	}
	_, err = f.Maintenance.Status(ctx, otherOwner(), id)
	wantDenied(t, err)
	// Keep a test-side deadline even when a broken index worker ignores ctx.
	drained := make(chan error, 1)
	go func() { drained <- f.DrainIndex(ctx) }()
	select {
	case err = <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatalf("index drain did not complete: %v", ctx.Err())
	}
	status, err = f.Indexer.Status(ctx, owner(), id)
	if err != nil || status.Processed < int64(len(f.VisibleIDs)) || status.FailureCode != "" {
		t.Fatalf("rebuild failed: %+v %v", status, err)
	}
	// Exercise the embedded maintenance capability through Indexer itself.
	second, err := f.Indexer.Rebuild(ctx, owner())
	if err != nil || second == "" {
		t.Fatalf("indexer rebuild: %q %v", second, err)
	}
}
