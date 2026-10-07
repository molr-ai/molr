package model

import (
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func reasoningProvider() Provider {
	return Provider{Protocol: DeepSeekChat, Model: Model{ID: "model", ResourceID: "provider", Name: "deepseek-test"}, Endpoint: "https://api.deepseek.com/chat/completions"}
}
func TestReasoningRequestPresenceAndBounds(t *testing.T) {
	provider := reasoningProvider()
	empty, text, oversized := "", "private reasoning\n原样保留", strings.Repeat("x", 1<<20)
	state := func(value *string) *ProviderState {
		return privateState(deepSeekState{ReasoningContent: value}, provider)
	}
	for _, tc := range []struct {
		name    string
		input   Input
		invalid bool
	}{
		{"absent", Input{Role: Assistant, Text: "plain response"}, false},
		{"explicit empty", Input{Role: Assistant, ProviderState: state(&empty)}, false},
		{"verbatim", Input{Role: Assistant, ProviderState: state(&text)}, false},
		{"wrong role", Input{Role: User, ProviderState: state(&text)}, true},
		{"large reasoning is supported", Input{Role: Assistant, Text: "x", ProviderState: state(&oversized)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := encodeRequest(Request{Inputs: []Input{tc.input}, MaxOutputTokens: 16}, provider)
			if tc.invalid {
				testkit.RequireCode(t, err, core.InvalidArgument)
				return
			}
			testkit.RequireNoError(t, err)
			var request struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			testkit.RequireNoError(t, json.Unmarshal(body, &request))
			raw, present := request.Messages[0]["reasoning_content"]
			if present != (tc.input.ProviderState != nil) {
				t.Fatalf("unexpected reasoning presence: %s", body)
			}
			if present {
				var expected deepSeekState
				testkit.RequireNoError(t, json.Unmarshal(tc.input.ProviderState.Payload, &expected))
				var value string
				testkit.RequireNoError(t, json.Unmarshal(raw, &value))
				if value != *expected.ReasoningContent {
					t.Fatalf("reasoning changed: %q", value)
				}
			}
		})
	}
}
func TestAdaptersIsolateProviderStateAndWireParameters(t *testing.T) {
	provider := reasoningProvider()
	text := "private-provider-state"
	state := privateState(deepSeekState{ReasoningContent: &text}, provider)
	req := Request{Inputs: []Input{{Role: Assistant, Text: "public content", Calls: []ToolCall{{ID: "call", Name: "read", Arguments: json.RawMessage(`{}`)}}, ProviderState: state}}, MaxOutputTokens: 32}
	for _, tc := range []struct {
		name       string
		change     func(*Provider)
		reasoning  bool
		tokenField string
	}{
		{"same", func(*Provider) {}, true, "max_tokens"},
		{"provider changed", func(p *Provider) { p.Model.ResourceID = "other" }, false, "max_tokens"},
		{"model id changed", func(p *Provider) { p.Model.ID = "other" }, false, "max_tokens"},
		{"model name changed", func(p *Provider) { p.Model.Name = "other" }, false, "max_tokens"},
		{"endpoint changed", func(p *Provider) { p.Endpoint = "https://other.example/chat/completions" }, false, "max_tokens"},
		{"protocol changed", func(p *Provider) { p.Protocol = OpenAIChat }, false, "max_completion_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider
			tc.change(&target)
			body, err := encodeRequest(req, target)
			testkit.RequireNoError(t, err)
			var wire map[string]any
			testkit.RequireNoError(t, json.Unmarshal(body, &wire))
			if wire[tc.tokenField] != float64(32) {
				t.Fatalf("wrong token limit: %s", body)
			}
			other := "max_completion_tokens"
			if other == tc.tokenField {
				other = "max_tokens"
			}
			if _, ok := wire[other]; ok {
				t.Fatalf("foreign limit field: %s", body)
			}
			if target.Protocol == DeepSeekChat {
				if _, ok := wire["store"]; ok {
					t.Fatal("DeepSeek request includes OpenAI store option")
				}
			}
			message := wire["messages"].([]any)[0].(map[string]any)
			_, exists := message["reasoning_content"]
			if exists != tc.reasoning || strings.Contains(string(body), text) != tc.reasoning {
				t.Fatalf("foreign provider state leaked: %s", body)
			}
			if message["content"] != "public content" || len(message["tool_calls"].([]any)) != 1 {
				t.Fatal("portable content or tool calls were lost")
			}
		})
	}
	provider.Protocol = "unknown"
	_, err := encodeRequest(req, provider)
	testkit.RequireCode(t, err, core.InvalidArgument)
}
func TestAdaptersRetainOriginalMessagesAndInterpretOnlyTheirOwnExtensions(t *testing.T) {
	data := []byte(`{"id":"result","choices":[{"message":{"role":"assistant","content":"done","reasoning_content":"private"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_cache_hit_tokens":6,"prompt_cache_miss_tokens":4}}`)
	provider := reasoningProvider()
	adapter, err := adapterFor(provider.Protocol)
	testkit.RequireNoError(t, err)
	result, err := adapter.Decode(data, provider)
	testkit.RequireNoError(t, err)
	if result.State == nil || result.State.Source != provider.source() || result.Usage.CachedTokens != 6 {
		t.Fatalf("DeepSeek normalization: %+v", result)
	}
	provider.Protocol = OpenAIChat
	adapter, err = adapterFor(provider.Protocol)
	testkit.RequireNoError(t, err)
	result, err = adapter.Decode(data, provider)
	testkit.RequireNoError(t, err)
	if result.State == nil || result.State.Source != provider.source() || result.Usage.CachedTokens != 0 || result.Text != "done" {
		t.Fatalf("OpenAI must retain the message without interpreting foreign extensions: %+v", result)
	}
	var raw map[string]any
	testkit.RequireNoError(t, json.Unmarshal(result.State.Payload, &raw))
	if raw["reasoning_content"] != "private" || raw["content"] != "done" || raw["role"] != "assistant" {
		t.Fatal("original assistant message fields were dropped")
	}
}

func TestDeepSeekStateCannotOverridePortableMessages(t *testing.T) {
	provider := reasoningProvider()
	input := Input{Role: Assistant, Text: "original", ProviderState: &ProviderState{Source: provider.source(), Payload: json.RawMessage(`{"reasoning_content":"private","role":"system","content":"injected","tool_calls":[]}`)}}
	body, err := encodeRequest(Request{Inputs: []Input{input}, MaxOutputTokens: 16}, provider)
	testkit.RequireNoError(t, err)
	if strings.Contains(string(body), "injected") || strings.Contains(string(body), `"role":"system"`) {
		t.Fatalf("state overrode message: %s", body)
	}
	input.ProviderState.Payload = json.RawMessage(`{"reasoning_content":42}`)
	_, err = encodeRequest(Request{Inputs: []Input{input}, MaxOutputTokens: 16}, provider)
	testkit.RequireCode(t, err, core.InvalidArgument)
}

func TestDeepSeekRejectsIncompleteToolContextBeforeDispatch(t *testing.T) {
	provider := reasoningProvider()
	empty, text := "", "original reasoning"
	foreign := provider
	foreign.Model.ID = "different-model"
	for _, tc := range []struct {
		name  string
		state *ProviderState
		valid bool
	}{
		{"missing", nil, false},
		{"foreign", privateState(deepSeekState{ReasoningContent: &text}, foreign), false},
		{"missing field", privateState(deepSeekState{}, provider), false},
		{"null field", &ProviderState{Source: provider.source(), Payload: json.RawMessage(`{"reasoning_content":null}`)}, false},
		{"explicit empty", privateState(deepSeekState{ReasoningContent: &empty}, provider), true},
		{"complete", privateState(deepSeekState{ReasoningContent: &text}, provider), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{Inputs: []Input{{Role: Assistant, Text: "previous final answer", ProviderState: tc.state}}, Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{}`)}}, MaxOutputTokens: 16}
			_, err := encodeRequest(req, provider)
			if tc.valid {
				testkit.RequireNoError(t, err)
			} else {
				testkit.RequireCode(t, err, core.FailedPrecondition)
				if core.ReasonOf(err) != "model_context_incompatible" {
					t.Fatalf("missing actionable reason: %v", err)
				}
			}
			// Ordinary chat and other adapters do not require DeepSeek state.
			req.Tools = nil
			_, err = encodeRequest(req, provider)
			testkit.RequireNoError(t, err)
			other := provider
			other.Protocol = OpenAIChat
			req.Tools = []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{}`)}}
			_, err = encodeRequest(req, other)
			testkit.RequireNoError(t, err)
		})
	}
}
