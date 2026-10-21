package model

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
)

// The client owns authorization, transport, idempotency and billing. Adapters
// own protocol-specific request/response shapes; the Loop sees only completion.
type protocolAdapter interface {
	Encode(Request, Provider) ([]byte, error)
	Decode([]byte, Provider) (completion, error)
}

type completion struct {
	FinishReason string
	ID           string
	Text         string
	Calls        []ToolCall
	State        *ProviderState
	Usage        Usage
}

func adapterFor(protocol Protocol) (protocolAdapter, error) {
	switch protocol {
	case OpenAIResponses:
		return responsesAdapter{}, nil
	case "", OpenAIChat:
		return openAIChatAdapter{}, nil
	case OpenRouterChat:
		return openRouterChatAdapter{}, nil
	case AnthropicMessages:
		return anthropicAdapter{}, nil
	case GeminiChat:
		return geminiChatAdapter{}, nil
	case MiniMaxChat:
		return reasoningChatAdapter{splitThinking: true}, nil
	case ReasoningChat:
		return reasoningChatAdapter{}, nil
	case DeepSeekChat:
		return deepSeekChatAdapter{}, nil
	default:
		return nil, core.Invalid("protocol", "unsupported model protocol")
	}
}

func (p Provider) source() StateSource { return p.StateSource() }

// StateSource identifies the backend whose continuation fields can be replayed.
func (p Provider) StateSource() StateSource {
	protocol := p.Protocol
	if protocol == "" {
		protocol = OpenAIChat
	}
	return StateSource{Protocol: protocol, ResourceID: p.Model.ResourceID, ModelID: p.Model.ID, ModelName: p.Model.Name, Endpoint: p.Endpoint}
}

// encodeRequest is also used by protocol contract tests.
func encodeRequest(req Request, provider Provider) ([]byte, error) {
	adapter, err := adapterFor(provider.Protocol)
	if err != nil {
		return nil, err
	}
	return adapter.Encode(req, provider)
}

func privateState(payload any, provider Provider) *ProviderState {
	data, _ := json.Marshal(payload)
	return &ProviderState{Source: provider.source(), Payload: data}
}
