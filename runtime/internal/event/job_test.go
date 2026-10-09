package event

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestJobCommitSignalAndStoppedOutboxFence(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	watch, err := db.Watch(DispatchSignal())
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	input, err := db.Watch(AssistantInputSignal("owner", "assistant"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	e := AssistantEvent{ID: "input", OwnerID: "owner", AssistantID: "assistant", Kind: UserMessage, MessageID: "message", CreatedAt: time.Now().UTC()}
	rollback := errors.New("rollback")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := NewWriter(tx.Store()).AppendAssistant(ctx, e); err != nil {
			return err
		}
		select {
		case <-watch.Changes():
			t.Fatal("woke before commit")
		default:
		}
		select {
		case <-input.Changes():
			t.Fatal("input woke before commit")
		default:
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
		t.Fatal("rollback woke scheduler")
	default:
	}
	select {
	case <-input.Changes():
		t.Fatal("rollback woke input watcher")
	default:
	}
	if _, err := persistence.Load[Job](t.Context(), db, "agent.jobs", "owner/input"); !persistence.Missing(err) {
		t.Fatal("rollback left job", err)
	}
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return SaveJob(ctx, tx, Job{Event: e, State: Canceled, NotBefore: e.CreatedAt, Version: 2})
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
	default:
		t.Fatal("committed state change did not wake scheduler")
	}
	select {
	case <-input.Changes():
	default:
		t.Fatal("committed job did not wake assistant input watcher")
	}
	if err := NewWriter(db).AppendAssistant(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	job, err := persistence.Load[Job](t.Context(), db, "agent.jobs", "owner/input")
	if err != nil || job.State != Canceled || job.Version != 2 || job.QueueKey == "" {
		t.Fatal("outbox resurrected fenced job", job, err)
	}
}
