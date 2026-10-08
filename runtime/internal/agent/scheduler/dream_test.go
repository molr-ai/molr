package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func TestDreamPinsQueueBetweenBatchesUntilDeadline(t *testing.T) {
	ctx := t.Context()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	deadline := now.Add(time.Minute)
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.dream_sessions_index", "owner/session", map[string]any{"ID": "session", "OwnerID": "owner", "AssistantID": "assistant", "State": "active", "Started": true, "Deadline": deadline}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "business.dream_run_sessions", "owner/batch", map[string]any{"ID": "batch", "OwnerID": "owner", "SessionID": "session"}); err != nil {
			return err
		}
		for _, e := range []event.AssistantEvent{{ID: "message", OwnerID: "owner", AssistantID: "assistant", Kind: event.UserMessage, CreatedAt: now}, {ID: "dream", OwnerID: "owner", AssistantID: "assistant", Kind: event.DreamDue, OperationID: "batch", CreatedAt: now.Add(time.Second)}} {
			if err := event.SaveJob(ctx, tx, Job{Event: e, State: Queued, NotBefore: e.CreatedAt, Version: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db)
	if _, err = q.Claim(ctx, now); core.CodeOf(err) != core.NotFound {
		t.Fatal("message entered batch gap", err)
	}
	claim, err := q.Claim(ctx, now.Add(time.Second))
	if err != nil || claim.Job.Event.Kind != event.DreamDue {
		t.Fatal(claim, err)
	}
	if err = q.Ack(ctx, claim.Principal, claim.Job.Event.ID, claim.Job.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = q.Claim(ctx, now.Add(2*time.Second)); core.CodeOf(err) != core.NotFound {
		t.Fatal("message entered session", err)
	}
	claim, err = q.Claim(ctx, deadline)
	if err != nil || claim.Job.Event.Kind != event.UserMessage {
		t.Fatal("message not released", claim, err)
	}
}
