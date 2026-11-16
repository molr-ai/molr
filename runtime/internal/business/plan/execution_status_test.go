package plan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

func TestExecutionStatusProjectsDurableProgressWithoutCompletingGoal(t *testing.T) {
	for _, tc := range []struct {
		job           event.JobState
		outcome, want string
	}{
		{event.Queued, "", "pending"}, {event.Running, "", "running"}, {event.Retry, "", "retry"},
		{event.Done, "idle", "executed"}, {event.Done, "waiting", "waiting"}, {event.Done, "", "needs_review"},
		{event.Failed, "", "failed"}, {event.NeedsReview, "", "needs_review"}, {event.Canceled, "", "canceled"}, {event.Interrupted, "", "interrupted"},
	} {
		t.Run(string(tc.job)+tc.outcome, func(t *testing.T) {
			s, db := fixture(t)
			ref := add(t, s, "ordinary", "assistant")
			run, err := s.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation("start", 0), Ref: ref, Action: "start"})
			if err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC().Add(time.Second)
			j := event.Job{Event: event.AssistantEvent{ID: "observed", OwnerID: "owner", AssistantID: "assistant", TaskID: run.ID, CreatedAt: at}, State: tc.job, ExecutionID: "execution", Version: 1}
			err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if err := event.SaveJob(ctx, tx, j); err != nil {
					return err
				}
				if err := tx.Put(ctx, "agent.turn_timing", persistence.Key("owner", j.ExecutionID), map[string]any{"StartedAt": at.Add(200 * time.Millisecond)}); err != nil {
					return err
				}
				if tc.outcome == "" {
					return nil
				}
				return tx.Put(ctx, "agent.handled", "owner/receipt", map[string]any{
					"Status":     map[string]any{"State": tc.outcome, "ExecutionID": j.ExecutionID, "ActiveTaskID": run.ID, "WaitingReason": "permission_pending"},
					"Checkpoint": map[string]any{"OwnerID": "owner", "LastEventID": j.Event.ID, "SavedAt": at.Add(time.Second), "Context": "private context must not be hydrated"},
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			detail, err := s.Detail(t.Context(), owner(), ref, nil)
			if err != nil {
				t.Fatal(err)
			}
			history, err := s.History(t.Context(), owner(), ref, core.PageRequest{Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			full, err := s.Execution(t.Context(), owner(), ref, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			read, err := s.Read(t.Context(), owner(), ref)
			if err != nil {
				t.Fatal(err)
			}
			list, err := s.List(t.Context(), owner(), time.Time{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			for name, state := range map[string]string{"detail": detail.Executions[0].State, "history": history.Items[0].State, "execution": full.State, "read": read.Executions[0].State, "list": list[0].Executions[0].State} {
				if state != tc.want {
					t.Fatalf("%s state=%s want=%s", name, state, tc.want)
				}
			}
			if full.RunStartedAt == nil || !full.RunStartedAt.Equal(at.Add(200*time.Millisecond)) || full.RunStartedAt.Equal(full.StartedAt) {
				t.Fatal("actual start confused with creation time", full.RunStartedAt, full.StartedAt)
			}
			if full.Task.State != task.Pending || full.TaskState != "pending" {
				t.Fatal("projection changed goal state", full)
			}
			saved, err := task.NewRepository(db).Get(t.Context(), "owner", run.ID)
			if err != nil || saved.State != task.Pending || saved.Version != 1 {
				t.Fatal("read changed durable task", saved, err)
			}
			if (full.CompletedAt != nil) != (tc.want == "executed") {
				t.Fatal("incorrect completion time", full)
			}
			if tc.want == "waiting" && full.WaitingReason != "permission_pending" {
				t.Fatal("lost wait reason")
			}
			// Runtime completion must not unlock a dependent goal or permit duplicate work.
			if tc.want == "executed" {
				_, err = s.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation("duplicate", 0), Ref: ref, Action: "start"})
				if core.CodeOf(err) != core.FailedPrecondition {
					t.Fatal("unverified goal permitted duplicate execution", err)
				}
			}
		})
	}
}

func TestExecutionStatusUsesLatestOwnedJobAndPreservesControls(t *testing.T) {
	s, db := fixture(t)
	ref := add(t, s, "status-controls", "assistant")
	run, err := s.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation("start", 0), Ref: ref, Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(time.Second)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i, j := range []event.Job{
			{Event: event.AssistantEvent{OwnerID: "owner", AssistantID: "assistant", TaskID: run.ID, CreatedAt: at}, State: event.Interrupted},
			{Event: event.AssistantEvent{OwnerID: "owner", AssistantID: "assistant", TaskID: run.ID, CreatedAt: at.Add(time.Second)}, State: event.Running},
			{Event: event.AssistantEvent{OwnerID: "other", AssistantID: "assistant", TaskID: run.ID, CreatedAt: at.Add(2 * time.Second)}, State: event.Failed},
			{Event: event.AssistantEvent{OwnerID: "owner", AssistantID: "other", TaskID: run.ID, CreatedAt: at.Add(3 * time.Second)}, State: event.Failed},
		} {
			j.Event.ID = core.ID(fmt.Sprintf("job-%d", i))
			j.Version = 1
			if err := event.SaveJob(ctx, tx, j); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		v, err := s.Execution(t.Context(), owner(), ref, run.ID)
		if err != nil || v.State != want {
			t.Fatal(want, v.State, err)
		}
	}
	check("running")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: at.Add(2 * time.Second)})
	})
	if err != nil {
		t.Fatal(err)
	}
	check("canceled")
	for _, state := range []task.State{task.Paused, task.Canceled, task.Completed, task.Failed} {
		err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			v, err := task.NewRepository(db).Get(ctx, "owner", run.ID)
			if err != nil {
				return err
			}
			v.State = state
			return tx.Put(ctx, "business.tasks", persistence.Key("owner", run.ID), v)
		})
		if err != nil {
			t.Fatal(err)
		}
		check(string(state))
	}
}
