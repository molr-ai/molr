// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package todo

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

// Factory starts with one active assistant "assistant" owned by "owner" and no
// objects of this module. Repository tests use independent fresh fixture storage.
type contractFixture struct {
	Service    Service
	Repository Repository
	// Observe counts durable tasks, granted permissions and queued assistant events.
	Observe func() (tasks, grants, assistantEvents int)
	// SourceIntact checks original conversation/checkpoint records seeded by the adapter.
	SourceIntact func() bool
	// FailCommitOnce injects a storage failure at the next transaction commit.
	FailCommitOnce func()
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDServiceCRUD(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	req := CreateRequest{Mutation: mutation("create", 0), Title: "Buy paper", AssistantID: "assistant"}
	created, err := f.Service.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.OwnerID != "owner" || created.Version != 1 {
		t.Fatalf("bad creation: %+v", created)
	}
	replay, err := f.Service.Create(ctx, owner(), req)
	if err != nil || replay.ID != created.ID || replay.Version != created.Version {
		t.Fatalf("non-idempotent create: %+v %v", replay, err)
	}
	got, err := f.Service.Get(ctx, owner(), created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	page, err := f.Service.List(ctx, owner(), Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("list: %+v %v", page, err)
	}
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Title: "Buy recycled paper", AssistantID: "assistant"}, ID: created.ID, Completed: true})
	if err != nil || updated.Version <= created.Version {
		t.Fatalf("update: %+v %v", updated, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Title: "Buy recycled paper", AssistantID: "assistant"}, ID: created.ID, Completed: true})
	if err != nil {
		t.Fatalf("replayed update must return original result: %v", err)
	}
	stale := UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Title: "Buy recycled paper", AssistantID: "assistant"}, ID: created.ID, Completed: true}
	stale.Mutation.IdempotencyKey = "stale-update"
	_, err = f.Service.Update(ctx, owner(), stale)
	wantCode(t, err, core.Conflict)
	if err = f.Service.Delete(ctx, owner(), DeleteRequest{Mutation: mutation("delete", updated.Version), ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Get(ctx, owner(), created.ID)
	wantCode(t, err, core.NotFound)
}
func TestTDDServiceOwnershipAndValidation(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	created, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", 0), Title: "Buy paper", AssistantID: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Get(ctx, otherOwner(), created.ID)
	wantDenied(t, err)
	page, err := f.Service.List(ctx, otherOwner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("foreign rows leaked: %+v %v", page, err)
	}
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Title: "Buy recycled paper", AssistantID: "assistant"}, ID: created.ID, Completed: true})
	wantDenied(t, err)
	err = f.Service.Delete(ctx, otherOwner(), DeleteRequest{Mutation: mutation("foreign-delete", created.Version), ID: created.ID})
	wantDenied(t, err)
	_, err = f.Service.Create(ctx, core.Principal{}, CreateRequest{Mutation: mutation("create", 0), Title: "Buy paper", AssistantID: "assistant"})
	wantCode(t, err, core.Unauthenticated)
	_, err = f.Service.Create(ctx, owner(), CreateRequest{})
	wantCode(t, err, core.InvalidArgument)
	_, err = f.Service.List(ctx, owner(), Query{Page: core.PageRequest{Limit: 201}})
	wantCode(t, err, core.InvalidArgument)
}
func TestTDDRepositoryVersionAndIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	item := Todo{ID: "item", OwnerID: "owner", Title: "Buy paper", Version: 1}
	if err := f.Repository.Insert(ctx, item); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Insert(ctx, item), core.Conflict)
	got, err := f.Repository.Get(ctx, "owner", item.ID)
	if err != nil || got.ID != item.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	_, err = f.Repository.Get(ctx, "other", item.ID)
	wantDenied(t, err)
	page, err := f.Repository.List(ctx, "owner", Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	page, err = f.Repository.List(ctx, "other", Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("foreign list: %+v %v", page, err)
	}
	item.Version = 2
	if err = f.Repository.Save(ctx, item, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Save(ctx, item, 1), core.Conflict)
	wantDenied(t, f.Repository.Delete(ctx, "other", item.ID, 2))
	wantCode(t, f.Repository.Delete(ctx, "owner", item.ID, 1), core.Conflict)
	if err = f.Repository.Delete(ctx, "owner", item.ID, 2); err != nil {
		t.Fatal(err)
	}
	_, err = f.Repository.Get(ctx, "owner", item.ID)
	wantCode(t, err, core.NotFound)
}
func TestTDDCreateCommitFailureLeavesNoObject(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	f.FailCommitOnce()
	_, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", 0), Title: "Buy paper", AssistantID: "assistant"})
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

func TestTDDAssignmentDoesNotStartExecution(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	beforeTasks, beforeGrants, beforeEvents := f.Observe()
	item, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("assign", 0), Title: "Review documents", AssistantID: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, grants, events := f.Observe()
	if tasks != beforeTasks || grants != beforeGrants || events != beforeEvents {
		t.Fatalf("assignment executed work: %d %d %d", tasks, grants, events)
	}
	done := false
	page, err := f.Service.List(ctx, owner(), Query{AssistantID: "assistant", Completed: &done})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != item.ID {
		t.Fatalf("filter: %+v %v", page, err)
	}
	changed := CreateRequest{Mutation: mutation("assign", 0), Title: "Different title", AssistantID: "assistant"}
	_, err = f.Service.Create(ctx, owner(), changed)
	wantCode(t, err, core.Conflict)
}
