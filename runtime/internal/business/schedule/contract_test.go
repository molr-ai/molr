// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package schedule

import (
	"strings"
	"testing"
	"time"

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
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	req := CreateRequest{Mutation: mutation("create", 0), Kind: AssistantPlan, AssistantID: "assistant", Title: "Morning brief", Trigger: validTrigger()}
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
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Kind: AssistantPlan, AssistantID: "assistant", Title: "Evening brief", Trigger: validTrigger()}, ID: created.ID, Enabled: false})
	if err != nil || updated.Version <= created.Version {
		t.Fatalf("update: %+v %v", updated, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Kind: AssistantPlan, AssistantID: "assistant", Title: "Evening brief", Trigger: validTrigger()}, ID: created.ID, Enabled: false})
	if err != nil {
		t.Fatalf("replayed update must return original result: %v", err)
	}
	stale := UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Kind: AssistantPlan, AssistantID: "assistant", Title: "Evening brief", Trigger: validTrigger()}, ID: created.ID, Enabled: false}
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
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	created, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", 0), Kind: AssistantPlan, AssistantID: "assistant", Title: "Morning brief", Trigger: validTrigger()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.Get(ctx, otherOwner(), created.ID)
	wantDenied(t, err)
	page, err := f.Service.List(ctx, otherOwner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("foreign rows leaked: %+v %v", page, err)
	}
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{CreateRequest: CreateRequest{Mutation: mutation("update", created.Version), Kind: AssistantPlan, AssistantID: "assistant", Title: "Evening brief", Trigger: validTrigger()}, ID: created.ID, Enabled: false})
	wantDenied(t, err)
	err = f.Service.Delete(ctx, otherOwner(), DeleteRequest{Mutation: mutation("foreign-delete", created.Version), ID: created.ID})
	wantDenied(t, err)
	_, err = f.Service.Create(ctx, core.Principal{}, CreateRequest{Mutation: mutation("create", 0), Kind: AssistantPlan, AssistantID: "assistant", Title: "Morning brief", Trigger: validTrigger()})
	wantCode(t, err, core.Unauthenticated)
	_, err = f.Service.Create(ctx, owner(), CreateRequest{})
	wantCode(t, err, core.InvalidArgument)
	_, err = f.Service.List(ctx, owner(), Query{Page: core.PageRequest{Limit: 201}})
	wantCode(t, err, core.InvalidArgument)
}
func TestTDDRepositoryVersionAndIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	item := Schedule{ID: "item", OwnerID: "owner", Kind: AssistantPlan, AssistantID: "assistant", Title: "Morning brief", Trigger: validTrigger(), Enabled: true, Version: 1}
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
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	f.FailCommitOnce()
	_, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("create", 0), Kind: AssistantPlan, AssistantID: "assistant", Title: "Morning brief", Trigger: validTrigger()})
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

func validTrigger() Trigger {
	return Trigger{StartAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC), Timezone: "UTC", MissedPolicy: Skip}
}
func TestTDDTriggerValidationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Trigger)
		code core.Code
	}{
		{"valid", func(*Trigger) {}, ""},
		{"zero-start", func(v *Trigger) { v.StartAt = time.Time{} }, core.InvalidArgument},
		{"empty-zone", func(v *Trigger) { v.Timezone = "" }, core.InvalidArgument},
		{"unknown-zone", func(v *Trigger) { v.Timezone = "Mars/Olympus" }, core.InvalidArgument},
		{"bounded-rule", func(v *Trigger) { v.Recurrence = strings.Repeat("x", 1024) }, ""},
		{"long-rule", func(v *Trigger) { v.Recurrence = strings.Repeat("x", 1025) }, core.InvalidArgument},
		{"skip-catchup", func(v *Trigger) { v.MaxCatchUp = 1 }, core.InvalidArgument},
		{"coalesce", func(v *Trigger) { v.MissedPolicy = Coalesce }, ""},
		{"coalesce-catchup", func(v *Trigger) { v.MissedPolicy = Coalesce; v.MaxCatchUp = 1 }, core.InvalidArgument},
		{"catchup-min", func(v *Trigger) { v.MissedPolicy = CatchUp; v.MaxCatchUp = 1 }, ""},
		{"catchup-max", func(v *Trigger) { v.MissedPolicy = CatchUp; v.MaxCatchUp = 100 }, ""},
		{"catchup-zero", func(v *Trigger) { v.MissedPolicy = CatchUp }, core.InvalidArgument},
		{"catchup-over", func(v *Trigger) { v.MissedPolicy = CatchUp; v.MaxCatchUp = 101 }, core.InvalidArgument},
		{"unknown-policy", func(v *Trigger) { v.MissedPolicy = "random" }, core.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) { v := validTrigger(); tc.edit(&v); wantCode(t, v.Validate(), tc.code) })
	}
}
func TestTDDDueAndUniqueOccurrences(t *testing.T) {
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	now := validTrigger().StartAt
	for _, item := range []Schedule{
		{ID: "due", OwnerID: "owner", Kind: AssistantPlan, AssistantID: "assistant", Trigger: validTrigger(), Enabled: true, NextAt: &now, Version: 1},
		{ID: "disabled", OwnerID: "owner", Kind: AssistantPlan, AssistantID: "assistant", Trigger: validTrigger(), Enabled: false, NextAt: &now, Version: 1},
	} {
		if err := f.Repository.Insert(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.Repository.Due(ctx, now, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != "due" {
		t.Fatalf("due selection: %+v %v", rows, err)
	}
	occurrence := Occurrence{ScheduleID: "due", ScheduleVersion: 1, DueAt: now}
	inserted, err := f.Repository.RecordOccurrence(ctx, "owner", occurrence)
	if err != nil || !inserted {
		t.Fatalf("first occurrence: %v %v", inserted, err)
	}
	inserted, err = f.Repository.RecordOccurrence(ctx, "owner", occurrence)
	if err != nil || inserted {
		t.Fatalf("duplicate occurrence: %v %v", inserted, err)
	}
	_, err = f.Repository.RecordOccurrence(ctx, "other", occurrence)
	wantDenied(t, err)
	occurrence.DueAt = now.Add(time.Hour)
	inserted, err = f.Repository.RecordOccurrence(ctx, "owner", occurrence)
	if err != nil || !inserted {
		t.Fatalf("next occurrence: %v %v", inserted, err)
	}
}
func TestTDDPersonalScheduleDoesNotWakeAssistant(t *testing.T) {
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	_, _, before := f.Observe()
	_, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("personal", 0), Kind: Personal, Title: "Dentist", Trigger: validTrigger()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, after := f.Observe()
	if after != before {
		t.Fatal("personal calendar entry generated assistant event")
	}
}

func TestTDDOccurrenceConcurrentDeduplication(t *testing.T) {
	f := testkit.RequireFactory(t, "schedule", newContractFixture)
	ctx := testkit.Context(t)
	now := validTrigger().StartAt
	if err := f.Repository.Insert(ctx, Schedule{ID: "concurrent-plan", OwnerID: "owner", Kind: AssistantPlan, AssistantID: "assistant", Trigger: validTrigger(), Enabled: true, NextAt: &now, Version: 1}); err != nil {
		t.Fatal(err)
	}
	occurrence := Occurrence{ScheduleID: "concurrent-plan", ScheduleVersion: 1, DueAt: now}
	type outcome struct {
		inserted bool
		err      error
	}
	results := make(chan outcome, 8)
	for range 8 {
		go func() {
			inserted, err := f.Repository.RecordOccurrence(ctx, "owner", occurrence)
			results <- outcome{inserted, err}
		}()
	}
	inserted := 0
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
		if r.inserted {
			inserted++
		}
	}
	if inserted != 1 {
		t.Fatalf("same due occurrence inserted %d times", inserted)
	}
}
