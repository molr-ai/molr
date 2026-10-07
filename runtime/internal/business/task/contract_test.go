// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package task

import (
	"reflect"
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

// Adapter seeds active assistant "assistant", a live execution for that assistant,
// an owned artifact/evidence pair, and no tasks. ExecutionFor binds persisted run
// state to an existing task; it never mutates task progress or delivery results.
type contractFixture struct {
	Service        Service
	Execution      Execution
	Repository     Repository
	ExecutionFor   func(core.ID) core.Execution
	RecordSpent    func(core.ID, core.Money)
	ArtifactID     core.ID
	EvidenceID     core.ID
	FailCommitOnce func()
	Observe        func() (tasks, revisions, wakeEvents int)
}

var newContractFixture func(*testing.T) contractFixture

func createTask(t *testing.T, f contractFixture) Task {
	t.Helper()
	v, e := f.Service.Create(testkit.Context(t), owner(), CreateRequest{Mutation: mutation("create", 0), AssistantID: "assistant", Goal: "Produce report", AcceptanceCriteria: []string{"report exists"}})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestTDDTaskCreateAndRead(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	task := createTask(t, f)
	if task.ID == "" || task.OwnerID != "owner" || task.AssistantID != "assistant" || task.Version != 1 || task.Revision != 1 || task.State != Pending {
		t.Fatalf("new task: %+v", task)
	}
	replay := createTask(t, f)
	if replay.ID != task.ID {
		t.Fatal("replay duplicated task")
	}
	got, err := f.Service.Get(ctx, owner(), task.ID)
	if err != nil || got.Goal != "Produce report" {
		t.Fatalf("get: %+v %v", got, err)
	}
	page, err := f.Service.List(ctx, owner(), Query{AssistantID: "assistant", States: []State{Pending}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != task.ID {
		t.Fatalf("list: %+v %v", page, err)
	}
	_, err = f.Service.Get(ctx, otherOwner(), task.ID)
	wantDenied(t, err)
	page, err = f.Service.List(ctx, otherOwner(), Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("foreign list: %+v %v", page, err)
	}
	_, err = f.Service.Create(ctx, core.Principal{}, CreateRequest{Mutation: mutation("anonymous", 0), AssistantID: "assistant", Goal: "Report"})
	wantCode(t, err, core.Unauthenticated)
	_, err = f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation("empty-goal", 0), AssistantID: "assistant", Goal: ""})
	wantCode(t, err, core.InvalidArgument)
}
func TestTDDTaskControlsAndOptimisticVersion(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	task := createTask(t, f)
	_, err := f.Service.Pause(ctx, otherOwner(), ControlRequest{Mutation: mutation("foreign-pause", task.Version), ID: task.ID})
	wantDenied(t, err)
	paused, err := f.Service.Pause(ctx, owner(), ControlRequest{Mutation: mutation("pause", task.Version), ID: task.ID})
	if err != nil || paused.State != Paused {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	_, err = f.Service.Resume(ctx, owner(), ControlRequest{Mutation: mutation("stale-resume", task.Version), ID: task.ID})
	wantCode(t, err, core.Conflict)
	resumed, err := f.Service.Resume(ctx, owner(), ControlRequest{Mutation: mutation("resume", paused.Version), ID: task.ID})
	if err != nil || (resumed.State != Pending && resumed.State != Running) {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	canceled, err := f.Service.Cancel(ctx, owner(), ControlRequest{Mutation: mutation("cancel", resumed.Version), ID: task.ID})
	if err != nil || canceled.State != Canceled {
		t.Fatalf("cancel: %+v %v", canceled, err)
	}
	_, err = f.Service.Resume(ctx, owner(), ControlRequest{Mutation: mutation("resume-canceled", canceled.Version), ID: task.ID})
	wantCode(t, err, core.FailedPrecondition)
}
func TestTDDRevisionPreservesOutputsAndTotalSpend(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	task := createTask(t, f)
	execution := f.ExecutionFor(task.ID)
	running, err := f.Execution.Report(ctx, execution, Progress{TaskID: task.ID, State: Running, ExpectedVersion: task.Version})
	if err != nil {
		t.Fatal(err)
	}
	spent := core.Money{Currency: "USD", Micros: 125000}
	f.RecordSpent(task.ID, spent)
	// Read current version because persisted accounting may have advanced it.
	running, err = f.Service.Get(ctx, owner(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := f.Execution.Deliver(ctx, execution, Delivery{Mutation: mutation("deliver", running.Version), TaskID: task.ID, Revision: task.Revision, ArtifactIDs: []core.ID{f.ArtifactID}, Checks: []Check{{Criterion: "report exists", Passed: true, EvidenceIDs: []core.ID{f.EvidenceID}}}})
	if err != nil || completed.State != Completed {
		t.Fatalf("delivery: %+v %v", completed, err)
	}
	revised, err := f.Service.Revise(ctx, owner(), ReviseRequest{Mutation: mutation("revise", completed.Version), ID: task.ID, Instructions: "Add citations", AcceptanceCriteria: []string{"citations exist"}})
	if err != nil || revised.ID != task.ID || revised.Revision != task.Revision+1 || revised.Spent != spent {
		t.Fatalf("revision: %+v %v", revised, err)
	}
	history, err := f.Service.Revisions(ctx, owner(), task.ID, core.PageRequest{})
	if err != nil || len(history.Items) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	var first, second *Revision
	for i := range history.Items {
		r := &history.Items[i]
		if r.Number == 1 {
			first = r
		}
		if r.Number == 2 {
			second = r
		}
	}
	if first == nil || second == nil || !reflect.DeepEqual(first.ArtifactIDs, []core.ID{f.ArtifactID}) || second.Instructions != "Add citations" {
		t.Fatalf("revision history lost: %+v", history.Items)
	}
	_, err = f.Service.Revisions(ctx, otherOwner(), task.ID, core.PageRequest{})
	wantDenied(t, err)
}
func TestTDDDeliveryRejectsClaimsStaleRevisionAndWrongExecution(t *testing.T) {
	for _, tc := range []string{"no-evidence", "failed-check", "wrong-revision", "wrong-assistant", "wrong-task"} {
		t.Run(tc, func(t *testing.T) {
			f := testkit.RequireFactory(t, "task", newContractFixture)
			ctx := testkit.Context(t)
			task := createTask(t, f)
			execution := f.ExecutionFor(task.ID)
			running, err := f.Execution.Report(ctx, execution, Progress{TaskID: task.ID, State: Running, ExpectedVersion: task.Version})
			if err != nil {
				t.Fatal(err)
			}
			d := Delivery{Mutation: mutation("invalid-delivery", running.Version), TaskID: task.ID, Revision: task.Revision, ArtifactIDs: []core.ID{f.ArtifactID}, Checks: []Check{{Criterion: "report exists", Passed: true, EvidenceIDs: []core.ID{f.EvidenceID}}}}
			switch tc {
			case "no-evidence":
				d.Checks[0].EvidenceIDs = nil
			case "failed-check":
				d.Checks[0].Passed = false
			case "wrong-revision":
				d.Revision++
			case "wrong-assistant":
				execution.AssistantID = "other-assistant"
				execution.Principal.SubjectID = "other-assistant"
			case "wrong-task":
				execution.TaskID = "other-task"
			}
			result, err := f.Execution.Deliver(ctx, execution, d)
			// Failed evidence may produce a non-completed outcome; it may never complete.
			if err == nil && (tc != "failed-check" || result.State == Completed) {
				t.Fatalf("invalid delivery accepted: %+v", result)
			}
			got, err := f.Service.Get(ctx, owner(), task.ID)
			if err != nil || got.State == Completed {
				t.Fatalf("invalid evidence completed task: %+v %v", got, err)
			}
		})
	}
}
func TestTDDReportCannotBypassDeliveryOrCancel(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	task := createTask(t, f)
	execution := f.ExecutionFor(task.ID)
	_, err := f.Execution.Report(ctx, execution, Progress{TaskID: task.ID, State: Completed, ExpectedVersion: task.Version})
	if err == nil {
		t.Fatal("Report bypassed verified delivery")
	}
	canceled, err := f.Service.Cancel(ctx, owner(), ControlRequest{Mutation: mutation("cancel", task.Version), ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Execution.Report(ctx, execution, Progress{TaskID: task.ID, State: Running, ExpectedVersion: canceled.Version})
	if err == nil {
		t.Fatal("old execution restarted canceled task")
	}
}
func TestTDDTaskRepositoryAndAppendOnlyRevisions(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	v := Task{ID: "task-row", OwnerID: "owner", AssistantID: "assistant", Goal: "Report", State: Pending, Revision: 1, Version: 1}
	if err := f.Repository.Insert(ctx, v); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Insert(ctx, v), core.Conflict)
	got, err := f.Repository.Get(ctx, "owner", v.ID)
	if err != nil || got.ID != v.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	_, err = f.Repository.Get(ctx, "other", v.ID)
	wantDenied(t, err)
	page, err := f.Repository.List(ctx, "owner", Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	v.Version = 2
	v.State = Running
	if err = f.Repository.Save(ctx, v, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Save(ctx, v, 1), core.Conflict)
	revision := Revision{Number: 1, Instructions: "Original"}
	if err = f.Repository.AppendRevision(ctx, "owner", v.ID, revision); err != nil {
		t.Fatal(err)
	}
	revision.Instructions = "Overwrite"
	wantCode(t, f.Repository.AppendRevision(ctx, "owner", v.ID, revision), core.Conflict)
	history, err := f.Repository.Revisions(ctx, "owner", v.ID, core.PageRequest{})
	if err != nil || len(history.Items) != 1 || history.Items[0].Instructions != "Original" {
		t.Fatalf("history overwritten: %+v %v", history, err)
	}
	_, err = f.Repository.Revisions(ctx, "other", v.ID, core.PageRequest{})
	wantDenied(t, err)
	wantDenied(t, f.Repository.AppendRevision(ctx, "other", v.ID, Revision{Number: 2, Instructions: "Foreign"}))
}
func TestTDDTaskCreationCommitFailureIsAtomic(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	f.FailCommitOnce()
	_, err := f.Service.Create(testkit.Context(t), owner(), CreateRequest{Mutation: mutation("failed-create", 0), AssistantID: "assistant", Goal: "Report"})
	if err == nil {
		t.Fatal("failed commit succeeded")
	}
	tasks, revisions, events := f.Observe()
	if tasks+revisions+events != 0 {
		t.Fatalf("partial task: %d %d %d", tasks, revisions, events)
	}
}

func TestTDDTaskAllControlsRejectForeignOwner(t *testing.T) {
	f := testkit.RequireFactory(t, "task", newContractFixture)
	ctx := testkit.Context(t)
	task := createTask(t, f)
	_, err := f.Service.Revise(ctx, otherOwner(), ReviseRequest{Mutation: mutation("foreign-revise", task.Version), ID: task.ID, Instructions: "Steal"})
	wantDenied(t, err)
	_, err = f.Service.Resume(ctx, otherOwner(), ControlRequest{Mutation: mutation("foreign-resume", task.Version), ID: task.ID})
	wantDenied(t, err)
	_, err = f.Service.Cancel(ctx, otherOwner(), ControlRequest{Mutation: mutation("foreign-cancel", task.Version), ID: task.ID})
	wantDenied(t, err)
	got, err := f.Service.Get(ctx, owner(), task.ID)
	if err != nil || got.Version != task.Version || got.State != task.State {
		t.Fatalf("foreign commands mutated task: %+v %v", got, err)
	}
}
