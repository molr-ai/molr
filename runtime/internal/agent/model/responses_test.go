package model

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResponsesToolHistoryAndSubscriptionParameters(t *testing.T) {
	p := Provider{Protocol: OpenAIResponses, Model: Model{ID: "model", ResourceID: "provider", Name: "fixture"}, Endpoint: "https://api.openai.com/v1/responses"}
	adapter, err := adapterFor(p.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"resp","status":"completed","output":[{"type":"reasoning","id":"reason","encrypted_content":"opaque","summary":[]},{"type":"function_call","call_id":"call","name":"lookup","namespace":"molr","arguments":"{ }"}],"usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":2}}}`)
	parsed, err := adapter.Decode(raw, p)
	if err != nil || len(parsed.Calls) != 1 || parsed.State == nil || parsed.Usage.CachedTokens != 2 {
		t.Fatal("decode failed", err)
	}
	req := Request{MaxOutputTokens: 100, Tools: []ToolDefinition{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}, Inputs: []Input{{Role: System, Text: "instruction"}, {Role: User, Text: "question"}, {Role: Assistant, Calls: parsed.Calls, ProviderState: parsed.State}, {Role: Tool, ReplyTo: "call", Text: "result"}}}
	encoded, _ := json.Marshal(req)
	var resumed Request
	if err := json.Unmarshal(encoded, &resumed); err != nil {
		t.Fatal(err)
	}
	req = resumed
	if reasoning, err := ReplayedReasoning(req.Inputs[2], p.source()); err != nil || reasoning == nil || !strings.Contains(*reasoning, "opaque") {
		t.Fatal("reasoning missing from context estimate", err)
	}
	body, err := adapter.Encode(req, p)
	if err != nil {
		t.Fatal(err)
	}
	var q map[string]any
	_ = json.Unmarshal(body, &q)
	if q["store"] != false || q["stream"] != true || q["max_output_tokens"] != nil || q["previous_response_id"] != nil {
		t.Fatal("invalid subscription parameters")
	}
	input := q["input"].([]any)
	if input[0].(map[string]any)["role"] != "developer" || input[2].(map[string]any)["encrypted_content"] != "opaque" || input[4].(map[string]any)["call_id"] != "call" {
		t.Fatalf("invalid history: %s", body)
	}
	if q["tools"].([]any)[0].(map[string]any)["type"] != "namespace" {
		t.Fatal("tools not namespaced")
	}
	p.Model.ID = "other"
	body, err = adapter.Encode(req, p)
	if err != nil || strings.Contains(string(body), "opaque") {
		t.Fatal("cross-model reasoning replay")
	}
}
func TestResponsesStreamRequiresCompletionAndMapsLimits(t *testing.T) {
	final := `{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`
	stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":" + final + "}\n\n"
	raw, data, timing, err := readProviderStream(OpenAIResponses, strings.NewReader(stream), time.Now(), time.Now)
	if err != nil || len(raw) == 0 || timing.FirstTokenMs == nil {
		t.Fatal("stream failed", err)
	}
	parsed, err := (responsesAdapter{}).Decode(data, Provider{})
	if err != nil || parsed.Text != "hello" {
		t.Fatal("completion failed", err)
	}
	for _, s := range []string{"data: {\"type\":\"response.created\"}\n\n", stream + "data: {}\n\n", "data: {\"type\":\"response.incomplete\"}\n\n"} {
		if _, _, _, err = readResponsesStream(strings.NewReader(s), time.Now(), time.Now); err == nil {
			t.Fatal("incomplete stream accepted")
		}
	}
	failed := `data: {"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"private"}}}` + "\n\n"
	_, _, _, err = readResponsesStream(strings.NewReader(failed), time.Now(), time.Now)
	f := FailureOf(err)
	if f == nil || f.Reason != core.ModelQuotaExceeded || f.Retryable || f.NoCharge || f.OutcomeKnown || strings.Contains(f.Error(), "private") {
		t.Fatal("incorrect limit handling", err)
	}
}

func TestResponsesPreservesAssistantPhaseAndExactOutputOrder(t *testing.T) {
	p := Provider{Protocol: OpenAIResponses, Model: Model{ID: "model", ResourceID: "provider", Name: "fixture"}}
	raw := []byte(`{"id":"r","status":"completed","output":[{"type":"message","id":"m","role":"assistant","phase":"commentary","status":"completed","content":[{"type":"output_text","text":"Working","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	previous, err := (responsesAdapter{}).Decode(raw, p)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{MaxOutputTokens: 100, Inputs: []Input{{Role: Assistant, Text: previous.Text, ProviderState: previous.State}, {Role: User, Text: "Continue"}}}
	body, err := (responsesAdapter{}).Encode(req, p)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	_ = json.Unmarshal(body, &payload)
	if len(payload.Input) != 2 || payload.Input[0]["phase"] != "commentary" || payload.Input[0]["id"] != "m" {
		t.Fatalf("assistant phase or order lost: %s", body)
	}
	req.Inputs[0].Text = "changed"
	if _, err = (responsesAdapter{}).Encode(req, p); err == nil {
		t.Fatal("replayed changed history")
	}
	duplicate := []byte(`{"id":"r","status":"completed","output":[{"type":"function_call","call_id":"x","name":"lookup","arguments":"{}"},{"type":"function_call","call_id":"x","name":"lookup","arguments":"{}"}],"usage":{}}`)
	if _, err = (responsesAdapter{}).Decode(duplicate, p); err == nil {
		t.Fatal("duplicate tool call accepted")
	}
}

func TestResponsesTransportRetainsReferenceReceipt(t *testing.T) {
	f := newContractFixture(t)
	c := f.Client.(*client)
	resolve := c.options.Resolve
	c.options.Resolve = func(ctx context.Context, p core.Principal, id core.ID) (Provider, error) {
		out, err := resolve(ctx, p, id)
		out.Protocol = OpenAIResponses
		out.Endpoint = "https://api.openai.com/v1/responses"
		out.Model.PricingInfo = &PricingInfo{Basis: "subscription-reference", Source: "manual"}
		return out, err
	}
	c.options.CheckDestination = func(_ context.Context, _ core.Execution, _ core.ID, dest string) error {
		if dest != "https://api.openai.com/v1/responses" {
			t.Error("wrong destination")
		}
		return nil
	}
	calls := 0
	c.options.HTTPClient = &http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request["stream"] != true || request["store"] != false || request["messages"] != nil || request["max_output_tokens"] != nil {
			t.Error("wrong Responses request")
		}
		body := `data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","id":"m","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":4,"output_tokens":1}}}` + "\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	stream, err := c.Generate(t.Context(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	text := ""
	for {
		delta, err := stream.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if delta.Kind == TextDelta {
			text += delta.Text
		}
	}
	receipt, err := c.receipts.ByOperation(t.Context(), f.Owner.OwnerID, f.Request.Execution.OperationID)
	if err != nil || calls != 1 || text != "hello" || receipt.Charged == nil || receipt.Charged.Micros != 5 || receipt.PricingInfo.Basis != "subscription-reference" {
		t.Fatalf("reference receipt lost: %+v %v", receipt, err)
	}
}
