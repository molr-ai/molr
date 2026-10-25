package model

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

// Builders remain behind pointers: copying a live strings.Builder is invalid.
// Each field is materialized once at the end of assembly.
type streamCall struct {
	id, name, arguments strings.Builder
	extra               *googleExtraContent
}

// readChatStream assembles a normal completion for the existing protocol adapters
// while observing arrival times at the network reader. Raw SSE remains billing
// evidence; neither replay nor UI delivery contributes to timing measurements.
func readChatStream(protocol Protocol, reader io.Reader, started time.Time, now func() time.Time) (raw, completion []byte, timing *Timing, resultErr error) {
	frames := newSSEFrames(reader)
	timing = &Timing{}
	defer func() {
		raw = frames.captured.Bytes()
		timing.RequestMs = max(0, float64(now().Sub(started))/float64(time.Millisecond))
	}()
	invalid := func() error { return core.NewError(core.Unavailable, "invalid or incomplete provider stream") }
	message := map[string]any{"role": "assistant", "content": ""}
	calls := map[int]*streamCall{}
	var reasoning openRouterReasoningStream
	texts := map[string]*strings.Builder{}
	var id, finish string
	var usage json.RawMessage
	var first, last time.Time
	fragments := 0
	done := false
	process := func(payload string) error {
		if done {
			return invalid()
		}
		if payload == "[DONE]" {
			done = true
			return nil
		}
		var chunk struct {
			ID      string          `json:"id"`
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Index        int                        `json:"index"`
				Delta        map[string]json.RawMessage `json:"delta"`
				FinishReason *string                    `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil || (len(chunk.Error) > 0 && string(chunk.Error) != "null") || len(chunk.Choices) > 1 {
			return invalid()
		}
		if chunk.ID != "" {
			if id != "" && id != chunk.ID {
				return invalid()
			}
			id = chunk.ID
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			usage = chunk.Usage
		}
		generated := false
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				return invalid()
			}
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
			for key, value := range choice.Delta {
				if string(value) == "null" {
					continue
				}
				if key == "reasoning_details" && protocol == OpenRouterChat {
					if err := reasoning.append(value); err != nil {
						return err
					}
					generated = generated || len(reasoning.blocks) > 0
					continue
				}
				switch key {
				case "role":
					if string(value) != `"assistant"` {
						return invalid()
					}
				case "tool_calls":
					var parts []struct {
						ExtraContent *googleExtraContent `json:"extra_content"`
						Index        int                 `json:"index"`
						ID           string              `json:"id"`
						Type         string              `json:"type"`
						Function     struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}
					if json.Unmarshal(value, &parts) != nil {
						return invalid()
					}
					for _, part := range parts {
						if part.Index < 0 || part.Index >= 64 {
							return invalid()
						}
						call := calls[part.Index]
						if call == nil {
							call = &streamCall{}
							calls[part.Index] = call
						}
						if protocol == GeminiChat && part.ExtraContent != nil {
							if call.extra != nil && call.extra.Google.ThoughtSignature != part.ExtraContent.Google.ThoughtSignature {
								return invalid()
							}
							call.extra = part.ExtraContent
						}
						call.id.WriteString(part.ID)
						if part.Type != "" && part.Type != "function" {
							return invalid()
						}
						call.name.WriteString(part.Function.Name)
						call.arguments.WriteString(part.Function.Arguments)
						generated = generated || part.Function.Arguments != ""
					}
				default:
					var text string
					if json.Unmarshal(value, &text) == nil {
						builder := texts[key]
						if builder == nil {
							builder = &strings.Builder{}
							texts[key] = builder
						}
						builder.WriteString(text)
						if key == "content" || key == "reasoning_content" || key == "reasoning" || key == "refusal" {
							generated = generated || text != ""
						}
					} else {
						// Keep vendor continuation fields without allowing them to replace the
						// role or tools assembled above. The raw stream is also retained.
						delete(texts, key)
						message[key] = value
					}
				}
			}
		}
		if generated {
			last = now()
			if first.IsZero() {
				first = last
			}
			fragments++
		}
		return nil
	}
	for {
		payload, ok := frames.next()
		if !ok {
			break
		}
		if err := process(payload); err != nil {
			return nil, nil, timing, err
		}
		if done {
			break
		}
	}
	if frames.failed() {
		return nil, nil, timing, invalid()
	}
	if !done || id == "" || finish == "" || len(usage) == 0 {
		return nil, nil, timing, invalid()
	}
	for key, text := range texts {
		message[key] = text.String()
	}
	if len(reasoning.blocks) > 0 {
		message["reasoning_details"] = reasoning.value()
	}
	if len(calls) > 0 {
		ordered := make([]wireCall, len(calls))
		for i := range ordered {
			if calls[i] == nil {
				return nil, nil, timing, invalid()
			}
			ordered[i] = wireCall{ID: calls[i].id.String(), Type: "function", ExtraContent: calls[i].extra}
			ordered[i].Function.Name = calls[i].name.String()
			ordered[i].Function.Arguments = calls[i].arguments.String()
		}
		message["tool_calls"] = ordered
	}
	if !first.IsZero() {
		ms := max(0, float64(first.Sub(started))/float64(time.Millisecond))
		timing.FirstTokenMs = &ms
		if fragments > 1 && last.After(first) {
			ms := float64(last.Sub(first)) / float64(time.Millisecond)
			timing.GenerationMs = &ms
		}
	}
	completion, resultErr = json.Marshal(map[string]any{"id": id, "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": usage})
	return
}
