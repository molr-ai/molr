package model

import (
	"encoding/json"

	"molr.ai/runtime/internal/core"
)

type openRouterChatAdapter struct{}
type openRouterState struct {
	Reasoning *string           `json:"reasoning,omitempty"`
	Details   []json.RawMessage `json:"reasoning_details,omitempty"`
}
type openRouterMessage struct {
	wireMessage
	openRouterState
}

func readOpenRouterState(data []byte) (openRouterState, error) {
	var state openRouterState
	if json.Unmarshal(data, &state) != nil {
		return state, core.Invalid("provider_state", "invalid OpenRouter continuation state")
	}
	for _, detail := range state.Details {
		var object map[string]json.RawMessage
		if json.Unmarshal(detail, &object) != nil || object == nil {
			return state, core.Invalid("provider_state", "invalid OpenRouter reasoning detail")
		}
	}
	// Details carry signatures/encrypted blocks. Do not duplicate the plaintext
	// alias when the provider supplies the complete structured continuation.
	if len(state.Details) > 0 {
		state.Reasoning = nil
	}
	return state, nil
}

func (openRouterChatAdapter) Encode(req Request, provider Provider) ([]byte, error) {
	base, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	base, err = mediaMessages(req, provider, base)
	if err != nil {
		return nil, err
	}
	messages := []openRouterMessage{}
	for _, msg := range chatMessageLayout(base) {
		item := openRouterMessage{wireMessage: msg}
		if msg.sourceIndex >= 0 {
			in := req.Inputs[msg.sourceIndex]
			if in.ProviderState != nil && in.ProviderState.Source == provider.source() {
				item.openRouterState, err = readOpenRouterState(in.ProviderState.Payload)
				if err != nil {
					return nil, err
				}
			}
		}
		messages = append(messages, item)
	}
	return json.Marshal(chatRequest[openRouterMessage]{Model: provider.Model.Name, Messages: messages, Tools: tools, Stream: true, StreamOptions: &chatStreamOptions{IncludeUsage: true}, MaxTokens: req.MaxOutputTokens})
}

func (openRouterChatAdapter) Decode(data []byte, provider Provider) (completion, error) {
	result, message, err := decodeChat(data)
	if err != nil {
		return result, err
	}
	if _, err := readOpenRouterState(message); err != nil {
		return completion{}, core.Wrap(core.Unavailable, "invalid OpenRouter reasoning state", err)
	}
	result.State = privateState(message, provider)
	return result, nil
}
