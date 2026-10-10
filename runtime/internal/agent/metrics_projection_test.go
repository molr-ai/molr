package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type metricPayloadStore struct {
	persistence.Store
	bytes int
}
type metricPayloadTx struct {
	persistence.Tx
	parent *metricPayloadStore
}

func (s *metricPayloadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return fn(ctx, &metricPayloadTx{Tx: tx, parent: s})
	})
}
func (t *metricPayloadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "agent.operations" {
		for _, row := range rows {
			t.parent.bytes += len(row.Data)
		}
	}
	return rows, err
}
func metricsProjectionFixture(tb testing.TB) (*Engine, core.Principal, *metricPayloadStore) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "metrics.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	result, _ := json.Marshal(map[string]any{"output": strings.Repeat("x", 64<<10)})
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "im.conversations", "owner/chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "im.messages", "owner/message", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.Assistant, Source: conversation.Source{ExecutionID: "turn"}}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "agent.jobs", "owner/event", scheduler.Job{Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant"}, ExecutionID: "turn", State: scheduler.Done, Attempts: 1}); err != nil {
			return err
		}
		for i := range 100 {
			id := core.ID(fmt.Sprintf("tool-%03d", i))
			op := operation.Operation{ID: id, OwnerID: "owner", AssistantID: "assistant", ExecutionID: "turn", Action: "shell", State: operation.Succeeded, CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC), Result: result}
			if err := tx.Put(ctx, "agent.operations", persistence.Key("owner", id), op); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &metricPayloadStore{Store: db}
	engine := NewRuntime(observed, Options{Now: func() time.Time { return time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC) }, Dependencies: Dependencies{Policy: fixtureAuthorizer{}}})
	return engine, owner, observed
}
func TestTurnMetricsDoesNotReadToolResultBodies(t *testing.T) {
	engine, owner, observed := metricsProjectionFixture(t)
	got, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolCalls != 100 || got.ModelCalls != 0 || !got.Finalized {
		t.Fatalf("wrong metrics: %+v", got)
	}
	if observed.bytes > 128<<10 {
		t.Fatalf("metrics summary read %d bytes including tool result bodies", observed.bytes)
	}
}
func BenchmarkTurnMetricsLargeToolResults(b *testing.B) {
	engine, owner, _ := metricsProjectionFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := engine.MessageMetrics(b.Context(), owner, "chat", "message")
		if err != nil || got.ToolCalls != 100 {
			b.Fatalf("calls=%d err=%v", got.ToolCalls, err)
		}
	}
}

func TestTurnMetricsProjectionExcludesUnrelatedOperationsAndKeepsPendingState(t *testing.T) {
	engine, owner, store := metricsProjectionFixture(t)
	err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, op := range []operation.Operation{
			{ID: "other-owner", OwnerID: "other", AssistantID: "assistant", ExecutionID: "turn", Action: "model.generate", State: operation.Prepared},
			{ID: "other-assistant", OwnerID: "owner", AssistantID: "other", ExecutionID: "turn", Action: "model.generate", State: operation.Prepared},
			{ID: "other-turn", OwnerID: "owner", AssistantID: "assistant", ExecutionID: "other", Action: "model.generate", State: operation.Prepared},
		} {
			if err := tx.Put(ctx, "agent.operations", persistence.Key(op.OwnerID, op.ID), op); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolCalls != 100 || got.ModelCalls != 0 || !got.Finalized {
		t.Fatalf("unrelated calls affected metrics: %+v", got)
	}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var op operation.Operation
		if err := tx.Get(ctx, "agent.operations", "owner/tool-000", &op); err != nil {
			return err
		}
		op.State = operation.Unknown
		return tx.Put(ctx, "agent.operations", "owner/tool-000", op)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.MessageMetrics(t.Context(), owner, "chat", "message")
	if err != nil {
		t.Fatal(err)
	}
	if got.Finalized || got.ToolCalls != 100 {
		t.Fatal("pending outcome lost", got.Finalized, got.ToolCalls)
	}
}

func TestTurnMetricsHistoricalSourceReadsOnlyIdentity(t *testing.T) {
	engine, owner, store := metricsProjectionFixture(t)
	err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "im.messages", "owner/message", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.Assistant, Source: conversation.Source{OperationID: "tool-000"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
	if err != nil {
		t.Fatal(err)
	}
	if got.TurnID != "turn" || got.ToolCalls != 100 || !got.Finalized || store.bytes > 128<<10 {
		t.Fatalf("historical metrics: turn=%s tools=%d finalized=%v bytes=%d", got.TurnID, got.ToolCalls, got.Finalized, store.bytes)
	}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var op operation.Operation
		if err := tx.Get(ctx, "agent.operations", "owner/tool-000", &op); err != nil {
			return err
		}
		op.AssistantID = "other"
		return tx.Put(ctx, "agent.operations", "owner/tool-000", op)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.MessageMetrics(t.Context(), owner, "chat", "message")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "unavailable" || got.TurnID != "" {
		t.Fatal("historical source crossed assistant boundary")
	}
}
