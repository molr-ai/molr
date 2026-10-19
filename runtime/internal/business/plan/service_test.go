package plan

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type allowOwner struct{}

func (allowOwner) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}
func owner() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
}
func fixture(t *testing.T) (*Coordinator, *sqlitestore.Store) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active, Version: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	var s *Coordinator
	tasks := task.NewService(db, task.Options{Authorizer: allowOwner{}, CheckAssistant: func(ctx context.Context, p core.Principal, id core.ID) error {
		a, err := assistant.NewRepository(db).Get(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		if a.State != assistant.Active {
			return core.NewError(core.Forbidden, "inactive")
		}
		return nil
	}, BindPlan: func(ctx context.Context, p core.Principal, v *task.Task, r task.CreateRequest) error {
		return s.BindTask(ctx, p, v, r)
	}, Emit: func(ctx context.Context, p core.Principal, v task.Task, _ string) error {
		return event.NewWriter(db).AppendAssistant(ctx, event.AssistantEvent{ID: id(), OwnerID: p.OwnerID, AssistantID: v.AssistantID, TaskID: v.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()})
	}})
	s = New(db, allowOwner{}, tasks)
	return s, db
}
func add(t *testing.T, s *Coordinator, ref core.ID, assignee core.ID) core.Object {
	t.Helper()
	v := todo.Todo{ID: ref, OwnerID: "owner", AssistantID: assignee, Title: string(ref), Version: 1}
	if err := todo.NewRepository(s.store).Insert(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	return core.Object{Kind: "todo", ID: ref}
}
func mutation(key string, version core.Version) core.Mutation {
	return core.Mutation{IdempotencyKey: key, ExpectedVersion: version}
}
func TestPersonalHandoffCreatesExactlyOneLinkedExecutionWithSnapshot(t *testing.T) {
	s, db := fixture(t)
	ctx := context.Background()
	prep := add(t, s, "prepare", "")
	analysis := add(t, s, "analyze", "assistant")
	_, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("configure", 0), Ref: analysis, Dependencies: []core.Object{prep}, Handoff: "automatic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks, _ := persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 0 {
		t.Fatal("ran before prerequisite")
	}
	complete := ExecuteRequest{Mutation: mutation("complete", 0), Ref: prep, Action: "complete", Result: "資料已准备：销量 120，成本 80。"}
	first, err := s.Execute(ctx, owner(), complete)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Execute(ctx, owner(), complete)
	if err != nil || again.ID != first.ID {
		t.Fatal("completion replay duplicated")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Tick(ctx, time.Now()) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	tasks, _ = persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 1 || tasks[0].Plan != analysis || tasks[0].PlanTitle != "analyze" || !strings.Contains(tasks[0].Goal, "销量 120") {
		t.Fatalf("bad handoff: %+v", tasks)
	}
	original := tasks[0].PlanInstructions
	item, _ := todo.NewRepository(db).Get(ctx, "owner", "analyze")
	item.Notes = "new instructions"
	item.Version++
	if err := todo.NewRepository(db).Save(ctx, item, 1); err != nil {
		t.Fatal(err)
	}
	stored, _ := task.NewRepository(db).Get(ctx, "owner", tasks[0].ID)
	if stored.PlanInstructions != original {
		t.Fatal("historical snapshot changed")
	}
	plans, err := s.List(ctx, owner(), time.Time{}, time.Time{})
	if err != nil || len(plans) != 2 {
		t.Fatalf("list: %v %v", plans, err)
	}
}
func TestDependenciesRejectCyclesAndCrossOwnerAndPausePreventsHandoff(t *testing.T) {
	s, db := fixture(t)
	ctx := context.Background()
	a := add(t, s, "a", "")
	b := add(t, s, "b", "assistant")
	links, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("b", 0), Ref: b, Dependencies: []core.Object{a}, Handoff: "automatic", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("cycle", 0), Ref: a, Dependencies: []core.Object{b}, Handoff: "notify"}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("cycle accepted: %v", err)
	}
	other := owner()
	other.OwnerID = "other"
	other.SubjectID = "other"
	if _, err = s.Configure(ctx, other, ConfigureRequest{Mutation: mutation("cross", 0), Ref: b, Handoff: "notify"}); core.CodeOf(err) != core.NotFound {
		t.Fatalf("cross owner: %v", err)
	}
	if _, err = s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("done", 0), Ref: a, Action: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks, _ := persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 0 {
		t.Fatal("paused handoff executed")
	}
	if _, err = s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("resume", links.Version), Ref: b, Dependencies: []core.Object{a}, Handoff: "automatic"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks, _ = persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 1 {
		t.Fatal("resumed handoff missing")
	}
}
func TestOrdinaryTaskCreatesOnePlan(t *testing.T) {
	s, db := fixture(t)
	ctx := context.Background()
	req := task.CreateRequest{Mutation: mutation("new", 0), AssistantID: "assistant", Goal: "Analyze sales"}
	v, err := s.tasks.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.tasks.Create(ctx, owner(), req)
	if err != nil || again.Plan != v.Plan || v.Plan.ID == "" {
		t.Fatalf("bad link: %+v %v", v, err)
	}
	todos, _ := persistence.Scan[todo.Todo](ctx, db, "business.todos")
	if len(todos) != 1 {
		t.Fatalf("task creation duplicated plan: %d", len(todos))
	}
}
func TestScheduledOccurrencesSharePlanAndRemainIndependent(t *testing.T) {
	s, db := fixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	source := schedule.Schedule{ID: "daily", OwnerID: "owner", Kind: schedule.AssistantPlan, AssistantID: "assistant", Title: "Daily review", Instructions: "Review notes", Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=2", MissedPolicy: schedule.Coalesce}, Enabled: true, NextAt: &at, Version: 1}
	if err := schedule.NewRepository(db).Insert(ctx, source); err != nil {
		t.Fatal(err)
	}
	d := schedule.NewDispatcher(db)
	d.Ready = s.ScheduleReady
	d.Start = s.StartScheduled
	for _, now := range []time.Time{at, at, at.Add(24 * time.Hour), at.Add(24 * time.Hour)} {
		if err := d.Tick(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	tasks, _ := persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 2 || tasks[0].Plan != tasks[1].Plan || tasks[0].OccurrenceAt.Equal(*tasks[1].OccurrenceAt) {
		t.Fatalf("occurrences: %+v", tasks)
	}
	events, _ := persistence.Scan[event.AssistantEvent](ctx, db, "agent.events")
	if len(events) != 2 {
		t.Fatalf("events: %v", events)
	}
	for _, e := range events {
		if e.TaskID == "" || e.Kind != event.ScheduleDue {
			t.Fatalf("missing execution: %+v", e)
		}
	}
}
func TestFailedCommitRollsBackPersonalCompletion(t *testing.T) {
	s, db := fixture(t)
	ref := add(t, s, "prep", "")
	db.FailNextCommit(errors.New("commit failed"))
	_, err := s.Execute(context.Background(), owner(), ExecuteRequest{Mutation: mutation("done", 0), Ref: ref, Action: "complete"})
	if err == nil {
		t.Fatal("expected failure")
	}
	item, _ := todo.NewRepository(db).Get(context.Background(), "owner", ref.ID)
	links, _ := s.links(context.Background(), "owner", ref)
	if item.Completed || len(links.Runs) != 0 {
		t.Fatal("partial completion persisted")
	}
}

func TestNotifyModeDoesNotRunAndPersonalEvidenceCannotBeOverwritten(t *testing.T) {
	s, db := fixture(t)
	ctx := context.Background()
	prep := add(t, s, "prep", "")
	next := add(t, s, "next", "assistant")
	_, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("notify", 0), Ref: next, Dependencies: []core.Object{prep}, Handoff: "notify"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("done", 0), Ref: prep, Action: "complete", Result: "Original evidence"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("overwrite", 1), Ref: prep, Action: "complete", Result: "Replacement"})
	if err != nil || second.Result != first.Result {
		t.Fatal("completed evidence overwritten")
	}
	if err = s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks, _ := persistence.Scan[task.Task](ctx, db, "business.tasks")
	if len(tasks) != 0 {
		t.Fatal("notify unexpectedly executed")
	}
	plans, err := s.List(ctx, owner(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if p.Ref == next && (!p.Ready || len(p.Executions) > 0) {
			t.Fatal("ready prompt missing")
		}
	}
	todos := todo.NewService(db, todo.Options{Authorizer: allowOwner{}})
	item, _ := todo.NewRepository(db).Get(ctx, "owner", prep.ID)
	err = todos.Delete(ctx, owner(), todo.DeleteRequest{Mutation: mutation("delete", item.Version), ID: prep.ID})
	if core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("deleted referenced history: %v", err)
	}
}

func TestPausingFutureArrangementsDoesNotBlockCurrentPersonalExecution(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	ref := add(t, s, "personal", "")
	if _, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("start", 0), Ref: ref, Action: "start"}); err != nil {
		t.Fatal(err)
	}
	links, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("pause", 1), Ref: ref, Paused: true, Handoff: "notify"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("complete", links.Version), Ref: ref, Action: "complete", Result: "Finished current work"})
	if err != nil || run.State != "completed" {
		t.Fatalf("current execution blocked: %+v %v", run, err)
	}
}

func TestReadPlanIncludesVersionsAndInstructions(t *testing.T) {
	s, _ := fixture(t)
	ref := add(t, s, "readable", "assistant")
	got, err := s.Read(t.Context(), owner(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != ref || got.Todo.Version != 1 || got.Title != "readable" || !got.Ready {
		t.Fatalf("read=%+v", got)
	}
	if _, err = s.Read(t.Context(), owner(), core.Object{Kind: "task", ID: "readable"}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("invalid reference accepted: %v", err)
	}
}

func TestNotifyHandoffPersistsOnceAndResolvesAfterStarting(t *testing.T) {
	s, db := fixture(t)
	ctx := t.Context()
	prep := add(t, s, "notify-prep", "")
	next := add(t, s, "notify-next", "assistant")
	if _, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("notify-configure", 0), Ref: next, Dependencies: []core.Object{prep}, Handoff: "notify"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("notify-complete", 0), Ref: prep, Action: "complete"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Tick(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	notices := notification.New(db, allowOwner{})
	page, err := notices.List(ctx, owner(), notification.Query{Filter: "active", Kind: "plan.ready"})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	n := page.Items[0]
	if err := notices.Update(ctx, owner(), []core.ID{n.ID}, "dismiss"); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	all, err := notices.List(ctx, owner(), notification.Query{})
	if err != nil || len(all.Items) != 1 || all.Items[0].DismissedAt == nil {
		t.Fatal(all, err)
	}
	if _, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("notify-start", 1), Ref: next, Action: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := notices.Get(ctx, owner(), n.ID)
	if err != nil || got.ResolvedAt == nil {
		t.Fatal(got, err)
	}
}
