package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

// readChatStream assembles a normal completion for the existing protocol adapters
// while observing arrival times at the network reader. Raw SSE remains billing
// evidence; neither replay nor UI delivery contributes to timing measurements.
func readChatStream(reader io.Reader, started time.Time, now func() time.Time) (raw, completion []byte, timing *Timing, resultErr error) {
	var captured bytes.Buffer
	limited := &io.LimitedReader{R: reader, N: (8 << 20) + 1}
	scanner := bufio.NewScanner(io.TeeReader(limited, &captured))
	scanner.Buffer(make([]byte, 4096), (8<<20)+1)
	timing = &Timing{}
	defer func() {
		raw = append([]byte(nil), captured.Bytes()...)
		timing.RequestMs = max(0, float64(now().Sub(started))/float64(time.Millisecond))
	}()
	invalid := func() error { return core.NewError(core.Unavailable, "invalid or incomplete provider stream") }
	message := map[string]any{"role": "assistant", "content": ""}
	calls := map[int]*wireCall{}
	var id, finish string
	var usage json.RawMessage
	var first, last time.Time
	fragments := 0
	done := false
	data := []string{}
	process := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
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
				switch key {
				case "role":
					if string(value) != `"assistant"` {
						return invalid()
					}
				case "tool_calls":
					var parts []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
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
							call = &wireCall{Type: "function"}
							calls[part.Index] = call
						}
						call.ID += part.ID
						if part.Type != "" && part.Type != "function" {
							return invalid()
						}
						call.Function.Name += part.Function.Name
						call.Function.Arguments += part.Function.Arguments
						generated = generated || part.Function.Arguments != ""
					}
				default:
					var text string
					if json.Unmarshal(value, &text) == nil {
						previous, _ := message[key].(string)
						message[key] = previous + text
						if key == "content" || key == "reasoning_content" || key == "refusal" {
							generated = generated || text != ""
						}
					} else {
						// Keep vendor continuation fields without allowing them to replace the
						// role or tools assembled above. The raw stream is also retained.
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
	for scanner.Scan() {
		if captured.Len() > 8<<20 {
			return nil, nil, timing, invalid()
		}
		line := scanner.Text()
		if line == "" {
			if err := process(); err != nil {
				return nil, nil, timing, err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if scanner.Err() != nil || captured.Len() > 8<<20 {
		return nil, nil, timing, invalid()
	}
	if err := process(); err != nil {
		return nil, nil, timing, err
	}
	if !done || id == "" || finish == "" || len(usage) == 0 {
		return nil, nil, timing, invalid()
	}
	if len(calls) > 0 {
		ordered := make([]wireCall, len(calls))
		for i := range ordered {
			if calls[i] == nil {
				return nil, nil, timing, invalid()
			}
			ordered[i] = *calls[i]
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
