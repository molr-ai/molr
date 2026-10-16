package model

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"net/http"
	"strings"
	"testing"
	"time"
)

func anthropicToolStream() string {
	events := []string{
		`{"type":"message_start","message":{"id":"response","type":"message","role":"assistant","content":[],"usage":{"input_tokens":20,"cache_read_input_tokens":30,"output_tokens":0}}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Consider tools"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"state"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"检查"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call-a","name":"read","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"call-b","name":"read","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"b\"}"}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`,
		`{"type":"message_stop"}`,
	}
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}
func nativeProvider() Provider {
	return Provider{Protocol: AnthropicMessages, Endpoint: "https://anthropic.example/v1/messages", Model: Model{ID: "m", ResourceID: "r", Name: "claude-test", SupportsImages: true, SupportsTools: true}}
}
func TestAnthropicStreamingToolContinuationAndMedia(t *testing.T) {
	p := nativeProvider()
	_, normalized, timing, err := readProviderStream(p.Protocol, strings.NewReader(anthropicToolStream()), time.Now(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (anthropicAdapter{}).Decode(normalized, p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "检查" || len(result.Calls) != 2 || result.Usage.InputTokens != 50 || result.Usage.CachedTokens != 30 || result.Usage.OutputTokens != 12 || timing.FirstTokenMs == nil {
		t.Fatalf("bad result: %+v", result)
	}
	req := Request{MaxOutputTokens: 512, Inputs: []Input{{Role: System, Text: "policy"}, {Role: User, Text: "read files"}, {Role: Assistant, Text: result.Text, Calls: result.Calls, ProviderState: result.State}, {Role: Tool, ReplyTo: "call-a", Text: "a", Media: []media.Part{{Kind: "image", MIME: "image/png", Data: []byte("fake image"), Path: "a"}}}, {Role: Tool, ReplyTo: "call-b", Text: "b"}, {Role: System, Text: "runtime notification"}}, Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	encoded, err := encodeRequest(req, p)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		System    string
		Messages  []anthropicMessage
		MaxTokens int `json:"max_tokens"`
		Stream    bool
		Tools     []map[string]any
	}
	if err = json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body.System != "policy" || body.MaxTokens != 512 || !body.Stream || len(body.Tools) != 1 || len(body.Messages) != 3 {
		t.Fatalf("invalid request: %s", encoded)
	}
	if body.Messages[1].Content[0].Signature != "signed-state" || body.Messages[1].Content[2].ID != "call-a" {
		t.Fatalf("lost state: %s", encoded)
	}
	blocks := body.Messages[2].Content
	if blocks[0].Type != "tool_result" || blocks[1].Type != "tool_result" || blocks[3].Type != "image" || blocks[3].Source.MediaType != "image/png" || blocks[len(blocks)-1].Text != "runtime notification" {
		t.Fatalf("tool batch/media order: %s", encoded)
	}
	if reasoning, err := ReplayedReasoning(req.Inputs[2], p.source()); err != nil || reasoning == nil || !strings.Contains(*reasoning, "signed-state") {
		t.Fatal("missing context estimate", err)
	}
	for _, change := range []func(*Provider){func(p *Provider) { p.Model.ID = "other" }, func(p *Provider) { p.Model.ResourceID = "other" }, func(p *Provider) { p.Model.Name = "other" }, func(p *Provider) { p.Endpoint = "https://other.example/v1/messages" }, func(p *Provider) { p.Protocol = OpenAIChat }} {
		other := p
		change(&other)
		data, err := encodeRequest(req, other)
		if err != nil || strings.Contains(string(data), "signed-state") {
			t.Fatalf("foreign state replayed: %s %v", data, err)
		}
	}
	req.Inputs[2].Text = "changed"
	if _, err := encodeRequest(req, p); core.ReasonOf(err) != core.ModelContextIncompatible {
		t.Fatal("accepted altered context", err)
	}
}
func TestAnthropicInvalidStreamAndUsage(t *testing.T) {
	good := anthropicToolStream()
	for _, bad := range []string{strings.Replace(good, `data: {"type":"message_stop"}`, "", 1), strings.Replace(good, `"index":2`, `"index":8`, 1), strings.Replace(good, `"output_tokens":12`, `"wrong":12`, 1), strings.Replace(good, `"type":"ping"`, `"type":"error"`, 1), strings.Replace(good, `"input_tokens":20`, `"input_tokens":null`, 1)} {
		if _, _, _, err := readAnthropicStream(strings.NewReader(bad), time.Now(), time.Now); err == nil {
			t.Fatal("accepted invalid stream")
		}
	}
	_, data, _, err := readAnthropicStream(strings.NewReader(good), time.Now(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(data), `"input_tokens":20`, `"input_tokens":-1`, 1), strings.Replace(string(data), `"cache_creation_input_tokens":0`, `"cache_creation_input_tokens":10`, 1), strings.Replace(string(data), `"stop_reason":"tool_use"`, `"stop_reason":""`, 1)} {
		if _, err := (anthropicAdapter{}).Decode([]byte(bad), nativeProvider()); err == nil {
			t.Fatal("accepted invalid usage/stop", bad)
		}
	}
}
func TestNewProviderTransportAndReceipt(t *testing.T) {
	f := newContractFixture(t)
	c := f.Client.(*client)
	resolve := c.options.Resolve
	c.options.Resolve = func(ctx context.Context, p core.Principal, id core.ID) (Provider, error) {
		out, err := resolve(ctx, p, id)
		out.Protocol = AnthropicMessages
		return out, err
	}
	c.options.HTTPClient = &http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing protocol header")
		}
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), "max_completion_tokens") || !strings.Contains(string(data), `"max_tokens"`) {
			t.Errorf("wrong protocol %s", data)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(anthropicToolStream()))}, nil
	})}
	f.Request.Inputs = append(f.Request.Inputs, Input{Role: User, Text: "Read files"})
	stream, err := c.Generate(t.Context(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	calls := 0
	for {
		d, err := stream.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if d.Kind == ToolCallDelta {
			calls++
		}
	}
	receipt, err := c.receipts.ByOperation(t.Context(), f.Owner.OwnerID, f.Request.Execution.OperationID)
	if err != nil || calls != 2 || !receipt.OutcomeKnown || receipt.Usage.CachedTokens != 30 || receipt.Charged == nil {
		t.Fatalf("receipt: %+v %v calls=%d", receipt, err, calls)
	}
	h := http.Header{}
	SetProviderHeaders(h, AnthropicMessages, core.NewSecret("fixture-key"))
	if h.Get("x-api-key") != "fixture-key" || h.Get("Authorization") != "" {
		t.Fatal("incorrect Anthropic authentication")
	}
}
func TestGeminiStreamingSignaturesStayOnTheirCalls(t *testing.T) {
	body := `data: {"id":"r","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","extra_content":{"google":{"thought_signature":"signed"},"untrusted":true},"function":{"name":"read","arguments":"{"}},{"index":1,"id":"b","function":{"name":"read","arguments":"{}"}}]}}]}` + "\n\n" + `data: {"id":"r","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}` + "\n\ndata: [DONE]\n\n"
	p := nativeProvider()
	p.Protocol = GeminiChat
	_, data, _, err := readProviderStream(p.Protocol, strings.NewReader(body), time.Now(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (geminiChatAdapter{}).Decode(data, p)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{MaxOutputTokens: 100, Inputs: []Input{{Role: Assistant, Calls: result.Calls, ProviderState: result.State}, {Role: Tool, ReplyTo: "a", Text: "a"}, {Role: Tool, ReplyTo: "b", Text: "b"}}}
	encoded, err := encodeRequest(req, p)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ Messages []geminiMessage }
	_ = json.Unmarshal(encoded, &wire)
	if len(wire.Messages[0].Calls) != 2 || wire.Messages[0].Calls[0].ExtraContent == nil || wire.Messages[0].Calls[0].ExtraContent.Google.ThoughtSignature != "signed" || wire.Messages[0].Calls[1].ExtraContent != nil || strings.Contains(string(encoded), "untrusted") || strings.Contains(string(encoded), `"store"`) {
		t.Fatalf("bad signature replay %s", encoded)
	}
	p.Model.ID = "other"
	encoded, err = encodeRequest(req, p)
	if err != nil || strings.Contains(string(encoded), "signed") {
		t.Fatal("foreign state replayed", err)
	}
}
func TestCompatibleReasoningState(t *testing.T) {
	p := nativeProvider()
	p.Protocol = ReasoningChat
	result, err := (reasoningChatAdapter{}).Decode([]byte(`{"id":"r","choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"private","untrusted":"discard"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_cache_hit_tokens":4}}`), p)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{MaxOutputTokens: 50, Inputs: []Input{{Role: User, Text: "hello"}, {Role: Assistant, Text: result.Text, ProviderState: result.State}}}
	data, err := encodeRequest(req, p)
	if err != nil || !strings.Contains(string(data), `"reasoning_content":"private"`) || strings.Contains(string(data), "untrusted") || strings.Contains(string(data), `"store"`) || result.Usage.CachedTokens != 4 {
		t.Fatalf("bad compatible state %s %v", data, err)
	}
	p.Model.ID = "other"
	data, err = encodeRequest(req, p)
	if err != nil || strings.Contains(string(data), "private") {
		t.Fatal("foreign reasoning", err)
	}
}

func TestMiniMaxSeparatesThinkingFromVisibleText(t *testing.T) {
	p := nativeProvider()
	p.Protocol = MiniMaxChat
	data, err := encodeRequest(Request{MaxOutputTokens: 100, Inputs: []Input{{Role: User, Text: "hello"}}}, p)
	if err != nil || !strings.Contains(string(data), `"reasoning_split":true`) {
		t.Fatalf("missing MiniMax reasoning split: %s %v", data, err)
	}
	result, err := (reasoningChatAdapter{}).Decode([]byte(`{"id":"r","choices":[{"message":{"content":"visible","reasoning_content":"private"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`), p)
	if err != nil || result.Text != "visible" {
		t.Fatal(result, err)
	}
	data, err = encodeRequest(Request{MaxOutputTokens: 100, Inputs: []Input{{Role: Assistant, Text: result.Text, ProviderState: result.State}}}, p)
	if err != nil || !strings.Contains(string(data), `"reasoning_content":"private"`) {
		t.Fatal("lost MiniMax continuation", err)
	}
}

func TestAnthropicJSONContinuationSurvivesSerialization(t *testing.T) {
	p := nativeProvider()
	result, err := (anthropicAdapter{}).Decode([]byte(`{"id":"r","type":"message","role":"assistant","content":[{"type":"tool_use","id":"call","name":"read","input":{ "path": "a" }}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":2}}`), p)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{MaxOutputTokens: 100, Inputs: []Input{{Role: User, Text: "read"}, {Role: Assistant, Calls: result.Calls, ProviderState: result.State}, {Role: Tool, ReplyTo: "call", Text: "result"}}}
	if _, err := encodeRequest(req, p); err != nil {
		t.Fatal("JSON whitespace changed continuation", err)
	}
	data, _ := json.Marshal(req)
	var stored Request
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, err := encodeRequest(stored, p); err != nil {
		t.Fatal("stored continuation failed", err)
	}
}
