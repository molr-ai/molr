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
	before, _ := contextTokens(p, "model", model.StateSource{})
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
	p := turnProgress{Inputs: inputs, Usage: &contextUsage{ModelID: "m", Tokens: 250000, Inputs: usageInputs(inputs, model.StateSource{}), Tools: contextHash([]model.ToolDefinition(nil))}}
	tokens, source := contextTokens(p, "m", model.StateSource{})
	if tokens != 250000 || source != "provider" {
		t.Fatal(tokens, source)
	}
	p.Inputs = append(p.Inputs, model.Input{Role: model.Tool, Text: "new output"})
	tokens, source = contextTokens(p, "m", model.StateSource{})
	if tokens <= 250000 || source != "provider_with_estimate" {
		t.Fatal(tokens, source)
	}
	tokens, source = contextTokens(p, "other", model.StateSource{})
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
	p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
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

func TestSingleSummaryRejectsIncompleteOutput(t *testing.T) {
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
			p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
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

func TestOversizedEstimateStillSendsCompleteSummaryRequest(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("历史原文", 30000)}, {Role: model.User, RuntimeContext: true, Text: "current automatic context"}}}
	original := append([]model.Input{}, p.Inputs...)
	_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.ModelRequests()
	if len(reqs) != 1 || len(reqs[0].Inputs) != len(original)+1 || contextHash(reqs[0].Inputs[:len(original)]) != contextHash(modelInputs(original)) {
		t.Fatal("summary did not preserve the complete request prefix")
	}
	last := reqs[0].Inputs[len(original)]
	if last.Role != model.User || last.Text != compactionInstruction {
		t.Fatal("missing appended user instruction")
	}
	records, err := persistence.Scan[contextArchive](t.Context(), db, "agent.context_archives")
	if err != nil || len(records) != 1 || contextHash(records[0].Inputs) != contextHash(original) {
		t.Fatal("archive missing", err)
	}
}

func TestProviderContextRejectionRetainsHistory(t *testing.T) {
	calls := 0
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"context_length_exceeded","message":"private prompt details"}}`))
	}))
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("history ", 30000)}}}
	before := contextHash(p)
	_, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	failure := model.FailureOf(err)
	if calls != 1 || core.ReasonOf(err) != core.ModelContextExceeded || failure == nil || failure.ProviderCode != "context_length_exceeded" || !failure.NoCharge || failure.Retryable || contextHash(p) != before {
		t.Fatalf("provider rejection lost: calls=%d err=%v failure=%+v", calls, err, failure)
	}
	archives, err := persistence.Scan[contextArchive](t.Context(), db, "agent.context_archives")
	if err != nil || len(archives) != 1 {
		t.Fatal("archive lost", err)
	}
}

func TestInputEstimateMatchesReplayedReasoning(t *testing.T) {
	source := model.StateSource{Protocol: model.DeepSeekChat, ModelID: "model", Endpoint: "https://model.example"}
	in := model.Input{Role: model.Assistant, Text: "visible answer", Calls: []model.ToolCall{{ID: "one", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)}}}
	baseline := inputEstimate(in, source)
	in.ProviderState = &model.ProviderState{Source: source, Payload: json.RawMessage(`{"role":"assistant","content":"visible answer","reasoning":"not replayed","reasoning_content":"retained reasoning","other":"not replayed"}`)}
	if got := inputEstimate(in, source); got != baseline+textTokens("retained reasoning") {
		t.Fatal("replayed state estimate", got, baseline)
	}
	for _, target := range []model.StateSource{{Protocol: model.OpenAIChat, ModelID: "model"}, {Protocol: model.DeepSeekChat, ModelID: "different"}, {Protocol: model.DeepSeekChat, ModelID: "model", Endpoint: "https://other.example"}} {
		if got := inputEstimate(in, target); got != baseline {
			t.Fatal("counted state that is not sent", target, got)
		}
	}
}

func TestSuccessfulSummaryIsNotRejectedByEstimates(t *testing.T) {
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "summary", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": strings.Repeat("summary ", 20000)}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 500}})
	}))
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "short original"}}}
	if _, err := e.compactContext(t.Context(), f.Turn, "model", &p, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.prepareContext(t.Context(), f.Turn, "model", &p); err != nil {
		t.Fatal(err)
	}
	if len(f.ModelRequests()) != 1 {
		t.Fatal("estimated summary size caused immediate recompaction")
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
	p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
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

func TestDefaultCompressionThresholdUsesMeasuredBaseline(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 10000, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("conservatively estimated ", 10000)}}}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 7999, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
	if _, err := e.prepareContext(t.Context(), f.Turn, "model", &p); err != nil {
		t.Fatal(err)
	}
	if len(f.ModelRequests()) != 0 {
		t.Fatal("full estimate replaced the measured baseline")
	}
	p.Usage.Tokens = 8000
	if _, err := e.prepareContext(t.Context(), f.Turn, "model", &p); err != nil {
		t.Fatal(err)
	}
	if e.options.CompressionThreshold != 0.8 || len(f.ModelRequests()) != 1 {
		t.Fatal("default threshold is not 80 percent")
	}
}

func TestMeasuredInputBaselineExcludesUnsentOutputReasoning(t *testing.T) {
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"response","choices":[{"message":{"role":"assistant","content":"answer","reasoning":"private long reasoning"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10000}}`))
	}))
	e := f.Runtime.(*Engine)
	p := turnProgress{Inputs: []model.Input{{Role: model.User, Text: "question"}}}
	response, _, err := e.generateStep(t.Context(), f.Turn, "model", nil, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Usage = e.measureContext(t.Context(), f.Turn, "model", p)
	p.Inputs = append(p.Inputs, response)
	got, source := contextTokens(p, "model", model.StateSource{})
	if got != 100+inputEstimate(model.Input{Role: model.Assistant, Text: "answer"}, model.StateSource{}) || source != "provider_with_estimate" {
		t.Fatal("included billed but unsent reasoning", got, source)
	}
	before := got
	p.Inputs[1].ProviderState.Payload = json.RawMessage(`{"reasoning":"changed private data"}`)
	got, _ = contextTokens(p, "model", model.StateSource{})
	if got != before {
		t.Fatal("private state changed the estimate", got, before)
	}
}

func TestCompactionPreservesWirePrefixAndTools(t *testing.T) {
	var wire []struct {
		Messages []json.RawMessage `json:"messages"`
		Tools    json.RawMessage   `json:"tools"`
	}
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    json.RawMessage   `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		wire = append(wire, req)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"summary","choices":[{"message":{"role":"assistant","content":"complete checkpoint"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`))
	}))
	e := f.Runtime.(*Engine)
	p := turnProgress{Inputs: []model.Input{
		{Role: model.System, Text: loopInstructions},
		{Role: model.User, Text: "original goal"},
		{Role: model.Assistant, Text: "checking", Calls: []model.ToolCall{{ID: "read-one", Name: "file_read", Arguments: json.RawMessage(`{"path":"/work/sample"}`)}}, ProviderState: &model.ProviderState{Payload: json.RawMessage(`{"reasoning":"private state"}`)}},
		{Role: model.Tool, ReplyTo: "read-one", Text: `{"Path":"/work/sample","Text":"evidence"}`},
		{Role: model.User, Text: "latest correction"},
		{Role: model.User, RuntimeContext: true, Text: "unchanged automatic context"},
	}, Definitions: []model.ToolDefinition{{Name: "file_read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	if _, _, err := e.generateStep(t.Context(), f.Turn, "model", p.Definitions, p); err != nil {
		t.Fatal(err)
	}
	if _, err := e.compactContext(t.Context(), f.Turn, "model", &p, true); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 2 || len(wire[1].Messages) != len(wire[0].Messages)+1 || contextHash(wire[1].Messages[:len(wire[0].Messages)]) != contextHash(wire[0].Messages) || string(wire[1].Tools) != string(wire[0].Tools) {
		t.Fatal("wire prefix or tool definitions changed")
	}
	var last struct{ Role, Content string }
	json.Unmarshal(wire[1].Messages[len(wire[0].Messages)], &last)
	if last.Role != "user" || last.Content != compactionInstruction {
		t.Fatal("compaction instruction was not appended")
	}
}
