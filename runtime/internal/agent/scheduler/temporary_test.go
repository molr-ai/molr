package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTemporaryFailureNotifiesEmployerOnce(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := Job{Event: event.AssistantEvent{ID: "input", OwnerID: "owner", AssistantID: "worker", Kind: event.AssistantMessage, ConversationID: "chat", MessageID: "message", CreatedAt: time.Now()}, State: Failed, FailureCode: core.BudgetExceeded, Version: 2}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", "owner/worker", struct{ EmployerID core.ID }{"employer"}); err != nil {
			return err
		}
		if err := saveJob(ctx, tx, job); err != nil {
			return err
		}
		return saveJob(ctx, tx, job)
	})
	if err != nil {
		t.Fatal(err)
	}
	notices, err := persistence.Select[event.AssistantEvent](t.Context(), store, "agent.events", persistence.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].Kind != event.AssistantNotice || notices[0].AssistantID != "employer" || !strings.Contains(string(notices[0].Details), "worker") || !strings.Contains(string(notices[0].Details), string(core.BudgetExceeded)) {
		t.Fatalf("wrong failure notice: %+v", notices)
	}
}
