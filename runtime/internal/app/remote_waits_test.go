package app

import (
	"context"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"
)

func TestRemoteFailureDoesNotRestoreStoppedWork(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	router, err := newSandboxRouter(f.ad, nil, make([]byte, 32))
	need(t, err)
	e := f.execution
	e.TaskID = "remote-task"
	e.OperationID = "remote-operation"
	now := time.Now().UTC()
	created := now.Add(-time.Minute)
	need(t, task.NewRepository(f.b.store).Insert(t.Context(), task.Task{ID: e.TaskID, OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, State: task.Running, Version: 1, CreatedAt: created, UpdatedAt: created}))
	need(t, operation.NewRepository(f.b.store).Insert(t.Context(), operation.Operation{ID: e.OperationID, OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, ExecutionID: e.Principal.ExecutionID, Action: "shell_execute", State: operation.Unknown, IdempotencyKey: "remote-wait", InputDigest: "digest", Version: 1, CreatedAt: created, UpdatedAt: created}))
	router.rememberRemoteWait(e, "remote", core.NewError(core.Unavailable, "offline"))
	waits, err := persistence.Scan[remoteWait](t.Context(), f.b.store, remoteWaitBucket)
	need(t, err)
	if len(waits) != 1 || !waits[0].CreatedAt.Equal(created) {
		t.Fatal(waits)
	}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Delete(ctx, remoteWaitBucket, persistence.Key(e.Principal.OwnerID, e.TaskID)); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.interrupt_boundaries", persistence.Key(e.Principal.OwnerID, e.AssistantID), event.InterruptBoundary{At: now})
	}))
	// A late transport error belongs to the old operation, even if it arrives
	// after the user stopped execution. It must not install a fresh wakeup.
	router.rememberRemoteWait(e, "remote", core.NewError(core.Unavailable, "late offline result"))
	waits, err = persistence.Scan[remoteWait](t.Context(), f.b.store, remoteWaitBucket)
	need(t, err)
	if len(waits) != 0 {
		t.Fatal("late failure resurrected stopped work", waits)
	}
}
