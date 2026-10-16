package model

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
)

// DeepSeek uses max_tokens and requires reasoning_content to be replayed on
// subsequent tool turns: https://api-docs.deepseek.com/guides/thinking_mode/
type deepSeekChatAdapter struct{}
type deepSeekState struct {
	ReasoningContent *string `json:"reasoning_content,omitempty"`
}
type deepSeekMessage struct {
	wireMessage
	deepSeekState
}

func (deepSeekChatAdapter) Encode(req Request, provider Provider) ([]byte, error) {
	base, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	messages := make([]deepSeekMessage, len(base))
	for i, message := range base {
		messages[i].wireMessage = message
		reasoning, err := ReplayedReasoning(req.Inputs[i], provider.source())
		if err != nil {
			return nil, err
		}
		messages[i].ReasoningContent = reasoning
		// With tools, DeepSeek requires state for every assistant message,
		// including final answers. Never fabricate reasoning or silently discard
		// context when an old checkpoint or model switch cannot provide it.
		if len(tools) > 0 && message.Role == Assistant && messages[i].ReasoningContent == nil {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "DeepSeek continuation state is missing; reset the assistant context"), core.ModelContextIncompatible)
		}
	}
	expanded := []deepSeekMessage{}
	mediaBase, err := mediaMessages(req, provider, base)
	if err != nil {
		return nil, err
	}
	for _, msg := range chatMessageLayout(mediaBase) {
		item := deepSeekMessage{wireMessage: msg}
		if msg.sourceIndex >= 0 {
			item.deepSeekState = messages[msg.sourceIndex].deepSeekState
		}
		expanded = append(expanded, item)
	}
	messages = expanded
	return json.Marshal(chatRequest[deepSeekMessage]{Model: provider.Model.Name, Messages: messages, Tools: tools, Stream: true, StreamOptions: &chatStreamOptions{IncludeUsage: true}, MaxTokens: req.MaxOutputTokens})
}
func (deepSeekChatAdapter) Decode(data []byte, provider Provider) (completion, error) {
	result, message, err := decodeChat(data)
	if err != nil {
		return result, err
	}
	var state deepSeekState
	if json.Unmarshal(message, &state) != nil {
		return completion{}, core.NewError(core.Unavailable, "invalid DeepSeek reasoning state")
	}
	result.State = privateState(message, provider)
	var usage struct {
		Usage struct {
			Hit  *int64 `json:"prompt_cache_hit_tokens"`
			Miss *int64 `json:"prompt_cache_miss_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &usage) != nil {
		return completion{}, core.NewError(core.Unavailable, "invalid DeepSeek usage")
	}
	if hit := usage.Usage.Hit; hit != nil {
		if *hit < 0 || *hit > result.Usage.InputTokens || (usage.Usage.Miss != nil && *usage.Usage.Miss != result.Usage.InputTokens-*hit) {
			return completion{}, core.NewError(core.Unavailable, "invalid cache usage")
		}
		result.Usage.CachedTokens = *hit
	}
	return result, nil
}

// ReplayedReasoning is shared by encoding and advisory context estimates.
// Other opaque provider fields stay private and do not occupy input tokens.
func ReplayedReasoning(in Input, source StateSource) (*string, error) {
	if in.ProviderState == nil || in.ProviderState.Source != source {
		return nil, nil
	}
	if source.Protocol == OpenRouterChat {
		state, err := readOpenRouterState(in.ProviderState.Payload)
		if err != nil {
			return nil, err
		}
		if len(state.Details) > 0 {
			data, _ := json.Marshal(state.Details)
			value := string(data)
			return &value, nil
		}
		return state.Reasoning, nil
	}
	if source.Protocol != DeepSeekChat {
		return nil, nil
	}
	var state deepSeekState
	if json.Unmarshal(in.ProviderState.Payload, &state) != nil {
		return nil, core.Invalid("provider_state", "invalid DeepSeek continuation state")
	}
	return state.ReasoningContent, nil
}
