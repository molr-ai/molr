// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package assistant

import (
	"reflect"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
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

// Adapter supplies an active owner session, supported "model", empty assistant
// storage and transactional observation of conversations, Loops and wake events.
type contractFixture struct {
	Service        Service
	Repository     Repository
	FailCommitOnce func()
	Observe        func() (assistants, conversations, loops, wakeEvents int)
	// LifecycleState observes actual dependent rows; not service return values.
	LifecycleState func(core.ID) (archived, canSend bool, activeSchedules, activeGrants int)
	// SeedDependencies writes authoritative history, memory, grants, schedules and
	// execution/checkpoint rows through test storage setup, before invoking SUT.
	SeedDependencies func(core.ID, dependentState)
	Dependencies     func(core.ID) dependentState
	GrantDetails     func(core.ID) []policy.Grant
}

type dependentState struct {
	Messages            []string
	Memories            []string
	ActiveGrantIDs      []core.ID
	ActiveScheduleIDs   []core.ID
	RunningTaskIDs      []core.ID
	HasTransientContext bool
}

var newContractFixture func(*testing.T) contractFixture

func hire(t *testing.T, f contractFixture) Assistant {
	t.Helper()
	a, e := f.Service.Hire(testkit.Context(t), owner(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("hire", 0), Name: "Researcher", ModelID: "model"})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestTDDHireIdentityAtomicityAndIdempotency(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := hire(t, f)
	if a.ID == "" || a.ConversationID == "" || a.OwnerID != "owner" || a.State != Active || a.Version != 1 {
		t.Fatalf("invalid hire: %+v", a)
	}
	replay := hire(t, f)
	if replay.ID != a.ID || replay.ConversationID != a.ConversationID {
		t.Fatal("duplicate hire created another identity")
	}
	n, c, l, events := f.Observe()
	if n != 1 || c != 1 || l != 1 || events != 1 {
		t.Fatalf("hire rows = %d %d %d %d", n, c, l, events)
	}
	_, err := f.Service.Hire(ctx, owner(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("hire", 0), Name: "Different", ModelID: "model"})
	wantCode(t, err, core.Conflict)
	got, err := f.Service.Get(ctx, owner(), a.ID)
	if err != nil || got.ID != a.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	list, err := f.Service.List(ctx, owner(), Query{State: Active, Name: "Research"})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("rename", a.Version), ID: a.ID, Name: "Editor", ModelID: "model"})
	if err != nil || updated.Name != "Editor" || updated.Version <= a.Version {
		t.Fatalf("rename: %+v %v", updated, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("stale", a.Version), ID: a.ID, Name: "Stale"})
	wantCode(t, err, core.Conflict)
}
func TestTDDHireRollsBackDependentRows(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	f.FailCommitOnce()
	_, err := f.Service.Hire(testkit.Context(t), owner(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("hire-fails", 0), Name: "Researcher", ModelID: "model"})
	if err == nil {
		t.Fatal("commit failure succeeded")
	}
	n, c, l, e := f.Observe()
	if n+c+l+e != 0 {
		t.Fatalf("partial hire: %d %d %d %d", n, c, l, e)
	}
}
func TestTDDLifecycleAndCloneKeepSeparateIdentity(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := hire(t, f)
	clone, err := f.Service.Clone(ctx, owner(), CloneRequest{Mutation: mutation("clone", 0), SourceID: a.ID, Name: "Researcher copy", CopyMessages: true, CopyMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == a.ID || clone.ConversationID == a.ConversationID || clone.State != Active {
		t.Fatalf("clone reused execution identity: %+v", clone)
	}
	dismissed, err := f.Service.Dismiss(ctx, owner(), LifecycleRequest{Mutation: mutation("dismiss", a.Version), ID: a.ID})
	if err != nil || dismissed.State != Dismissed {
		t.Fatalf("dismiss: %+v %v", dismissed, err)
	}
	archived, canSend, schedules, grants := f.LifecycleState(a.ID)
	if !archived || canSend || schedules != 0 || grants != 0 {
		t.Fatalf("dismiss left active capabilities: %v %v %d %d", archived, canSend, schedules, grants)
	}
	rehired, err := f.Service.Rehire(ctx, owner(), RehireRequest{LifecycleRequest: LifecycleRequest{Mutation: mutation("rehire", dismissed.Version), ID: a.ID}})
	if err != nil || rehired.ID != a.ID || rehired.ConversationID != a.ConversationID || rehired.State != Active {
		t.Fatalf("rehire lost history: %+v %v", rehired, err)
	}
	_, _, _, events := f.Observe()
	if events != 1 {
		t.Fatalf("clone or rehire created another birth event: %d", events)
	}
	_, _, schedules, grants = f.LifecycleState(a.ID)
	if schedules != 0 || grants != 0 {
		t.Fatal("rehire restored unconfirmed authority")
	}
}
func TestTDDGroupsAndPlacement(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := hire(t, f)
	group, err := f.Service.CreateGroup(ctx, owner(), GroupRequest{Mutation: mutation("group", 0), Name: "Work", Position: 0})
	if err != nil || group.ID == "" || group.OwnerID != "owner" {
		t.Fatalf("group: %+v %v", group, err)
	}
	group, err = f.Service.UpdateGroup(ctx, owner(), GroupRequest{Mutation: mutation("rename-group", group.Version), ID: group.ID, Name: "Projects", Position: 1})
	if err != nil || group.Name != "Projects" || group.Position != 1 {
		t.Fatalf("group rename: %+v %v", group, err)
	}
	placed, err := f.Service.Place(ctx, owner(), PlacementRequest{Mutation: mutation("place", a.Version), ID: a.ID, GroupID: group.ID, Pinned: true})
	if err != nil || placed.GroupID != group.ID || !placed.Pinned {
		t.Fatalf("placement: %+v %v", placed, err)
	}
	groups, err := f.Service.Groups(ctx, owner(), core.PageRequest{})
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("groups: %+v %v", groups, err)
	}
	if err = f.Service.DeleteGroup(ctx, owner(), LifecycleRequest{Mutation: mutation("delete-group", group.Version), ID: group.ID}); err != nil {
		t.Fatal(err)
	}
	groups, err = f.Service.Groups(ctx, owner(), core.PageRequest{})
	if err != nil || len(groups.Items) != 0 {
		t.Fatalf("deleted group retained: %+v %v", groups, err)
	}
	got, err := f.Service.Get(ctx, owner(), a.ID)
	if err != nil || got.ID != a.ID {
		t.Fatal("deleting group deleted assistant")
	}
}
func TestTDDAssistantAndGroupOwnership(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := hire(t, f)
	_, err := f.Service.Get(ctx, otherOwner(), a.ID)
	wantDenied(t, err)
	page, err := f.Service.List(ctx, otherOwner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("owner leak: %+v %v", page, err)
	}
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("foreign-update", a.Version), ID: a.ID, Name: "Stolen"})
	wantDenied(t, err)
	_, err = f.Service.Dismiss(ctx, otherOwner(), LifecycleRequest{Mutation: mutation("foreign-dismiss", a.Version), ID: a.ID})
	wantDenied(t, err)
	_, err = f.Service.Clone(ctx, otherOwner(), CloneRequest{Mutation: mutation("foreign-clone", 0), SourceID: a.ID, Name: "Stolen"})
	wantDenied(t, err)
	_, err = f.Service.Hire(ctx, agent(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("agent-hire", 0), Name: "Unauthorized", ModelID: "model"})
	wantCode(t, err, core.Forbidden)
	_, err = f.Service.Hire(ctx, core.Principal{}, HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("anonymous", 0), Name: "Unauthorized", ModelID: "model"})
	wantCode(t, err, core.Unauthenticated)
	group, err := f.Service.CreateGroup(ctx, otherOwner(), GroupRequest{Mutation: mutation("other-group", 0), Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Place(ctx, owner(), PlacementRequest{Mutation: mutation("foreign-group", a.Version), ID: a.ID, GroupID: group.ID})
	wantDenied(t, err)
	_, err = f.Service.UpdateGroup(ctx, owner(), GroupRequest{Mutation: mutation("foreign-group-update", group.Version), ID: group.ID, Name: "Stolen"})
	wantDenied(t, err)
	wantDenied(t, f.Service.DeleteGroup(ctx, owner(), LifecycleRequest{Mutation: mutation("foreign-group-delete", group.Version), ID: group.ID}))
}
func TestTDDRepositoryAssistantAndGroups(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := Assistant{ID: "assistant-row", OwnerID: "owner", Name: "Research", State: Active, ConversationID: "conversation", Version: 1}
	if err := f.Repository.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Insert(ctx, a), core.Conflict)
	got, err := f.Repository.Get(ctx, "owner", a.ID)
	if err != nil || got.ID != a.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	_, err = f.Repository.Get(ctx, "other", a.ID)
	wantDenied(t, err)
	page, err := f.Repository.List(ctx, "owner", Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	a.Name = "Changed"
	a.Version = 2
	if err = f.Repository.Save(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Save(ctx, a, 1), core.Conflict)
	group := Group{ID: "group", OwnerID: "owner", Name: "Work", Version: 1}
	if err = f.Repository.PutGroup(ctx, group, 0); err != nil {
		t.Fatal(err)
	}
	groups, err := f.Repository.Groups(ctx, "owner", core.PageRequest{})
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("groups: %+v %v", groups, err)
	}
	group.Name = "Changed"
	group.Version = 2
	if err = f.Repository.PutGroup(ctx, group, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.PutGroup(ctx, group, 1), core.Conflict)
	wantDenied(t, f.Repository.DeleteGroup(ctx, "other", group.ID, 2))
	wantCode(t, f.Repository.DeleteGroup(ctx, "owner", group.ID, 1), core.Conflict)
	if err = f.Repository.DeleteGroup(ctx, "owner", group.ID, 2); err != nil {
		t.Fatal(err)
	}
	groups, err = f.Repository.Groups(ctx, "owner", core.PageRequest{})
	if err != nil || len(groups.Items) != 0 {
		t.Fatalf("delete: %+v %v", groups, err)
	}
}

func TestTDDConcurrentHireReplaysOneIdentity(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	type outcome struct {
		value Assistant
		err   error
	}
	results := make(chan outcome, 8)
	for range 8 {
		go func() {
			a, err := f.Service.Hire(ctx, owner(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("concurrent-hire", 0), Name: "Researcher", ModelID: "model"})
			results <- outcome{a, err}
		}()
	}
	var first Assistant
	for range 8 {
		var r outcome
		select {
		case r = <-results:
		case <-ctx.Done():
			t.Fatalf("concurrent contract calls did not complete: %v", ctx.Err())
		}
		if r.err != nil {
			t.Fatal(r.err)
		}
		if first.ID == "" {
			first = r.value
		}
		if r.value.ID != first.ID || r.value.ConversationID != first.ConversationID {
			t.Fatalf("concurrent duplicate identity: %+v / %+v", first, r.value)
		}
	}
	n, c, l, _ := f.Observe()
	if n != 1 || c != 1 || l != 1 {
		t.Fatalf("concurrent hire rows: %d %d %d", n, c, l)
	}
}

func TestTDDRehireAndPlacementRejectForeignAssistant(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	a := hire(t, f)
	_, err := f.Service.Place(ctx, otherOwner(), PlacementRequest{Mutation: mutation("foreign-place", a.Version), ID: a.ID, Pinned: true})
	wantDenied(t, err)
	dismissed, err := f.Service.Dismiss(ctx, owner(), LifecycleRequest{Mutation: mutation("dismiss-for-rehire", a.Version), ID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Rehire(ctx, otherOwner(), RehireRequest{LifecycleRequest: LifecycleRequest{Mutation: mutation("foreign-rehire", dismissed.Version), ID: a.ID}})
	wantDenied(t, err)
	got, err := f.Service.Get(ctx, owner(), a.ID)
	if err != nil || got.State != Dismissed {
		t.Fatalf("foreign rehire changed state: %+v %v", got, err)
	}
}

func TestTDDCloneCopiesOnlySelectedPersistentData(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	source := hire(t, f)
	seed := dependentState{Messages: []string{"Please research rail travel", "Here is the report"}, Memories: []string{"Prefers sleeper trains"}, ActiveGrantIDs: []core.ID{"grant-selected", "grant-omitted"}, ActiveScheduleIDs: []core.ID{"daily-plan"}, RunningTaskIDs: []core.ID{"inflight-research"}, HasTransientContext: true}
	f.SeedDependencies(source.ID, seed)
	original := f.Dependencies(source.ID)
	sourceGrants := f.GrantDetails(source.ID)
	cloned, err := f.Service.Clone(ctx, owner(), CloneRequest{Mutation: mutation("clone-data", 0), SourceID: source.ID, Name: "Copy", CopyMessages: true, CopyMemory: true, GrantIDs: []core.ID{"grant-selected"}})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.ID == source.ID || cloned.ConversationID == source.ConversationID {
		t.Fatal("clone retained source identity")
	}
	got := f.Dependencies(cloned.ID)
	if !reflect.DeepEqual(got.Messages, seed.Messages) || !reflect.DeepEqual(got.Memories, seed.Memories) {
		t.Fatalf("clone lost selected persistent state: %+v", got)
	}
	if len(got.ActiveScheduleIDs) != 0 || len(got.RunningTaskIDs) != 0 || got.HasTransientContext {
		t.Fatalf("clone inherited live execution: %+v", got)
	}
	assertCopiedGrant(t, sourceGrants, f.GrantDetails(cloned.ID), "grant-selected", cloned.ID)
	if after := f.Dependencies(source.ID); !reflect.DeepEqual(after, original) {
		t.Fatalf("clone changed source: %+v", after)
	}
	empty, err := f.Service.Clone(ctx, owner(), CloneRequest{Mutation: mutation("clone-without-data", 0), SourceID: source.ID, Name: "Fresh copy"})
	if err != nil {
		t.Fatal(err)
	}
	got = f.Dependencies(empty.ID)
	if len(got.Messages)+len(got.Memories)+len(got.ActiveGrantIDs)+len(got.ActiveScheduleIDs)+len(got.RunningTaskIDs) != 0 || got.HasTransientContext {
		t.Fatalf("clone ignored copy opt-outs: %+v", got)
	}
}
func TestTDDRehireRestoresOnlyExplicitlySelectedCapabilities(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	source := hire(t, f)
	seed := dependentState{Messages: []string{"Existing history"}, Memories: []string{"Existing memory"}, ActiveGrantIDs: []core.ID{"grant-selected", "grant-omitted"}, ActiveScheduleIDs: []core.ID{"plan-selected", "plan-omitted"}, RunningTaskIDs: []core.ID{"previous-execution"}, HasTransientContext: true}
	f.SeedDependencies(source.ID, seed)
	sourceGrants := f.GrantDetails(source.ID)
	dismissed, err := f.Service.Dismiss(ctx, owner(), LifecycleRequest{Mutation: mutation("dismiss-with-data", source.Version), ID: source.ID})
	if err != nil {
		t.Fatal(err)
	}
	inactive := f.Dependencies(source.ID)
	if len(inactive.ActiveGrantIDs)+len(inactive.ActiveScheduleIDs)+len(inactive.RunningTaskIDs) != 0 {
		t.Fatalf("dismissed assistant retains active dependencies: %+v", inactive)
	}
	rehired, err := f.Service.Rehire(ctx, owner(), RehireRequest{LifecycleRequest: LifecycleRequest{Mutation: mutation("selected-rehire", dismissed.Version), ID: source.ID}, RestoreGrantIDs: []core.ID{"grant-selected"}, RestoreScheduleIDs: []core.ID{"plan-selected"}})
	if err != nil {
		t.Fatal(err)
	}
	if rehired.ConversationID != source.ConversationID || rehired.ID != source.ID {
		t.Fatal("rehire replaced identity or conversation")
	}
	got := f.Dependencies(source.ID)
	assertCopiedGrant(t, sourceGrants, f.GrantDetails(source.ID), "grant-selected", source.ID)
	if !reflect.DeepEqual(got.ActiveScheduleIDs, []core.ID{"plan-selected"}) {
		t.Fatalf("rehire restored unselected capabilities: %+v", got)
	}
	if !reflect.DeepEqual(got.Messages, seed.Messages) || !reflect.DeepEqual(got.Memories, seed.Memories) || len(got.RunningTaskIDs) != 0 {
		t.Fatalf("rehire lost history or resumed old execution: %+v", got)
	}
}

// Copies are independent grants bound to the destination assistant. Reusing a
// source grant ID would overwrite authority belonging to the original identity.
func assertCopiedGrant(t *testing.T, sources, copies []policy.Grant, selected, assistantID core.ID) {
	t.Helper()
	var source policy.Grant
	for _, grant := range sources {
		if grant.ID == selected {
			source = grant
		}
	}
	if source.ID == "" || len(copies) != 1 {
		t.Fatalf("selected grant missing or unexpected copies: %+v", copies)
	}
	copy := copies[0]
	if copy.ID == source.ID || copy.ID == "" || copy.AssistantID != assistantID || copy.OwnerID != source.OwnerID || copy.ResourceID != source.ResourceID || copy.RevokedAt != nil || !reflect.DeepEqual(copy.Actions, source.Actions) || !reflect.DeepEqual(copy.Scope, source.Scope) || !reflect.DeepEqual(copy.ExpiresAt, source.ExpiresAt) {
		t.Fatalf("copied grant changed authority or reused identity: source=%+v copy=%+v", source, copy)
	}
}

func TestAssistantBudgetPersistsUpdatesAndClone(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	budget := core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 3000000}, Period: "weekly"}
	a, err := f.Service.Hire(ctx, owner(), HireRequest{Mutation: mutation("budget-hire", 0), Name: "Budget assistant", ModelID: "model", Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if a.Budget != budget {
		t.Fatal("hire lost budget")
	}
	budget.Period = "daily"
	budget.Limit.Micros = 1000000
	a, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("budget-update", a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := f.Service.Get(ctx, owner(), a.ID)
	if err != nil || persisted.Budget != budget {
		t.Fatalf("update lost budget: %+v %v", persisted, err)
	}
	clone, err := f.Service.Clone(ctx, owner(), CloneRequest{Mutation: mutation("budget-clone", 0), SourceID: a.ID, Name: "Clone"})
	if err != nil || clone.Budget != budget {
		t.Fatalf("clone lost budget: %+v %v", clone, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("invalid-budget", a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: core.AssistantBudget{}})
	wantCode(t, err, core.InvalidArgument)
}
