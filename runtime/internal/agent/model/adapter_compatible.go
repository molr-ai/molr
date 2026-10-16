package model

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"net/http"
)

// The same explicit header contract is used by inference and catalog discovery.
func SetProviderHeaders(h http.Header, protocol Protocol, secret core.Secret) {
	if protocol == AnthropicMessages {
		h.Set("anthropic-version", "2023-06-01")
		if !secret.Empty() {
			h.Set("x-api-key", secret.Reveal())
		}
	} else if !secret.Empty() {
		h.Set("Authorization", "Bearer "+secret.Reveal())
	}
}

// Compatible vendors may require reasoning_content on tool continuations without
// DeepSeek's requirement that every assistant message contain that field.
type reasoningChatAdapter struct{ splitThinking bool }

func (a reasoningChatAdapter) Encode(req Request, p Provider) ([]byte, error) {
	base, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	base, err = mediaMessages(req, p, base)
	if err != nil {
		return nil, err
	}
	messages := make([]deepSeekMessage, 0, len(base))
	for _, msg := range chatMessageLayout(base) {
		item := deepSeekMessage{wireMessage: msg}
		if msg.sourceIndex >= 0 {
			item.ReasoningContent, err = ReplayedReasoning(req.Inputs[msg.sourceIndex], p.source())
			if err != nil {
				return nil, err
			}
		}
		messages = append(messages, item)
	}
	return json.Marshal(chatRequest[deepSeekMessage]{ReasoningSplit: a.splitThinking, Model: p.Model.Name, Messages: messages, Tools: tools, Stream: true, StreamOptions: &chatStreamOptions{IncludeUsage: true}, MaxTokens: req.MaxOutputTokens})
}
func (reasoningChatAdapter) Decode(data []byte, p Provider) (completion, error) {
	// Includes optional prompt_cache_hit_tokens used by compatible services.
	return (deepSeekChatAdapter{}).Decode(data, p)
}

type googleExtraContent struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature,omitempty"`
	} `json:"google"`
}
type geminiChatAdapter struct{}
type geminiMessage struct {
	wireMessage
	ExtraContent *googleExtraContent `json:"extra_content,omitempty"`
}

func (geminiChatAdapter) Encode(req Request, p Provider) ([]byte, error) {
	base, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	base, err = mediaMessages(req, p, base)
	if err != nil {
		return nil, err
	}
	messages := make([]geminiMessage, 0, len(base))
	for _, msg := range chatMessageLayout(base) {
		item := geminiMessage{wireMessage: msg}
		if msg.sourceIndex >= 0 {
			in := req.Inputs[msg.sourceIndex]
			if in.ProviderState != nil && in.ProviderState.Source == p.source() {
				var state geminiMessage
				if json.Unmarshal(in.ProviderState.Payload, &state) != nil {
					return nil, core.Invalid("provider_state", "invalid Gemini continuation state")
				}
				item.ExtraContent = state.ExtraContent
				for i := range item.Calls {
					call := &item.Calls[i]
					for _, previous := range state.Calls {
						if previous.ID == call.ID && previous.Function == call.Function {
							call.ExtraContent = previous.ExtraContent
							break
						}
					}
				}
			}
		}
		messages = append(messages, item)
	}
	return json.Marshal(chatRequest[geminiMessage]{Model: p.Model.Name, Messages: messages, Tools: tools, Stream: true, StreamOptions: &chatStreamOptions{IncludeUsage: true}, MaxCompletionTokens: req.MaxOutputTokens})
}
func (geminiChatAdapter) Decode(data []byte, p Provider) (completion, error) {
	result, raw, err := decodeChat(data)
	if err != nil {
		return result, err
	}
	var state geminiMessage
	if json.Unmarshal(raw, &state) != nil {
		return completion{}, core.NewError(core.Unavailable, "invalid Gemini continuation state")
	}
	result.State = privateState(raw, p)
	return result, nil
}
func continuationEstimate(in Input, source StateSource) (*string, error) {
	var payload any
	if source.Protocol == AnthropicMessages {
		var blocks []anthropicBlock
		if json.Unmarshal(in.ProviderState.Payload, &blocks) != nil {
			return nil, core.Invalid("provider_state", "invalid Anthropic continuation state")
		}
		var thinking []anthropicBlock
		for _, block := range blocks {
			if block.Type == "thinking" || block.Type == "redacted_thinking" {
				thinking = append(thinking, block)
			}
		}
		if len(thinking) == 0 {
			return nil, nil
		}
		payload = thinking
	} else {
		var state geminiMessage
		if json.Unmarshal(in.ProviderState.Payload, &state) != nil {
			return nil, core.Invalid("provider_state", "invalid Gemini continuation state")
		}
		signatures := []string{}
		if state.ExtraContent != nil {
			signatures = append(signatures, state.ExtraContent.Google.ThoughtSignature)
		}
		for _, call := range state.Calls {
			if call.ExtraContent != nil {
				signatures = append(signatures, call.ExtraContent.Google.ThoughtSignature)
			}
		}
		if len(signatures) == 0 {
			return nil, nil
		}
		payload = signatures
	}
	encoded, _ := json.Marshal(payload)
	text := string(encoded)
	return &text, nil
}
