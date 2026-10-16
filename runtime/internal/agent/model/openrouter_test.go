package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func routerProvider() Provider {
	return Provider{Protocol: OpenRouterChat, Model: Model{ID: "model", ResourceID: "provider", Name: "vendor/model"}, Endpoint: "https://openrouter.example/api/v1/chat/completions"}
}

func TestOpenRouterStreamingToolContinuation(t *testing.T) {
	// Reused indices across types occur in real upstream streams. Encrypted
	// blocks remain separate; consecutive summary/text/signature deltas merge.
	chunks := []string{
		`{"reasoning_details":[{"type":"reasoning.summary","summary":"Think ","index":0,"format":"openai-responses-v1"}]}`,
		`{"reasoning_details":[{"type":"reasoning.summary","summary":"carefully","index":0}]}`,
		`{"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque-a","id":"a","index":0}]}`,
		`{"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque-b","id":"b","index":0}]}`,
		`{"reasoning_details":[{"type":"reasoning.text","text":"检查","signature":null,"index":1}]}`,
		`{"reasoning_details":[{"type":"reasoning.text","text":"工具","signature":"sig-","index":1}]}`,
		`{"reasoning_details":[{"type":"reasoning.text","signature":"end","index":1}],"tool_calls":[{"index":0,"id":"call","type":"function","function":{"name":"read","arguments":"{}"}}]}`,
	}
	var body strings.Builder
	for _, delta := range chunks {
		body.WriteString(`data: {"id":"response","choices":[{"index":0,"delta":` + delta + `}]}` + "\n\n")
	}
	body.WriteString("data: {\"id\":\"response\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":10,\"prompt_tokens_details\":{\"cached_tokens\":12}}}\n\ndata: [DONE]\n\n")
	_, normalized, timing, err := readChatStream(OpenRouterChat, strings.NewReader(body.String()), time.Now(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	p := routerProvider()
	result, err := (openRouterChatAdapter{}).Decode(normalized, p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.CachedTokens != 12 || len(result.Calls) != 1 || timing.FirstTokenMs == nil {
		t.Fatalf("lost completion evidence: %+v", result)
	}
	input := Input{Role: Assistant, Text: result.Text, Calls: result.Calls, ProviderState: result.State}
	req := Request{Inputs: []Input{input, {Role: Tool, ReplyTo: "call", Text: "tool result"}}, MaxOutputTokens: 100}
	encoded, err := encodeRequest(req, p)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Messages []struct {
			Details []map[string]any `json:"reasoning_details"`
			Reply   string           `json:"tool_call_id"`
		} `json:"messages"`
		MaxTokens           int   `json:"max_tokens"`
		Store               *bool `json:"store"`
		MaxCompletionTokens *int  `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatal(err)
	}
	details := request.Messages[0].Details
	if len(details) != 4 || details[0]["summary"] != "Think carefully" || details[1]["data"] != "opaque-a" || details[2]["data"] != "opaque-b" || details[3]["text"] != "检查工具" || details[3]["signature"] != "sig-end" {
		t.Fatalf("continuation changed: %s", encoded)
	}
	if request.MaxTokens != 100 || request.Store != nil || request.MaxCompletionTokens != nil || request.Messages[1].Reply != "call" {
		t.Fatalf("wrong wire contract: %s", encoded)
	}
	if text, err := ReplayedReasoning(input, p.source()); err != nil || text == nil || !strings.Contains(*text, "opaque-a") {
		t.Fatal("context estimate omitted replayed state", err)
	}
	for _, change := range []func(*Provider){
		func(p *Provider) { p.Protocol = OpenAIChat },
		func(p *Provider) { p.Model.ID = "other" },
		func(p *Provider) { p.Model.ResourceID = "other" },
		func(p *Provider) { p.Model.Name = "other" },
		func(p *Provider) { p.Endpoint = "https://other.example" },
	} {
		other := p
		change(&other)
		data, err := encodeRequest(req, other)
		if err != nil || strings.Contains(string(data), "reasoning_details") {
			t.Fatalf("state crossed backend boundary: %s %v", data, err)
		}
	}
}

func TestOpenRouterJSONStateWhitelistAndValidation(t *testing.T) {
	p := routerProvider()
	for _, field := range []string{`"reasoning":""`, `"reasoning":"private"`, `"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque","extra":{"x":1}}]`} {
		body := `{"id":"r","choices":[{"message":{"role":"assistant","content":"answer",` + field + `,"unknown":"must-not-replay"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`
		out, err := (openRouterChatAdapter{}).Decode([]byte(body), p)
		if err != nil {
			t.Fatal(err)
		}
		data, err := encodeRequest(Request{Inputs: []Input{{Role: Assistant, Text: out.Text, ProviderState: out.State}}, MaxOutputTokens: 20}, p)
		if err != nil || !strings.Contains(string(data), field) || strings.Contains(string(data), "must-not-replay") {
			t.Fatalf("invalid replay %s %v", data, err)
		}
	}
	for _, payload := range []string{`{"reasoning":42}`, `{"reasoning_details":{}}`, `{"reasoning_details":[null]}`, `{"reasoning_details":[1]}`} {
		_, err := encodeRequest(Request{Inputs: []Input{{Role: Assistant, ProviderState: &ProviderState{Source: p.source(), Payload: json.RawMessage(payload)}}}, MaxOutputTokens: 20}, p)
		if err == nil {
			t.Fatalf("accepted malformed state: %s", payload)
		}
	}
}
