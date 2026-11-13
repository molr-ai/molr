package planstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestDeletePreservesHistoryAndCancelsAllWork(t *testing.T) {
	for _, kind := range []string{"todo", "schedule", "watch"} {
		t.Run(kind, func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := t.Context()
			ref := core.Object{Kind: kind, ID: "plan"}
			err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				for _, owner := range []core.ID{"owner", "other"} {
					if err := tx.Put(ctx, Bucket(ref), persistence.Key(owner, ref.ID), map[string]any{"ID": ref.ID, "OwnerID": owner, "State": Active, "Version": 1, "Recent": []string{"history"}, "RunID": "watch-run"}); err != nil {
						return err
					}
					for _, state := range []string{"pending", "running", "waiting", "paused", "completed", "failed", "canceled"} {
						id := core.ID(state)
						if err := tx.Put(ctx, "business.tasks", persistence.Key(owner, id), map[string]any{"ID": id, "OwnerID": owner, "Plan": ref, "State": state, "Version": 1, "Goal": "preserve history"}); err != nil {
							return err
						}
						for _, js := range []event.JobState{event.Queued, event.Running, event.Retry, event.Interrupted, event.Done} {
							job := event.Job{Event: event.AssistantEvent{ID: core.ID(state + "-" + string(js)), OwnerID: owner, AssistantID: "assistant", TaskID: id}, State: js, Version: 1}
							if err := event.SaveJob(ctx, tx, job); err != nil {
								return err
							}
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			// A failed transaction changes neither the plan nor its jobs/tasks.
			injected := errors.New("commit failed")
			db.FailNextCommit(injected)
			if err = Delete(ctx, db, "owner", ref, 1); !errors.Is(err, injected) {
				t.Fatal(err)
			}
			state, err := Load(ctx, db, "owner", ref)
			if err != nil || state != Active {
				t.Fatal(state, err)
			}
			if err = Delete(ctx, db, "owner", ref, 1); err != nil {
				t.Fatal(err)
			}
			if err = Delete(ctx, db, "owner", ref, 1); err != nil {
				t.Fatal("repeat delete", err)
			}
			state, err = Load(ctx, db, "owner", ref)
			if err != nil || state != Deleted || !persistence.Missing(Visible(state)) {
				t.Fatal(state, err)
			}
			state, err = Load(ctx, db, "other", ref)
			if err != nil || state != Active {
				t.Fatal("other owner changed", state, err)
			}
			tasks, err := persistence.Scan[struct {
				OwnerID     core.ID
				State, Goal string
			}](ctx, db, "business.tasks")
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range tasks {
				if task.OwnerID == "owner" && task.State != "completed" && task.State != "failed" && task.State != "canceled" {
					t.Fatal("work survived", task)
				}
				if task.Goal != "preserve history" {
					t.Fatal("history removed")
				}
			}
			jobs, err := persistence.Scan[event.Job](ctx, db, "agent.jobs")
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range jobs {
				if job.Event.OwnerID == "owner" && job.State != event.Done && job.State != event.Canceled {
					t.Fatal("job survived", job)
				}
			}
			err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return Set(ctx, tx, "owner", ref, Active, 2) })
			if !persistence.Missing(err) {
				t.Fatal("deleted plan restored", err)
			}
		})
	}
}

func TestOnlyUndeletedDependenciesPreventDeletion(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	ref := core.Object{Kind: "todo", ID: "first"}
	next := core.Object{Kind: "schedule", ID: "next"}
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, v := range []core.Object{ref, next} {
			if err := tx.Put(ctx, Bucket(v), persistence.Key("owner", v.ID), map[string]any{"State": Paused, "Version": 1}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "business.plan-links", "owner/schedule-next", map[string]any{"OwnerID": "owner", "Ref": next, "Dependencies": []core.Object{ref}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = Delete(ctx, db, "owner", ref, 1); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("paused dependent allowed deletion", err)
	}
	if err = Delete(ctx, db, "owner", next, 1); err != nil {
		t.Fatal(err)
	}
	if err = Delete(ctx, db, "owner", ref, 1); err != nil {
		t.Fatal("deleted dependency blocked deletion", err)
	}
	if _, err = Load(ctx, db, "owner", core.Object{Kind: "invalid", ID: "first"}); err == nil {
		t.Fatal("invalid kind")
	}
}

func TestLifecycleVersionAndCommitNotification(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	ref := core.Object{Kind: "todo", ID: "plan"}
	if err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, Bucket(ref), "owner/plan", map[string]any{"State": Active, "Version": 1})
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := db.Watch(persistence.RecordSignal(Bucket(ref), "owner/plan"))
	if err != nil {
		t.Fatal(err)
	}
	defer changes.Close()
	set := func(state State, v core.Version) error {
		return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return Set(ctx, tx, "owner", ref, state, v) })
	}
	if core.CodeOf(set(Paused, 99)) != core.Conflict {
		t.Fatal("stale update")
	}
	if set("invalid", 1) == nil {
		t.Fatal("invalid state")
	}
	select {
	case <-changes.Changes():
		t.Fatal("failed update notified")
	default:
	}
	if err = set(Paused, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes.Changes():
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}
	if err = set(Active, 2); err != nil {
		t.Fatal(err)
	}
}

func TestStateChangeRefreshesHandoffCandidate(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	ref := core.Object{Kind: "schedule", ID: "plan"}
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, Bucket(ref), "owner/plan", map[string]any{"State": Active, "Version": 1}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "business.plan-links", "owner/schedule-plan", map[string]any{"Handoff": "notify", "Dependencies": []core.Object{{Kind: "todo", ID: "before"}}}); err != nil {
			return err
		}
		return RefreshHandoff(ctx, tx, "owner", ref, Active)
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, state := range []State{Paused, Active} {
		err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return Set(ctx, tx, "owner", ref, state, core.Version(i+1))
		})
		if err != nil {
			t.Fatal(err)
		}
		links, err := persistence.Load[struct{ HandoffPending bool }](ctx, db, "business.plan-links", "owner/schedule-plan")
		if err != nil || links.HandoffPending != (state == Active) {
			t.Fatal(state, links, err)
		}
	}
}

func TestDeleteCancelsPersonalRunsAndSourceEvents(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	ref := core.Object{Kind: "schedule", ID: "plan"}
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, Bucket(ref), "owner/plan", map[string]any{"State": Active, "Version": 1}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "business.plan-links", "owner/schedule-plan", map[string]any{"Version": 1, "Runs": []map[string]string{{"State": "running"}, {"State": "completed", "Result": "preserve"}}}); err != nil {
			return err
		}
		return event.SaveJob(ctx, tx, event.Job{Event: event.AssistantEvent{ID: "source-event", OwnerID: "owner", ScheduleID: ref.ID}, State: event.Queued, Version: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = Delete(ctx, db, "owner", ref, 1); err != nil {
		t.Fatal(err)
	}
	links, err := persistence.Load[struct {
		Runs []struct {
			State, Result string
			CompletedAt   *time.Time
		}
	}](ctx, db, "business.plan-links", "owner/schedule-plan")
	if err != nil {
		t.Fatal(err)
	}
	if links.Runs[0].State != "canceled" || links.Runs[0].CompletedAt == nil || links.Runs[1].Result != "preserve" {
		t.Fatal(links)
	}
	job, err := persistence.Load[event.Job](ctx, db, "agent.jobs", "owner/source-event")
	if err != nil || job.State != event.Canceled {
		t.Fatal(job, err)
	}
}
