package model

import (
	"bytes"
	"encoding/json"
	"molr.ai/runtime/internal/media"
	"strings"
	"testing"
)

func TestMediaAdaptersPreserveToolBatchAndReasoning(t *testing.T) {
	for _, protocol := range []Protocol{OpenAIChat, DeepSeekChat} {
		t.Run(string(protocol), func(t *testing.T) {
			p := reasoningProvider()
			p.Protocol = protocol
			p.Model.SupportsImages = true
			state := privateState(map[string]any{"reasoning_content": "keep reasoning"}, p)
			req := Request{MaxOutputTokens: 64, Tools: []ToolDefinition{{Name: "file_read", InputSchema: json.RawMessage(`{"type":"object"}`)}}, Inputs: []Input{
				{Role: Assistant, ProviderState: state, Calls: []ToolCall{{ID: "a", Name: "file_read", Arguments: json.RawMessage(`{}`)}, {ID: "b", Name: "file_read", Arguments: json.RawMessage(`{}`)}}},
				{Role: Tool, ReplyTo: "a", Text: `{"content_loaded":true}`, Media: []media.Part{{Kind: "image", MIME: "image/png", Path: "/work/image.png", Data: []byte("image bytes")}}},
				{Role: Tool, ReplyTo: "b", Text: "second tool"}, {Role: User, Text: "follow-up"},
			}}
			body, err := encodeRequest(req, p)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Messages []struct {
					Role      string
					Content   json.RawMessage
					Reasoning string `json:"reasoning_content"`
				}
			}
			if err = json.Unmarshal(body, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Messages) != 5 || wire.Messages[1].Role != "tool" || wire.Messages[2].Role != "tool" || wire.Messages[3].Role != "user" || string(wire.Messages[4].Content) != `"follow-up"` {
				t.Fatalf("broken ordering: %s", body)
			}
			if !bytes.Contains(wire.Messages[3].Content, []byte("data:image/png;base64,")) {
				t.Fatalf("missing image: %s", body)
			}
			if protocol == DeepSeekChat && wire.Messages[0].Reasoning != "keep reasoning" {
				t.Fatal("reasoning lost")
			}
			if bytes.Contains(sanitizedRequest(body), []byte("data:image/png;base64,")) {
				t.Fatal("trace leaked media")
			}
			checkpoint, _ := json.Marshal(req.Inputs)
			if bytes.Contains(checkpoint, []byte("aW1hZ2UgYnl0ZXM=")) {
				t.Fatal("checkpoint contains bytes")
			}
			p.Model.SupportsImages = false
			body, err = encodeRequest(req, p)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(body, []byte("data:image")) || !bytes.Contains(body, []byte("not loaded")) {
				t.Fatalf("unsupported model received image: %s", body)
			}
		})
	}
}
func TestAudioEncodingAndAccounting(t *testing.T) {
	p := Provider{Protocol: OpenAIChat, Model: Model{SupportsAudio: true}}
	r := Request{MaxOutputTokens: 16, Inputs: []Input{{Role: Tool, ReplyTo: "a", Text: "audio", Media: []media.Part{{Kind: "audio", MIME: "audio/wav", Data: []byte("wave")}}}}}
	body, err := encodeRequest(r, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"input_audio"`) {
		t.Fatalf("missing audio: %s", body)
	}
	p.Protocol = DeepSeekChat
	body, err = encodeRequest(r, p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"input_audio"`) {
		t.Fatal("DeepSeek received unsupported audio")
	}
	rate := int64(20_000_000)
	money, err := charge(&Pricing{Currency: "USD", InputMicrosPerMillion: 1_000_000, AudioInputMicrosPerMillion: &rate}, Usage{InputTokens: 100, AudioInputTokens: 80})
	if err != nil || money.Micros != 1620 {
		t.Fatalf("wrong audio bill: %+v %v", money, err)
	}
	if _, err = charge(&Pricing{Currency: "USD"}, Usage{InputTokens: 100, AudioInputTokens: 80}); err == nil {
		t.Fatal("missing audio price accepted")
	}
}
