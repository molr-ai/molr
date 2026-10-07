package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func TestCompactionRetainsOriginalHistoryAndResumes(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	engine := f.Runtime.(*Engine)
	engine.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	history := []model.Input{{Role: model.System, Text: loopInstructions}}
	for i := 0; i < 18; i++ {
		history = append(history, model.Input{Role: model.User, Text: "Original evidence 原始消息 " + strings.Repeat("history ", 650)})
	}
	p := turnProgress{Inputs: history}
	before, _ := contextTokens(p, "model")
	if before < 32768*9/10 {
		t.Fatal("test did not reach threshold")
	}
	interrupted, err := engine.prepareContext(t.Context(), f.Turn, "model", &p)
	if err != nil || interrupted {
		t.Fatal(interrupted, err)
	}
	if len(f.ModelRequests()) != 1 {
		t.Fatal("compaction must use one model request", len(f.ModelRequests()))
	}
	if len(p.Inputs) >= len(history) || !strings.Contains(p.Inputs[1].Text, "Continuation summary") {
		t.Fatal("history was not compacted")
	}
	records, err := persistence.Scan[contextArchive](t.Context(), db, "agent.context_archives")
	if err != nil || len(records) != 1 || len(records[0].Inputs) != len(history) || records[0].Inputs[1].Text != history[1].Text {
		t.Fatal("original execution history lost", err)
	}
	messages, err := persistence.Scan[json.RawMessage](t.Context(), db, "im.messages")
	if err != nil || len(messages) != 1 {
		t.Fatal("conversation history changed", err)
	}
	checkpoint, err := f.Checkpoints.Load(t.Context(), f.Turn.Principal.OwnerID, f.Turn.Event.AssistantID)
	saved, decodeErr := decodeState(checkpoint)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if err != nil || contextHash(saved.Inputs) != contextHash(p.Inputs) {
		t.Fatal("compacted context not durable", err)
	}
}

func TestCompactionFailureKeepsInputs(t *testing.T) {
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("original history ", 12000)}}}
	before := contextHash(p.Inputs)
	_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	if err == nil || before != contextHash(p.Inputs) {
		t.Fatal("failure replaced original inputs", err)
	}
	archives, err := persistence.Scan[contextArchive](t.Context(), db, "agent.context_archives")
	if err != nil || len(archives) != 1 {
		t.Fatal("original archive missing", err)
	}
}

func TestContextTokensUseProviderAndNewContent(t *testing.T) {
	inputs := []model.Input{{Role: model.User, Text: "hello"}}
	p := turnProgress{Inputs: inputs, Usage: &contextUsage{ModelID: "m", Tokens: 250000, Inputs: usageInputs(inputs), Tools: contextHash([]model.ToolDefinition(nil))}}
	tokens, source := contextTokens(p, "m")
	if tokens != 250000 || source != "provider" {
		t.Fatal(tokens, source)
	}
	p.Inputs = append(p.Inputs, model.Input{Role: model.Tool, Text: "new output"})
	tokens, source = contextTokens(p, "m")
	if tokens <= 250000 || source != "provider_with_estimate" {
		t.Fatal(tokens, source)
	}
	tokens, source = contextTokens(p, "other")
	if tokens >= 250000 || source != "estimated" {
		t.Fatal("reused usage from another model", tokens, source)
	}
}
func TestOldMessageAndJSONCapsNoLongerBlock(t *testing.T) {
	inputs := make([]model.Input, 300)
	for i := range inputs {
		inputs[i] = model.Input{Role: model.User, Text: strings.Repeat("a", 8192)}
	}
	if err := checkContextSize(inputs); err != nil {
		t.Fatal(err)
	}
}

func TestToolArtifactFailureDoesNotRepeatPublishedMessage(t *testing.T) {
	calls := 0
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"id":"publish-once","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"one","type":"function","function":{"name":"conversation_publish","arguments":"{\"conversation_id\":\"chat\",\"text\":\"publish once\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10}}`))
		} else {
			_, _ = w.Write([]byte(`{"id":"done","choices":[{"message":{"role":"assistant","content":"finished"},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":10}}`))
		}
	}))
	e := f.Runtime.(*Engine)
	fail := true
	e.options.PrepareToolResult = func(_ context.Context, _ core.Execution, r tools.Result) (tools.Result, error) {
		if fail {
			return r, core.NewError(core.Unavailable, "artifact disk temporarily unavailable")
		}
		return r, nil
	}
	if _, err := e.Handle(t.Context(), f.Turn); err == nil {
		t.Fatal("expected artifact failure")
	}
	fail = false
	if _, err := e.Handle(t.Context(), f.Turn); err != nil {
		t.Fatal(err)
	}
	records, err := persistence.Scan[conversation.Message](t.Context(), db, "im.messages")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range records {
		if r.Content.Text == "publish once" {
			n++
		}
	}
	if n != 1 || calls != 2 {
		t.Fatal("operation repeated", n, calls)
	}
}

func TestCompactionDoesNotInjectSummaryReasoningIntoContinuation(t *testing.T) {
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"summary","choices":[{"message":{"role":"assistant","content":"goals and progress retained","reasoning_content":"summary reasoning"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}), model.DeepSeekChat)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("historical data ", 4000)}}}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs), Tools: contextHash(p.Definitions)}
	_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	if err != nil {
		t.Fatalf("prepare context: %v (reason: %s)", err, core.ReasonOf(err))
	}
	if p.Inputs[1].ProviderState != nil || p.Inputs[1].Role != model.User {
		t.Fatal("synthetic checkpoint contains assistant reasoning")
	}
	_, _, err = e.generateContextStep(t.Context(), f.Turn, "model", []model.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}, p, "after-compaction", 128)
	if err != nil {
		t.Fatal("provider rejected compressed continuation", err)
	}
}

func TestContextStatusSurvivesTurnsAndResetsExplicitly(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	key := persistence.Key(f.Turn.Principal.OwnerID, f.Turn.Event.AssistantID)
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		s, err := persistence.Load[Status](ctx, db, statusBucket, key)
		if err != nil {
			return err
		}
		s.Context = &ContextStatus{Compressions: 3}
		return tx.Put(ctx, statusBucket, key, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	e := f.Runtime.(*Engine)
	out, err := e.Handle(t.Context(), f.Turn)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status.Context == nil || out.Status.Context.Compressions != 3 {
		t.Fatal("new turn reset context counter", out.Status.Context)
	}
	if err = e.ResetContext(t.Context(), f.Owner, f.Turn.Event.AssistantID, core.Mutation{IdempotencyKey: "reset-compaction"}, out.Checkpoint.Sequence); err != nil {
		t.Fatal(err)
	}
	s, err := e.Status(t.Context(), f.Owner, f.Turn.Event.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Context != nil {
		t.Fatal("explicit context reset retained old usage")
	}
}

func TestSingleSummaryRejectsIncompleteOrIneffectiveOutput(t *testing.T) {
	for _, tc := range []struct {
		name, finish, text string
		calls              bool
		reason             core.Reason
	}{
		{"partial-at-cap", "length", "partial useful text", false, core.ContextSummaryTruncated},
		{"reasoning-only-at-cap", "length", "", false, core.ContextSummaryTruncated},
		{"empty", "stop", "", false, core.ContextSummaryInvalid},
		{"tool-call", "tool_calls", "", true, core.ContextSummaryInvalid},
		{"unknown-finish", "content_filter", "partial", false, core.ContextSummaryInvalid},
		{"no-shrink", "stop", strings.Repeat("unhelpful output ", 5000), false, core.ContextCompactionIneffective},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				msg := map[string]any{"role": "assistant", "content": tc.text, "reasoning_content": "private reasoning"}
				if tc.calls {
					msg["tool_calls"] = []any{stateTestCall("never-execute", "conversation_publish", `{"conversation_id":"chat","text":"MUST NOT PUBLISH"}`)}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				chunk := map[string]any{"id": "summary", "choices": []any{map[string]any{"index": 0, "delta": msg, "finish_reason": tc.finish}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8192}}
				raw, _ := json.Marshal(chunk)
				w.Write(append(append([]byte("data: "), raw...), []byte("\n\ndata: [DONE]\n\n")...))
			}))
			e := f.Runtime.(*Engine)
			e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
			p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("original data ", 3000)}}}
			p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs), Tools: contextHash(p.Definitions)}
			before := contextHash(p)
			for i := 0; i < 2; i++ {
				_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
				if core.ReasonOf(err) != tc.reason {
					t.Fatalf("reason=%s error=%v", core.ReasonOf(err), err)
				}
				if contextHash(p) != before {
					t.Fatal("failed summary altered active context")
				}
			}
			if calls != 1 {
				t.Fatalf("replayed failed summary made %d paid requests", calls)
			}
			if _, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID); !persistence.Missing(err) {
				t.Fatal("failed summary committed checkpoint")
			}
			rows, err := persistence.Scan[model.Receipt](t.Context(), db, "agent.model_receipts")
			if err != nil || len(rows) != 1 || !rows[0].OutcomeKnown || rows[0].FinishReason != tc.finish || rows[0].Charged == nil {
				t.Fatal("finish/billing evidence lost", rows, err)
			}
			if f.VisibleMessageCount() != 0 {
				t.Fatal("summarizer executed a tool")
			}
		})
	}
}

func TestOversizedHistoryFailsBeforeSummaryRequest(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("历史原文", 30000)}}}
	before := contextHash(p)
	_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	if core.ReasonOf(err) != core.ContextSummaryInputTooLarge || len(f.ModelRequests()) != 0 || contextHash(p) != before {
		t.Fatal("oversized history was sent, split or altered", err)
	}
	records, err := persistence.Scan[contextArchive](t.Context(), db, "agent.context_archives")
	if err != nil || len(records) != 1 || contextHash(records[0].Inputs) != contextHash(p.Inputs) {
		t.Fatal("archive missing", err)
	}
}

func TestCompactionBoundaryPreservesWholeToolGroups(t *testing.T) {
	history := []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "goal"}, {Role: model.Assistant, Calls: []model.ToolCall{{ID: "a"}, {ID: "b"}}}, {Role: model.Tool, ReplyTo: "a", Text: strings.Repeat("long ", 1000)}, {Role: model.Tool, ReplyTo: "b", Text: "short"}, {Role: model.User, Text: "latest"}}
	cut, err := compactionCut(history, 100)
	if err != nil || cut != 5 {
		t.Fatal("split tool group", cut, err)
	}
	history = history[:4]
	if _, err = compactionCut(history, 0); err == nil {
		t.Fatal("pending tool group accepted")
	}
}

func TestInputEstimateDoesNotDoubleCountProviderMessage(t *testing.T) {
	in := model.Input{Role: model.Assistant, Text: "visible answer", Calls: []model.ToolCall{{ID: "one", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)}}}
	baseline := inputEstimate(in)
	in.ProviderState = &model.ProviderState{Payload: json.RawMessage(`{"role":"assistant","content":"visible answer","tool_calls":[{"id":"one","function":{"name":"read","arguments":"{\"path\":\"x\"}"}}],"reasoning_content":"retained reasoning"}`)}
	if got := inputEstimate(in); got != baseline+textTokens("retained reasoning") {
		t.Fatal("duplicate state estimate", got, baseline)
	}
}

type compactionCommitFaultBudget struct {
	policy.Budget
	fail func()
}

func (b compactionCommitFaultBudget) Settle(ctx context.Context, e core.Execution, r policy.SettleRequest) (policy.Reservation, error) {
	out, err := b.Budget.Settle(ctx, e, r)
	if err == nil {
		b.fail()
	}
	return out, err
}
func TestCompactionCheckpointAndCounterRollbackTogether(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	e.options.Dependencies.Budget = compactionCommitFaultBudget{Budget: e.options.Dependencies.Budget, fail: f.FailNextCommit}
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("original data ", 3000)}}}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs), Tools: contextHash(p.Definitions)}
	before := contextHash(p)
	if _, err := e.prepareContext(t.Context(), f.Turn, "model", &p); err == nil {
		t.Fatal("expected commit failure")
	}
	if contextHash(p) != before {
		t.Fatal("rolled-back candidate became active")
	}
	if _, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID); !persistence.Missing(err) {
		t.Fatal("checkpoint survived rollback")
	}
	status, err := e.Status(t.Context(), f.Owner, f.Turn.Event.AssistantID)
	if err != nil || status.Context.Compressions != 0 {
		t.Fatal("counter survived rollback", status, err)
	}
}
