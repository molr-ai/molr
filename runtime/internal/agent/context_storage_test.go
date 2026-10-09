package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestCheckpointStorageSharesMessagesAndRetainsOnlyCurrentReference(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, LastEventID: f.Turn.Event.ID, FormatVersion: 1, Sequence: 1, SavedAt: time.Now(), Context: json.RawMessage(`{"Inputs":[{"Role":"system","Text":"shared"},{"Role":"user","Text":"old"}]}`)}
	if err := f.Checkpoints.Save(t.Context(), cp, 0); err != nil {
		t.Fatal(err)
	}
	raw, err := persistence.Load[json.RawMessage](t.Context(), db, checkpointsBucket, persistence.Key(cp.OwnerID, cp.AssistantID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "shared") || strings.Contains(string(raw), "old") {
		t.Fatal("inline context persisted")
	}
	cp.Sequence++
	cp.Context = json.RawMessage(`{"Inputs":[{"Role":"system","Text":"shared"},{"Role":"user","Text":"new"}]}`)
	if err = f.Checkpoints.Save(t.Context(), cp, 1); err != nil {
		t.Fatal(err)
	}
	for {
		n, err := contextstore.Collect(t.Context(), db, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	// Recreate the repository: no in-memory state is necessary for recovery.
	restored, err := NewCheckpoints(db).Load(t.Context(), cp.OwnerID, cp.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	if contextHash(json.RawMessage(restored.Context)) != contextHash(json.RawMessage(cp.Context)) {
		t.Fatal("recovery changed context")
	}
	nodes, err := persistence.Scan[json.RawMessage](t.Context(), db, "context.objects")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if strings.Contains(string(n), `"Text":"old"`) {
			t.Fatal("old branch stayed pinned")
		}
	}
}

func TestCheckpointReferenceRollsBackWithExecutionTransaction(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, LastEventID: f.Turn.Event.ID, FormatVersion: 1, Sequence: 1, SavedAt: time.Now(), Context: json.RawMessage(`{"Inputs":[{"Role":"user","Text":"durable"}]}`)}
	if err := f.Checkpoints.Save(t.Context(), cp, 0); err != nil {
		t.Fatal(err)
	}
	cp.Sequence++
	cp.Context = json.RawMessage(`{"Inputs":[{"Role":"user","Text":"must rollback"}]}`)
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := f.Checkpoints.Save(ctx, cp, 1); err != nil {
			return err
		}
		return core.NewError(core.Unavailable, "simulated execution commit failure")
	})
	if err == nil {
		t.Fatal("failure swallowed")
	}
	for {
		n, e := contextstore.Collect(t.Context(), db, 100)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
	}
	restored, e := f.Checkpoints.Load(t.Context(), cp.OwnerID, cp.AssistantID)
	if e != nil || restored.Sequence != 1 || !strings.Contains(string(restored.Context), "durable") {
		t.Fatal(restored, e)
	}
}

type unavailableContextStore struct{ persistence.Store }
type unavailableContextTx struct{ persistence.Tx }

func (s unavailableContextStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, unavailableContextTx{tx}) })
}
func (tx unavailableContextTx) Put(ctx context.Context, bucket, key string, value any) error {
	if bucket == "context.objects" {
		return core.NewError(core.Unavailable, "simulated context disk failure")
	}
	return tx.Tx.Put(ctx, bucket, key, value)
}
func TestAgentDoesNotDispatchWhenContextCannotPersist(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	blocked := unavailableContextStore{db}
	e.store = blocked
	e.checkpoints = NewCheckpoints(blocked)
	if _, err := e.Handle(t.Context(), f.Turn); err == nil {
		t.Fatal("context persistence failure ignored")
	}
	if len(f.ModelRequests()) != 0 {
		t.Fatal("model dispatched before durable context")
	}
	if _, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant"); !persistence.Missing(err) {
		t.Fatal("failed checkpoint became visible", err)
	}
}
