package agent

import (
	"context"
	"path/filepath"
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

func TestTurnMetricsDynamicAggregationFinalizationAndIsolation(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "metrics.db"))
	testkit.RequireNoError(t, err)
	defer store.Close()
	start := time.Unix(100, 0).UTC()
	now := start.Add(5 * time.Second)
	engine := NewRuntime(store, Options{Now: func() time.Time { return now }, Dependencies: Dependencies{Policy: fixtureAuthorizer{}}})
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	job := scheduler.Job{Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant"}, ExecutionID: "turn", State: scheduler.Running, Attempts: 2}
	put := func(bucket, key string, value any) {
		t.Helper()
		testkit.RequireNoError(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }))
	}
	put("im.conversations", "owner/chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"})
	put("im.conversations", "owner/other-chat", conversation.Conversation{ID: "other-chat", OwnerID: "owner", PeerID: "other-assistant"})
	put("im.messages", "owner/message", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.Assistant, Source: conversation.Source{OperationID: "publish"}})
	put("agent.jobs", "owner/event", job)
	put("agent.turn_timing", "owner/turn", turnTiming{StartedAt: start})
	op := operation.Operation{ID: "publish", OwnerID: "owner", AssistantID: "assistant", ExecutionID: "turn", Action: "conversation_publish", State: operation.Succeeded}
	put("agent.operations", "owner/publish", op)
	for _, id := range []core.ID{"model1", "model2"} {
		op.ID = id
		op.Action = "model.generate"
		op.State = operation.Succeeded
		put("agent.operations", "owner/"+string(id), op)
	}
	first, gen := 100.0, 1000.0
	receipt := model.Receipt{ID: "r1", OperationID: "model1", OutcomeKnown: true, CreatedAt: start, ModelName: "Model", Usage: model.Usage{InputTokens: 100, CachedTokens: 60, OutputTokens: 20}, Charged: &core.Money{Currency: "USD", Micros: 200}, Timing: &model.Timing{FirstTokenMs: &first, GenerationMs: &gen}}
	put("agent.model_receipts", "owner/r1", receipt)
	query := func() TurnMetrics {
		t.Helper()
		out, err := engine.MessageMetrics(t.Context(), owner, "chat", "message")
		testkit.RequireNoError(t, err)
		return out
	}
	out := query()
	if out.Finalized || out.UsageComplete || out.CompletedCalls != 1 || out.ModelCalls != 2 || out.ToolCalls != 1 || out.Retries != 1 || out.Usage.InputTokens != 100 || out.Usage.CachedTokens != 60 || *out.ElapsedMs != 5000 {
		t.Fatalf("partial=%+v", out)
	}
	job.State = scheduler.Canceled
	put("agent.jobs", "owner/event", job)
	put("agent.turn_timing", "owner/turn", turnTiming{StartedAt: start, EndedAt: &now})
	if query().Finalized {
		t.Fatal("cancellation finalized unresolved billing")
	}
	receipt.ID = "r2"
	receipt.OperationID = "model2"
	receipt.Usage.OutputTokens = 40
	first2, gen2 := 300.0, 3000.0
	receipt.Timing = &model.Timing{FirstTokenMs: &first2, GenerationMs: &gen2}
	put("agent.model_receipts", "owner/r2", receipt)
	out = query()
	if !out.Finalized || out.Usage.InputTokens != 200 || out.Usage.CachedTokens != 120 || out.Usage.OutputTokens != 60 || out.Charges[0].Micros != 400 || *out.FirstTokenMs != 200 || *out.TokensPerSecond != 15 {
		t.Fatalf("final=%+v", out)
	}
	// No backend aggregate cache: a subsequent query reads fresh evidence.
	receipt.Charged = &core.Money{Currency: "CNY", Micros: 500}
	put("agent.model_receipts", "owner/r2", receipt)
	if len(query().Charges) != 2 {
		t.Fatal("backend returned cached aggregation")
	}
	_, err = engine.MessageMetrics(t.Context(), owner, "other-chat", "message")
	testkit.RequireCode(t, err, core.NotFound)
	other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
	_, err = engine.MessageMetrics(t.Context(), other, "chat", "message")
	testkit.RequireCode(t, err, core.NotFound)
	// Historical records can expose known usage without fabricating timings.
	testkit.RequireNoError(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.turn_timing", "owner/turn")
	}))
	if query().ElapsedMs != nil {
		t.Fatal("fabricated historical duration")
	}
}
