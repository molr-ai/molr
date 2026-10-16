package model

import (
	"bytes"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"strings"
)

type anthropicAdapter struct{}
type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   any             `json:"content,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	Source    *anthropicImage `json:"source,omitempty"`
}
type anthropicImage struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type anthropicMessage struct {
	Role    Role             `json:"role"`
	Content []anthropicBlock `json:"content"`
}
type anthropicUsage struct {
	Input   *int64 `json:"input_tokens"`
	Output  *int64 `json:"output_tokens"`
	Cached  int64  `json:"cache_read_input_tokens"`
	Created int64  `json:"cache_creation_input_tokens"`
}
type anthropicResponse struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Role       Role             `json:"role"`
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      *anthropicUsage  `json:"usage"`
}

func (anthropicAdapter) Encode(req Request, p Provider) ([]byte, error) {
	base, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	base, err = mediaMessages(req, p, base)
	if err != nil {
		return nil, err
	}
	var system any
	messages := []anthropicMessage{}
	for _, msg := range chatMessageLayout(base) {
		if msg.Role == System {
			system = msg.Content
			continue
		}
		blocks := []anthropicBlock{}
		role := msg.Role
		if role == Tool {
			role = User
			blocks = append(blocks, anthropicBlock{Type: "tool_result", ToolUseID: msg.ReplyTo, Content: msg.Content})
		} else if text, ok := msg.Content.(string); ok {
			if text != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
			}
		} else {
			// mediaMessages emits only trusted text and inline image/audio parts.
			parts, ok := msg.Content.([]map[string]any)
			if !ok {
				return nil, core.Invalid("content", "invalid media content")
			}
			for _, part := range parts {
				switch part["type"] {
				case "text":
					blocks = append(blocks, anthropicBlock{Type: "text", Text: part["text"].(string)})
				case "image_url":
					image := part["image_url"].(map[string]any)["url"].(string)
					mime, data, ok := strings.Cut(strings.TrimPrefix(image, "data:"), ";base64,")
					if !ok {
						return nil, core.Invalid("image", "inline image required")
					}
					blocks = append(blocks, anthropicBlock{Type: "image", Source: &anthropicImage{Type: "base64", MediaType: mime, Data: data}})
				default:
					return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Anthropic media format unsupported"), core.UnsupportedAttachment)
				}
			}
		}
		for _, call := range msg.Calls {
			blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: json.RawMessage(call.Function.Arguments)})
		}
		if msg.sourceIndex >= 0 {
			in := req.Inputs[msg.sourceIndex]
			if in.ProviderState != nil && in.ProviderState.Source == p.source() {
				var previous []anthropicBlock
				if json.Unmarshal(in.ProviderState.Payload, &previous) != nil {
					return nil, core.Invalid("provider_state", "invalid Anthropic continuation state")
				}
				decoded, err := anthropicContent(previous)
				if err != nil || decoded.Text != in.Text || !sameToolCalls(decoded.Calls, in.Calls) {
					return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Anthropic continuation differs from context"), core.ModelContextIncompatible)
				}
				// Whitelisted blocks retain signed thinking in its original position.
				blocks = previous
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
		} else {
			messages = append(messages, anthropicMessage{Role: role, Content: blocks})
		}
	}
	if len(messages) == 0 || messages[0].Role != User {
		return nil, core.Invalid("messages", "Anthropic requires an initial user message")
	}
	nativeTools := []any{}
	for _, tool := range tools {
		nativeTools = append(nativeTools, map[string]any{"name": tool.Function.Name, "description": tool.Function.Description, "input_schema": tool.Function.Parameters})
	}
	return json.Marshal(struct {
		Model     string             `json:"model"`
		System    any                `json:"system,omitempty"`
		Messages  []anthropicMessage `json:"messages"`
		Tools     []any              `json:"tools,omitempty"`
		MaxTokens int                `json:"max_tokens"`
		Stream    bool               `json:"stream"`
	}{p.Model.Name, system, messages, nativeTools, req.MaxOutputTokens, true})
}
func sameToolCalls(a, b []ToolCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		var left, right bytes.Buffer
		if json.Compact(&left, a[i].Arguments) != nil || json.Compact(&right, b[i].Arguments) != nil || a[i].ID != b[i].ID || a[i].Name != b[i].Name || !bytes.Equal(left.Bytes(), right.Bytes()) {
			return false
		}
	}
	return true
}
func anthropicContent(blocks []anthropicBlock) (completion, error) {
	result := completion{}
	var text strings.Builder
	invalid := func() (completion, error) {
		return completion{}, core.NewError(core.Unavailable, "invalid Anthropic content block")
	}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			var object map[string]json.RawMessage
			if block.ID == "" || !functionName.MatchString(block.Name) || json.Unmarshal(block.Input, &object) != nil || object == nil {
				return invalid()
			}
			result.Calls = append(result.Calls, ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
		case "thinking":
			if block.Thinking == nil || block.Signature == "" {
				return invalid()
			}
		case "redacted_thinking":
			if block.Data == "" {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	result.Text = text.String()
	return result, nil
}
func (anthropicAdapter) Decode(data []byte, p Provider) (completion, error) {
	var response anthropicResponse
	if json.Unmarshal(data, &response) != nil || response.ID == "" || response.Type != "message" || response.Role != Assistant || response.Usage == nil || response.Usage.Input == nil || response.Usage.Output == nil {
		return completion{}, core.NewError(core.Unavailable, "Anthropic response lacks completion evidence")
	}
	result, err := anthropicContent(response.Content)
	if err != nil {
		return result, err
	}
	switch response.StopReason {
	case "end_turn", "stop_sequence":
		result.FinishReason = "stop"
	case "tool_use":
		result.FinishReason = "tool_calls"
	case "max_tokens":
		result.FinishReason = "length"
	case "refusal":
		result.FinishReason = "content_filter"
	default:
		return completion{}, core.NewError(core.Unavailable, "unsupported Anthropic stop reason")
	}
	usage := response.Usage
	// We do not request cache writes. Their separate tariff cannot be represented
	// by the existing input/output/cache-read ledger; retain unknown cost on receipt.
	if *usage.Input < 0 || *usage.Output < 0 || usage.Cached < 0 || usage.Created != 0 || *usage.Input > 1<<50 || usage.Cached > 1<<50 {
		return completion{}, core.NewError(core.Unavailable, "unsupported Anthropic usage")
	}
	result.ID = response.ID
	result.Usage = Usage{InputTokens: *usage.Input + usage.Cached, OutputTokens: *usage.Output, CachedTokens: usage.Cached}
	result.State = privateState(response.Content, p)
	return result, nil
}
