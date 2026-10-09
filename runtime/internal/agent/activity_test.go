package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestActivitiesBoundedProjectionAndDreamDeadline(t *testing.T) {
	f, store := makeLoopFixture(t, nil)
	engine := f.Runtime.(*Engine)
	now := time.Now()
	engine.options.Now = func() time.Time { return now }
	checks := 0
	engine.options.CheckEnvironment = func(context.Context) error { checks++; return nil }
	past, future := now.Add(-time.Second), now.Add(time.Second)
	states := []Status{{AssistantID: "active", State: Dreaming, DreamDeadline: &future}, {AssistantID: "expired", State: Dreaming, DreamDeadline: &past}, {AssistantID: "idle", State: Idle}}
	ids := []core.ID{}
	err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, status := range states {
			ids = append(ids, status.AssistantID)
			if err := tx.Put(ctx, statusBucket, persistence.Key(f.Owner.OwnerID, status.AssistantID), status); err != nil {
				return err
			}
		}
		return tx.Put(ctx, statusBucket, persistence.Key(f.Other.OwnerID, "foreign"), Status{AssistantID: "foreign", State: Dreaming})
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Activities(t.Context(), f.Owner, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || checks != 1 {
		t.Fatalf("items=%v environment checks=%d", result, checks)
	}
	for _, item := range result {
		if item.Dreaming != (item.AssistantID == "active") {
			t.Fatal(item)
		}
	}
	engine.options.CheckEnvironment = func(context.Context) error { return errors.New("unavailable") }
	result, err = engine.Activities(t.Context(), f.Owner, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result {
		if item.Dreaming {
			t.Fatal("unavailable environment reported as dreaming")
		}
	}
	if _, err = engine.Activities(t.Context(), f.Owner, make([]core.ID, 201)); core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("unbounded batch: %v", err)
	}
}
