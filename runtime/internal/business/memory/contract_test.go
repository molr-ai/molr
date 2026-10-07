// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package memory

import (
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
func mutation(key string, v string) Mutation {
	return Mutation{IdempotencyKey: key, ExpectedVersion: v}
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

// Factory starts with one active assistant "assistant" owned by "owner" and no
// objects of this module. Repository tests use independent fresh fixture storage.
type contractFixture struct {
	Service    Service
	Repository *Files
	// Observe counts durable tasks, granted permissions and queued assistant events.
	Observe func() (tasks, grants, assistantEvents int)
	// SourceIntact checks original conversation/checkpoint records seeded by the adapter.
	SourceIntact func() bool
	// FailCommitOnce injects a failure immediately before the next file rename.
	FailCommitOnce func()
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDServiceCRUD(t *testing.T) {
	f := testkit.RequireFactory(t, "memory", newContractFixture)
	ctx := testkit.Context(t)
	req := CreateRequest{Mutation: mutation("create", ""), Text: "Prefers Chinese", Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}}
	created, err := f.Service.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	if created.Path == "" || created.OwnerID != "owner" || len(created.Version) != 64 {
		t.Fatalf("bad creation: %+v", created)
	}
	replay, err := f.Service.Create(ctx, owner(), req)
	if err != nil || replay.Ref != created.Ref || replay.Version != created.Version {
		t.Fatalf("non-idempotent create: %+v %v", replay, err)
	}
	got, err := f.Service.Get(ctx, owner(), created.Ref)
	if err != nil || got.Ref != created.Ref {
		t.Fatalf("get: %+v %v", got, err)
	}
	page, err := f.Service.List(ctx, owner(), Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref != created.Ref {
		t.Fatalf("list: %+v %v", page, err)
	}
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("update", created.Version), Ref: created.Ref, Text: "Prefers concise Chinese"})
	if err != nil || updated.Version == created.Version {
		t.Fatalf("update: %+v %v", updated, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("update", created.Version), Ref: created.Ref, Text: "Prefers concise Chinese"})
	if err != nil {
		t.Fatalf("replayed update must return original result: %v", err)
	}
	stale := UpdateRequest{Mutation: mutation("update", created.Version), Ref: created.Ref, Text: "Prefers concise Chinese"}
	stale.Mutation.IdempotencyKey = "stale-update"
	_, err = f.Service.Update(ctx, owner(), stale)
	wantCode(t, err, core.Conflict)
	if err = f.Service.Delete(ctx, owner(), DeleteRequest{Forget: true, Mutation: mutation("delete", updated.Version), Ref: created.Ref}); err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Get(ctx, owner(), created.Ref)
	wantCode(t, err, core.NotFound)
}
func TestTDDServiceOwnershipAndValidation(t *testing.T) {
	f := testkit.RequireFactory(t, "memory", newContractFixture)
	ctx := testkit.Context(t)
	created, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", ""), Text: "Prefers Chinese", Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Get(ctx, otherOwner(), created.Ref)
	wantDenied(t, err)
	page, err := f.Service.List(ctx, otherOwner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("foreign rows leaked: %+v %v", page, err)
	}
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{Mutation: mutation("update", created.Version), Ref: created.Ref, Text: "Prefers concise Chinese"})
	wantDenied(t, err)
	err = f.Service.Delete(ctx, otherOwner(), DeleteRequest{Forget: true, Mutation: mutation("foreign-delete", created.Version), Ref: created.Ref})
	wantDenied(t, err)
	_, err = f.Service.Create(ctx, core.Principal{}, CreateRequest{Mutation: mutation("create", ""), Text: "Prefers Chinese", Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}})
	wantCode(t, err, core.Unauthenticated)
	_, err = f.Service.Create(ctx, owner(), CreateRequest{})
	wantCode(t, err, core.InvalidArgument)
	_, err = f.Service.List(ctx, owner(), Query{Page: core.PageRequest{Limit: 201}})
	wantCode(t, err, core.InvalidArgument)
}
func TestFileRepositoryVersionAndIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "memory", newContractFixture)
	ctx := t.Context()
	item, err := f.Repository.Put(ctx, Memory{OwnerID: "owner", Text: "Prefers Chinese", Ref: Ref{Path: ".agents/memory/item.md", Scope: Assistant, AssistantID: "assistant"}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Repository.Put(ctx, item, "", nil)
	wantCode(t, err, core.Conflict)
	_, err = f.Repository.Get(ctx, "other", item.Ref)
	wantDenied(t, err)
	first := item.Version
	item.Text = "Updated preference"
	item, err = f.Repository.Put(ctx, item, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Repository.Put(ctx, item, first, nil)
	wantCode(t, err, core.Conflict)
	wantCode(t, f.Repository.Delete(ctx, item, first, false, nil), core.Conflict)
	if err = f.Repository.Delete(ctx, item, item.Version, false, nil); err != nil {
		t.Fatal(err)
	}
	_, err = f.Repository.Get(ctx, "owner", item.Ref)
	wantCode(t, err, core.NotFound)
}
func TestTDDCreateCommitFailureLeavesNoObject(t *testing.T) {
	f := testkit.RequireFactory(t, "memory", newContractFixture)
	ctx := testkit.Context(t)
	f.FailCommitOnce()
	_, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", ""), Text: "Prefers Chinese", Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}})
	if err == nil {
		t.Fatal("commit failure reported success")
	}
	page, err := f.Service.List(ctx, owner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("partial object: %+v %v", page, err)
	}
	_, _, events := f.Observe()
	if events != 0 {
		t.Fatalf("orphan events: %d", events)
	}
}

func TestTDDPrivateAndSharedMemoryPolicy(t *testing.T) {
	f := testkit.RequireFactory(t, "memory", newContractFixture)
	ctx := testkit.Context(t)
	private, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("private", ""), Text: "Assistant fact", Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/private.md"}})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("shared", ""), Text: "Global fact", Ref: Ref{Scope: Global, Path: ".agents/memory/shared.md"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Update(ctx, agent(), UpdateRequest{Mutation: mutation("agent-shared", shared.Version), Ref: shared.Ref, Text: "Overwrite"})
	wantCode(t, err, core.Forbidden)
	foreign := agent()
	foreign.SubjectID = "another-assistant"
	_, err = f.Service.Get(ctx, foreign, private.Ref)
	wantDenied(t, err)
	page, err := f.Service.List(ctx, owner(), Query{Scope: Global})
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref != shared.Ref {
		t.Fatalf("scope filtering: %+v %v", page, err)
	}
	if err = f.Service.Delete(ctx, owner(), DeleteRequest{Forget: true, Mutation: mutation("delete-private", private.Version), Ref: private.Ref}); err != nil {
		t.Fatal(err)
	}
	if !f.SourceIntact() {
		t.Fatal("deleting memory erased source conversation or checkpoint")
	}
}

func TestRecallTermsAndRanking(t *testing.T) {
	if Score("项目周报", "这个项目的周报先写结论") == 0 || Score("database migration", "Database migration plan") != 2 || len(Terms("a !")) != 0 {
		t.Fatal("multilingual retrieval terms")
	}
}
