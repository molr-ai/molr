package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

// Each unrelated turn has a job, an operation and an indexed model receipt.
func seedMetricsHistory(tb testing.TB, ctx context.Context, db persistence.Store, n int) {
	tb.Helper()
	err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < n; i++ {
			id := core.ID(fmt.Sprintf("history-%06d", i))
			job := scheduler.Job{Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant"}, ExecutionID: id, State: scheduler.Done}
			op := operation.Operation{ID: id, OwnerID: "owner", AssistantID: "assistant", ExecutionID: id, Action: "model.generate", State: operation.Succeeded}
			if err := tx.Put(ctx, "agent.jobs", persistence.Key("owner", id), job); err != nil {
				return err
			}
			if err := tx.Put(ctx, "agent.operations", persistence.Key("owner", id), op); err != nil {
				return err
			}
			if err := model.NewReceipts(db).Record(ctx, "owner", model.Receipt{ID: id, OperationID: id, CreatedAt: time.Unix(100, 0), OutcomeKnown: true}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
}

func seedMetricsTarget(tb testing.TB, ctx context.Context, db persistence.Store) core.Principal {
	tb.Helper()
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, row := range []struct {
			bucket, key string
			value       any
		}{
			{"im.conversations", "owner/chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"}},
			{"im.messages", "owner/message", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.Assistant, Source: conversation.Source{ExecutionID: "turn"}}},
			{"agent.jobs", "owner/event", scheduler.Job{Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant"}, ExecutionID: "turn", State: scheduler.Done, Attempts: 1}},
		} {
			if err := tx.Put(ctx, row.bucket, row.key, row.value); err != nil {
				return err
			}
		}
		for _, id := range []core.ID{"model1", "model2", "publish"} {
			action := "model.generate"
			if id == "publish" {
				action = "conversation_publish"
			}
			op := operation.Operation{ID: id, OwnerID: "owner", AssistantID: "assistant", ExecutionID: "turn", Action: action, State: operation.Succeeded}
			if err := tx.Put(ctx, "agent.operations", persistence.Key("owner", id), op); err != nil {
				return err
			}
			if id != "publish" {
				if err := model.NewReceipts(db).Record(ctx, "owner", model.Receipt{ID: id, OperationID: id, CreatedAt: time.Unix(100, 0), OutcomeKnown: true, Usage: model.Usage{InputTokens: 100, OutputTokens: 20}, Charged: &core.Money{Currency: "USD", Micros: 200}}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return owner
}

func TestTurnMetricsBoundsHistoryAndExecutionScope(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "metrics.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	owner := seedMetricsTarget(t, t.Context(), db)
	observed := &testkit.ObservedStore{Store: db}
	engine := NewRuntime(observed, Options{Now: func() time.Time { return time.Unix(200, 0) }, Dependencies: Dependencies{Policy: fixtureAuthorizer{}}})
	baseline, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
	testkit.RequireNoError(t, err)
	if !baseline.Finalized || baseline.ModelCalls != 2 || baseline.ToolCalls != 1 || baseline.Charges[0].Micros != 400 {
		t.Fatalf("unexpected metrics: %+v", baseline)
	}
	seedMetricsHistory(t, t.Context(), db, 1000)
	// Sharing an execution ID alone must not admit another owner or assistant.
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, scope := range []struct{ owner, assistant core.ID }{{"other", "assistant"}, {"owner", "other"}} {
			id := core.ID("foreign-" + string(scope.owner))
			job := scheduler.Job{Event: event.AssistantEvent{ID: id, OwnerID: scope.owner, AssistantID: scope.assistant}, ExecutionID: "turn", State: scheduler.Running}
			op := operation.Operation{ID: id, OwnerID: scope.owner, AssistantID: scope.assistant, ExecutionID: "turn", Action: "model.generate", State: operation.Running}
			if err := tx.Put(ctx, "agent.jobs", persistence.Key(scope.owner, id), job); err != nil {
				return err
			}
			if err := tx.Put(ctx, "agent.operations", persistence.Key(scope.owner, id), op); err != nil {
				return err
			}
		}
		// Same operation ID in a different account must not affect billing.
		return model.NewReceipts(db).Record(ctx, "other", model.Receipt{ID: "foreign", OperationID: "model1", CreatedAt: time.Unix(100, 0), OutcomeKnown: true, Charged: &core.Money{Currency: "USD", Micros: 999}})
	}))
	observed.Scans, observed.Rows = 0, nil
	got, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
	testkit.RequireNoError(t, err)
	if !reflect.DeepEqual(got, baseline) {
		t.Fatalf("unrelated history changed metrics: %+v", got)
	}
	if observed.Scans != 0 || observed.Rows["agent.jobs"] != 1 || observed.Rows["agent.operations"] != 3 {
		t.Fatalf("unrelated history loaded: %+v", observed)
	}
	// An absent execution must return unavailable without reading operation history.
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Delete(ctx, "agent.jobs", "owner/event") }))
	observed.Rows = nil
	got, err = engine.MessageMetrics(t.Context(), owner, "chat", "message")
	testkit.RequireNoError(t, err)
	if got.State != "unavailable" || observed.Rows["agent.operations"] != 0 || observed.Scans != 0 {
		t.Fatalf("missing turn: %+v, reads: %+v", got, observed)
	}
}

func BenchmarkTurnMetricsHistory(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "metrics.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			owner := seedMetricsTarget(b, b.Context(), db)
			seedMetricsHistory(b, b.Context(), db, n)
			engine := NewRuntime(db, Options{Dependencies: Dependencies{Policy: fixtureAuthorizer{}}})
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				got, err := engine.MessageMetrics(b.Context(), owner, "chat", "message")
				if err != nil || !got.Finalized || got.ModelCalls != 2 || got.ToolCalls != 1 {
					b.Fatalf("metrics=%+v err=%v", got, err)
				}
			}
		})
	}
}
