package plan

import (
	"context"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"testing"
	"time"
)

func TestContinuousPlansListWithoutCalendarOccurrences(t *testing.T) {
	s, db := fixture(t)
	svc := watch.New(db, watch.Options{Authorizer: allowOwner{}})
	w, err := svc.Save(t.Context(), owner(), watch.Request{Mutation: mutation("create-watch", 0), AssistantID: "assistant", Title: "Issues", Instructions: "Notify about changes", Watcher: watch.Config{ComputeID: "compute", Script: "private-program"}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(t.Context(), owner(), time.Now().AddDate(0, 0, 1), time.Now().AddDate(0, 0, 2))
	if err != nil || len(rows) != 1 || rows[0].Watch == nil {
		t.Fatal(rows, err)
	}
	calendar, err := s.Overview(t.Context(), owner(), time.Now(), time.Now().AddDate(0, 0, 7))
	if err != nil || len(calendar) != 0 {
		t.Fatal("watch entered calendar", calendar, err)
	}
	events, err := s.Events(t.Context(), owner(), EventQuery{From: time.Now()})
	if err != nil || len(events.Items) != 0 {
		t.Fatal("watch entered occurrences", events, err)
	}
	page, err := s.WatchPage(t.Context(), owner(), "assistant", "open", core.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Watch.Watcher.Script != "" {
		t.Fatal("watch list projection", page, err)
	}
	ref, err := s.Resolve(t.Context(), owner(), w.ID)
	if err != nil || ref.Kind != "watch" {
		t.Fatal(ref, err)
	}
	full, err := s.Detail(t.Context(), owner(), ref, nil)
	if err != nil || full.Watch.Watcher.Script != "private-program" {
		t.Fatal(full, err)
	}
	other := core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "other", ExecutionID: "run"}
	hidden, err := s.WatchPage(t.Context(), other, "all", "all", core.PageRequest{})
	if err != nil || len(hidden.Items) > 0 {
		t.Fatal("cross-assistant watch", hidden, err)
	}
}
func TestWatchBatchesAreBoundedImmutableAndIndependentOfObserver(t *testing.T) {
	s, db := fixture(t)
	svc := watch.New(db, watch.Options{Authorizer: allowOwner{}})
	w, err := svc.Save(t.Context(), owner(), watch.Request{Mutation: mutation("watch", 0), AssistantID: "assistant", Title: "Watch", Instructions: "Handle changes", Watcher: watch.Config{ComputeID: "compute", Script: "watcher"}})
	if err != nil {
		t.Fatal(err)
	}
	w, err = svc.Begin(t.Context(), w.OwnerID, w.ID, w.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{`{"type":"ready","version":1}`, `{"type":"checkpoint","state":1}`} {
		if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	jobs, _ := persistence.Scan[event.AssistantEvent](t.Context(), db, "agent.events")
	if len(jobs) != 0 {
		t.Fatal("checkpoint woke model")
	}
	if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"one","data":{"text":"first"}}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	if len(tasks) != 1 || !strings.Contains(tasks[0].PlanInstructions, "first") || tasks[0].Plan.Kind != "watch" {
		t.Fatal(tasks)
	}
	snapshot := tasks[0].PlanInstructions
	if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"two","data":{"text":"second"}}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ = persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	if len(tasks) != 1 || tasks[0].PlanInstructions != snapshot {
		t.Fatal("running batch changed")
	}
	tasks[0].State = task.Completed
	tasks[0].Version++
	if err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.tasks", persistence.Key(w.OwnerID, tasks[0].ID), tasks[0])
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(`{"type":"complete"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ = persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	if len(tasks) != 2 {
		t.Fatal("completion lost pending changes", tasks)
	}
	history, err := s.History(t.Context(), owner(), core.Object{Kind: "watch", ID: w.ID}, core.PageRequest{})
	if err != nil || len(history.Items) != 2 {
		t.Fatal(history, err)
	}
}

func TestWatchHandlingCompletionReleasesOnlyItsBatch(t *testing.T) {
	s, db := fixture(t)
	svc := watch.New(db, watch.Options{Authorizer: allowOwner{}})
	w, err := svc.Save(t.Context(), owner(), watch.Request{Mutation: mutation("watch-turns", 0), AssistantID: "assistant", Title: "Watch", Instructions: "Handle changes", Watcher: watch.Config{ComputeID: "compute", Script: "watcher"}})
	if err != nil {
		t.Fatal(err)
	}
	w, err = svc.Begin(t.Context(), w.OwnerID, w.ID, w.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{`{"type":"ready","version":1}`, `{"type":"event","key":"one","data":1}`} {
		if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	runs, _ := persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	id := runs[0].ID
	if err = s.RecordWatchHandling(t.Context(), w.OwnerID, w.AssistantID, id, task.Running, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordWatchHandling(t.Context(), w.OwnerID, w.AssistantID, id, task.Waiting, "permission_pending"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Receive(t.Context(), w.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"two","data":2}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	runs, _ = persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	if len(runs) != 1 {
		t.Fatal("permission wait released batch")
	}
	if err = s.RecordWatchHandling(t.Context(), w.OwnerID, w.AssistantID, id, task.Completed, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchWatch(t.Context(), w.OwnerID, w.ID); err != nil {
		t.Fatal(err)
	}
	runs, _ = persistence.Scan[task.Task](t.Context(), db, "business.tasks")
	if len(runs) != 2 {
		t.Fatal("next batch blocked")
	}
	current, _ := svc.Get(t.Context(), owner(), w.ID)
	if current.State != "running" {
		t.Fatal("handling completed the watcher")
	}
	ordinary := add(t, s, "ordinary", "assistant")
	run, err := s.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation("ordinary-start", 0), Ref: ordinary, Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordWatchHandling(t.Context(), "owner", "assistant", run.ID, task.Completed, ""); err != nil {
		t.Fatal(err)
	}
	original, _ := task.NewRepository(db).Get(t.Context(), "owner", run.ID)
	if original.State != task.Pending {
		t.Fatal("ordinary goal was marked delivered")
	}
}
