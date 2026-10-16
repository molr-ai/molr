package model

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"regexp"
)

var functionName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type wireCall struct {
	ExtraContent *googleExtraContent `json:"extra_content,omitempty"`
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Function     struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type wireMessage struct {
	sourceIndex int
	Role        Role       `json:"role"`
	Content     any        `json:"content"`
	Calls       []wireCall `json:"tool_calls,omitempty"`
	ReplyTo     string     `json:"tool_call_id,omitempty"`
}
type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
type chatRequest[M any] struct {
	ReasoningSplit      bool               `json:"reasoning_split,omitempty"`
	StreamOptions       *chatStreamOptions `json:"stream_options,omitempty"`
	Model               string             `json:"model"`
	Messages            []M                `json:"messages"`
	Tools               []wireTool         `json:"tools,omitempty"`
	MaxCompletionTokens int                `json:"max_completion_tokens,omitempty"`
	MaxTokens           int                `json:"max_tokens,omitempty"`
	Stream              bool               `json:"stream"`
	Store               *bool              `json:"store,omitempty"`
}
type chatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      json.RawMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		Input   int64 `json:"prompt_tokens"`
		Output  int64 `json:"completion_tokens"`
		Details struct {
			Cached int64  `json:"cached_tokens"`
			Audio  *int64 `json:"audio_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func chatInputs(req Request) ([]wireMessage, []wireTool, error) {
	if len(req.Inputs) == 0 || req.MaxOutputTokens < 1 || req.MaxOutputTokens > MaxOutputLimit {
		return nil, nil, core.Invalid("request", "bounded input and output required")
	}
	messages := []wireMessage{}
	total := 0
	for _, input := range req.Inputs {
		if len(input.FileIDs) > 0 {
			return nil, nil, core.WithReason(core.NewError(core.FailedPrecondition, "file model inputs are not supported by this adapter"), core.UnsupportedAttachment)
		}
		switch input.Role {
		case System, User, Assistant, Tool:
		default:
			return nil, nil, core.Invalid("role", "unsupported role")
		}
		if input.Role == Tool && input.ReplyTo == "" {
			return nil, nil, core.Invalid("reply_to", "tool reply must reference its call")
		}
		message := wireMessage{Role: input.Role, Content: input.Text, ReplyTo: input.ReplyTo}
		total += len(input.Text)
		if input.ProviderState != nil {
			if input.Role != Assistant {
				return nil, nil, core.Invalid("provider_state", "state belongs to assistant messages")
			}
			total += len(input.ProviderState.Payload)
		}
		for _, call := range input.Calls {
			if !functionName.MatchString(call.Name) || !json.Valid(call.Arguments) || call.ID == "" {
				return nil, nil, core.Invalid("tool_call", "invalid call")
			}
			w := wireCall{ID: call.ID, Type: "function"}
			w.Function.Name = call.Name
			w.Function.Arguments = string(call.Arguments)
			message.Calls = append(message.Calls, w)
			total += len(call.Arguments)
		}
		messages = append(messages, message)
	}
	if total > 256<<20 {
		return nil, nil, core.Invalid("inputs", "model context too large")
	}
	tools := []wireTool{}
	for _, tool := range req.Tools {
		if !functionName.MatchString(tool.Name) || !json.Valid(tool.InputSchema) {
			return nil, nil, core.Invalid("tool", "invalid tool definition")
		}
		w := wireTool{Type: "function"}
		w.Function.Name = tool.Name
		w.Function.Description = tool.Description
		w.Function.Parameters = tool.InputSchema
		tools = append(tools, w)
	}
	return messages, tools, nil
}

func decodeChat(data []byte) (completion, json.RawMessage, error) {
	var response chatResponse
	if json.Unmarshal(data, &response) != nil {
		return completion{}, nil, core.NewError(core.Unavailable, "invalid provider response")
	}
	if response.ID == "" || len(response.Choices) != 1 || response.Usage == nil {
		return completion{}, nil, core.NewError(core.Unavailable, "provider response lacks completion evidence")
	}
	choice := response.Choices[0]
	if choice.FinishReason == "" {
		return completion{}, nil, core.NewError(core.Unavailable, "provider response is not terminal")
	}
	var message wireMessage
	if json.Unmarshal(choice.Message, &message) != nil {
		return completion{}, nil, core.NewError(core.Unavailable, "invalid provider message")
	}
	text, ok := message.Content.(string)
	if message.Content != nil && !ok {
		return completion{}, nil, core.NewError(core.Unavailable, "invalid assistant content")
	}
	result := completion{FinishReason: choice.FinishReason, ID: response.ID, Text: text, Usage: Usage{InputTokens: response.Usage.Input, OutputTokens: response.Usage.Output, CachedTokens: response.Usage.Details.Cached}}
	if response.Usage.Details.Audio != nil {
		result.Usage.AudioInputTokens = *response.Usage.Details.Audio
		result.Usage.AudioUsageKnown = true
	}
	for _, call := range message.Calls {
		result.Calls = append(result.Calls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	return result, choice.Message, nil
}

type openAIChatAdapter struct{}

func (openAIChatAdapter) Encode(req Request, provider Provider) ([]byte, error) {
	messages, tools, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	messages, err = mediaMessages(req, provider, messages)
	if err != nil {
		return nil, err
	}
	messages = chatMessageLayout(messages)
	store := false
	return json.Marshal(chatRequest[wireMessage]{Model: provider.Model.Name, Messages: messages, Tools: tools, Stream: true, StreamOptions: &chatStreamOptions{IncludeUsage: true}, MaxCompletionTokens: req.MaxOutputTokens, Store: &store})
}
func (openAIChatAdapter) Decode(data []byte, provider Provider) (completion, error) {
	result, message, err := decodeChat(data)
	if err == nil {
		result.State = privateState(message, provider)
	}
	return result, err
}

// The Runtime supplies one initial system instruction. All later system inputs
// are event/context/control notifications, including those in legacy checkpoints.
// Keep them in place as user turns; adjacency does not make them system policy.
// There is no split system-instruction block to merge in the current Runtime.
func chatMessageLayout(messages []wireMessage) []wireMessage {
	out := append([]wireMessage(nil), messages...)
	for i := range out {
		if i > 0 && out[i].Role == System {
			out[i].Role = User
		}
	}
	return out
}
