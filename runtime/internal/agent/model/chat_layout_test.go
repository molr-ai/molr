package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/media"
)

func TestChatLayoutKeepsNotificationsAndToolHistoryInOrder(t *testing.T) {
	req := Request{MaxOutputTokens: 128, Inputs: []Input{
		{Role: System, Text: "Base instructions"},
		{Role: System, Text: "Runtime event: legacy adjacent notification"},
		{Role: User, Text: "Runtime event: current query"},
		{Role: Assistant, Calls: []ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}, {ID: "b", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: Tool, ReplyTo: "a", Text: "first", Media: []media.Part{{Kind: "image", MIME: "image/png", Path: "/work/image.png", Data: []byte("image bytes")}}},
		{Role: Tool, ReplyTo: "b", Text: "second"},
		{Role: System, Text: "Latest permissions", RuntimeContext: true},
		{Role: User, Text: "Next query"},
		{Role: System, Text: "Recovery instructions"},
	}}
	before, _ := json.Marshal(req)
	body, err := encodeRequest(req, Provider{Protocol: OpenAIChat, Model: Model{SupportsImages: true}})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ Messages []wireMessage }
	if err = json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	var roles []Role
	for _, m := range wire.Messages {
		roles = append(roles, m.Role)
	}
	if !reflect.DeepEqual(roles, []Role{System, User, User, Assistant, Tool, Tool, User, User, User, User}) {
		t.Fatalf("roles: %v", roles)
	}
	if wire.Messages[0].Content != "Base instructions" || wire.Messages[1].Content != "Runtime event: legacy adjacent notification" || wire.Messages[2].Content != "Runtime event: current query" || wire.Messages[7].Content != "Latest permissions" || wire.Messages[8].Content != "Next query" || wire.Messages[9].Content != "Recovery instructions" {
		t.Fatalf("lost context: %s", body)
	}
	if len(wire.Messages[3].Calls) != 2 || wire.Messages[4].ReplyTo != "a" || wire.Messages[5].ReplyTo != "b" {
		t.Fatalf("broken tool batch: %s", body)
	}
	if _, ok := wire.Messages[6].Content.([]any); !ok {
		t.Fatalf("lost multimodal content: %s", body)
	}
	after, _ := json.Marshal(req)
	if string(before) != string(after) {
		t.Fatal("wire rendering mutated checkpoint input")
	}
}

func TestOpenAIChatPreservesCanonicalMessages(t *testing.T) {
	for _, inputs := range [][]Input{
		{{Role: System, Text: "instructions"}, {Role: User, Text: "hello"}},
		{{Role: User, Text: "hello"}},
	} {
		body, err := encodeRequest(Request{Inputs: inputs, MaxOutputTokens: 16}, Provider{})
		if err != nil {
			t.Fatal(err)
		}
		var wire struct{ Messages []wireMessage }
		if err = json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		if len(wire.Messages) != len(inputs) {
			t.Fatalf("unexpected messages: %s", body)
		}
		for i, m := range wire.Messages {
			if m.Role != inputs[i].Role || m.Content != inputs[i].Text {
				t.Fatalf("changed message: %s", body)
			}
		}
	}
}

func TestDeepSeekChatLayoutPreservesReasoningAndAdjacentNotifications(t *testing.T) {
	provider := reasoningProvider()
	state := privateState(map[string]any{"reasoning_content": "retained reasoning"}, provider)
	request := Request{MaxOutputTokens: 128, Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{}`)}}, Inputs: []Input{
		{Role: System, Text: "base"}, {Role: System, Text: "Runtime notification"}, {Role: User, Text: "query"},
		{Role: Assistant, Text: "calling read", ProviderState: state, Calls: []ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: Tool, ReplyTo: "a", Text: "result"}, {Role: System, Text: "Runtime notification"},
	}}
	body, err := encodeRequest(request, provider)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ Messages []deepSeekMessage }
	if err = json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Messages) != 6 || wire.Messages[0].Content != "base" || wire.Messages[1].Role != User || wire.Messages[3].ReasoningContent == nil || *wire.Messages[3].ReasoningContent != "retained reasoning" || wire.Messages[4].ReplyTo != "a" || wire.Messages[5].Role != User {
		t.Fatalf("lost reasoning or order: %s", body)
	}
}
