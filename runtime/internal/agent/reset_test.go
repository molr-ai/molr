package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestResetContextPreservesProductDataAndFencesOldEvents(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	ctx := testkit.Context(t)
	out, err := f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	engine := f.Runtime.(*Engine)
	old := f.Turn.Event
	old.ID = "queued-before-reset"
	testkit.RequireNoError(t, event.NewWriter(db).AppendAssistant(ctx, old))
	buckets := []string{"im.messages", "business.assistants", "security.grants", "security.reservations", "agent.operations", "agent.model_receipts", "agent.handled"}
	snapshot := func() [][]persistence.Record {
		var rows [][]persistence.Record
		for _, bucket := range buckets {
			var v []persistence.Record
			err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
				var err error
				v, err = tx.Scan(ctx, bucket)
				return err
			})
			testkit.RequireNoError(t, err)
			rows = append(rows, v)
		}
		return rows
	}
	before := snapshot()
	mutation := core.Mutation{IdempotencyKey: "reset"}
	err = engine.ResetContext(ctx, f.Owner, old.AssistantID, mutation, out.Checkpoint.Sequence+1)
	testkit.RequireCode(t, err, core.Conflict)
	testkit.RequireNoError(t, engine.ResetContext(ctx, f.Owner, old.AssistantID, mutation, out.Checkpoint.Sequence))
	saved, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, old.AssistantID)
	testkit.RequireNoError(t, err)
	if saved.Sequence != out.Checkpoint.Sequence+1 || string(saved.Context) != `{"Inputs":[]}` {
		t.Fatalf("context not reset: %+v", saved)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("reset modified chat, assistant, permissions or billing evidence")
	}
	status, err := engine.Status(ctx, f.Owner, old.AssistantID)
	testkit.RequireNoError(t, err)
	if status.State != Idle || status.ExecutionID != "" || status.WaitingReason != "" {
		t.Fatalf("status=%+v", status)
	}
	jobs, err := scheduler.NewQueue(db).List(ctx, f.Owner.OwnerID, scheduler.Query{})
	testkit.RequireNoError(t, err)
	for _, job := range jobs.Items {
		if job.Event.ID == old.ID && job.State != scheduler.Canceled {
			t.Fatal("queued event remains runnable")
		}
	}
	turn := f.Turn
	turn.Event = old
	turn.Principal.ExecutionID = "old-delayed-run"
	_, err = f.Restart().Handle(ctx, turn)
	testkit.RequireCode(t, err, core.Canceled)
	// Idempotent retry must not reset context a second time, even after new work.
	turn.Event.ID = "new-event"
	turn.Event.CreatedAt = time.Now().Add(time.Second)
	turn.Principal.ExecutionID = "new-run"
	next, err := f.Restart().Handle(ctx, turn)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, engine.ResetContext(ctx, f.Owner, old.AssistantID, mutation, out.Checkpoint.Sequence))
	after, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, old.AssistantID)
	testkit.RequireNoError(t, err)
	if after.Sequence != next.Checkpoint.Sequence || string(after.Context) != string(next.Checkpoint.Context) {
		t.Fatal("reset retry erased newer work")
	}
	if len(f.ModelRequests()) != 4 {
		t.Fatal("reset or delayed event dispatched model")
	}
	var inputs struct{ Inputs []json.RawMessage }
	testkit.RequireNoError(t, json.Unmarshal(next.Checkpoint.Context, &inputs))
	if len(inputs.Inputs) != 5 {
		t.Fatalf("new execution retained old context: %d inputs", len(inputs.Inputs))
	}
}

func TestResetContextCancelsActiveCallAndRejectsLateCheckpoint(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte(`{"id":"late","choices":[{"message":{"role":"assistant","content":"old"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer close(release)
	ctx := testkit.Context(t)
	done := make(chan error, 1)
	go func() { _, err := f.Runtime.Handle(ctx, f.Turn); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("model did not start")
	}
	engine := f.Runtime.(*Engine)
	before, loadErr := f.Checkpoints.Load(ctx, f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, loadErr)
	testkit.RequireNoError(t, engine.ResetContext(ctx, f.Owner, f.Turn.Event.AssistantID, core.Mutation{IdempotencyKey: "active-reset"}, before.Sequence))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("old execution succeeded after reset")
		}
	case <-ctx.Done():
		t.Fatal("active request did not cancel")
	}
	checkpoint, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if checkpoint.Sequence != before.Sequence+1 || string(checkpoint.Context) != `{"Inputs":[]}` {
		t.Fatal("late execution overwrote reset")
	}
	status, err := engine.Status(ctx, f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if status.State != Idle {
		t.Fatalf("late failure overwrote idle status: %+v", status)
	}
}

func TestResetContextOwnershipAndTransactionRollback(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	ctx := testkit.Context(t)
	engine := f.Runtime.(*Engine)
	id := f.Turn.Event.AssistantID
	mutation := core.Mutation{IdempotencyKey: "reset"}
	if err := engine.ResetContext(ctx, f.Other, id, mutation, 0); err == nil {
		t.Fatal("foreign owner reset assistant")
	}
	if err := engine.ResetContext(ctx, f.Turn.Principal, id, mutation, 0); err == nil {
		t.Fatal("assistant reset itself")
	}
	f.FailNextCommit()
	if err := engine.ResetContext(ctx, f.Owner, id, mutation, 0); err == nil {
		t.Fatal("commit failure ignored")
	}
	_, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, id)
	testkit.RequireCode(t, err, core.NotFound)
	testkit.RequireNoError(t, engine.ResetContext(ctx, f.Owner, id, mutation, 0))
	// Dismissed assistants must not become runnable through the debug endpoint.
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(f.Owner.OwnerID, id), map[string]any{"OwnerID": f.Owner.OwnerID, "State": "dismissed"})
	}))
	err = engine.ResetContext(ctx, f.Owner, id, core.Mutation{IdempotencyKey: "dismissed"}, 1)
	testkit.RequireCode(t, err, core.FailedPrecondition)
}
