package model

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
)

type responsesAdapter struct{}
type responseItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id,omitempty"`
	Role             string          `json:"role,omitempty"`
	CallID           string          `json:"call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Namespace        string          `json:"namespace,omitempty"`
	Arguments        string          `json:"arguments,omitempty"`
	EncryptedContent string          `json:"encrypted_content,omitempty"`
	Summary          json.RawMessage `json:"summary,omitempty"`
	Content          []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content,omitempty"`
}
type responsesResponse struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Usage  *struct {
		Input   int64 `json:"input_tokens"`
		Output  int64 `json:"output_tokens"`
		Details struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

func (responsesAdapter) Encode(req Request, p Provider) ([]byte, error) {
	base, _, err := chatInputs(req)
	if err != nil {
		return nil, err
	}
	messages, err := mediaMessages(req, p, base)
	if err != nil {
		return nil, err
	}
	input := []any{}
	for _, m := range messages {
		if m.Role == Assistant && m.sourceIndex >= 0 {
			state := req.Inputs[m.sourceIndex].ProviderState
			if state != nil && state.Source == p.source() {
				var output []json.RawMessage
				if json.Unmarshal(state.Payload, &output) != nil || len(output) == 0 {
					return nil, core.Invalid("provider_state", "invalid Responses output")
				}
				body, _ := json.Marshal(map[string]any{"id": "replay", "status": "completed", "output": output, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}})
				previous, err := (responsesAdapter{}).Decode(body, p)
				text, _ := m.Content.(string)
				if err != nil || previous.Text != text || len(previous.Calls) != len(m.Calls) {
					return nil, core.Invalid("provider_state", "Responses history changed")
				}
				if !sameToolCalls(previous.Calls, req.Inputs[m.sourceIndex].Calls) {
					return nil, core.Invalid("provider_state", "Responses tool history changed")
				}

				for _, item := range output {
					input = append(input, item)
				}
				continue
			}
		}
		if m.Role == Tool {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ReplyTo, "output": m.Content})
			continue
		}
		role := string(m.Role)
		if m.Role == System {
			role = "developer"
		}
		content := []any{}
		if text, ok := m.Content.(string); ok {
			if text != "" {
				kind := "input_text"
				if m.Role == Assistant {
					kind = "output_text"
				}
				content = append(content, map[string]any{"type": kind, "text": text})
			}
		} else if parts, ok := m.Content.([]map[string]any); ok {
			for _, part := range parts {
				switch part["type"] {
				case "text":
					content = append(content, map[string]any{"type": "input_text", "text": part["text"]})
				case "image_url":
					image := part["image_url"].(map[string]any)
					content = append(content, map[string]any{"type": "input_image", "image_url": image["url"], "detail": image["detail"]})
				default:
					return nil, core.Invalid("media", "unsupported ChatGPT input")
				}
			}
		}
		if len(content) > 0 {
			var value any = content
			if m.Role == Assistant {
				value = m.Content
			}
			input = append(input, map[string]any{"role": role, "content": value})
		}
		for _, call := range m.Calls {
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "namespace": "molr", "arguments": call.Function.Arguments})
		}
	}
	tools := []any{}
	if len(req.Tools) > 0 {
		functions := []any{}
		for _, t := range req.Tools {
			functions = append(functions, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.InputSchema, "strict": false})
		}
		tools = append(tools, map[string]any{"type": "namespace", "name": "molr", "description": "Molr tools", "tools": functions})
	}
	// The subscription route rejects max_output_tokens and previous_response_id.
	return json.Marshal(map[string]any{"model": p.Model.Name, "input": input, "tools": tools, "store": false, "stream": true, "include": []string{"reasoning.encrypted_content"}})
}
func (responsesAdapter) Decode(data []byte, p Provider) (completion, error) {
	var r responsesResponse
	if json.Unmarshal(data, &r) != nil || r.ID == "" || r.Status != "completed" || r.Usage == nil || r.Output == nil {
		return completion{}, core.NewError(core.Unavailable, "invalid or incomplete Responses result")
	}
	out := completion{ID: r.ID, FinishReason: "stop", Usage: Usage{InputTokens: r.Usage.Input, OutputTokens: r.Usage.Output, CachedTokens: r.Usage.Details.Cached}}
	seenCalls := map[string]bool{}
	for _, raw := range r.Output {
		var item responseItem
		if json.Unmarshal(raw, &item) != nil {
			return completion{}, core.Invalid("response", "invalid output item")
		}
		switch item.Type {
		case "message":
			if item.Role != "assistant" {
				return completion{}, core.Invalid("response", "invalid response role")
			}
			for _, c := range item.Content {
				switch c.Type {
				case "output_text":
					out.Text += c.Text
				case "refusal":
					out.Text += c.Refusal
				default:
					return completion{}, core.Invalid("response", "unsupported response content")
				}
			}
		case "function_call":
			if item.CallID == "" || seenCalls[item.CallID] || !functionName.MatchString(item.Name) || !json.Valid([]byte(item.Arguments)) {
				return completion{}, core.Invalid("response", "invalid function call")
			}
			seenCalls[item.CallID] = true
			if item.Namespace != "" && item.Namespace != "molr" {
				return completion{}, core.Invalid("response", "unknown tool namespace")
			}
			out.Calls = append(out.Calls, ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
			out.FinishReason = "tool_calls"
		case "reasoning":
			// Preserve encrypted state and every message phase in original order.

		default:
			return completion{}, core.Invalid("response", "unsupported Responses output")
		}
	}
	if len(r.Output) > 0 {
		out.State = privateState(r.Output, p)
	}
	return out, nil
}
