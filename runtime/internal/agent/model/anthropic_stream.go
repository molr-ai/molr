package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"strings"
	"time"
)

func readProviderStream(protocol Protocol, r io.Reader, started time.Time, now func() time.Time) ([]byte, []byte, *Timing, error) {
	if protocol == OpenAIResponses {
		return readResponsesStream(r, started, now)
	}
	if protocol == AnthropicMessages {
		return readAnthropicStream(r, started, now)
	}
	return readChatStream(protocol, r, started, now)
}
func readAnthropicStream(reader io.Reader, started time.Time, now func() time.Time) (raw, completion []byte, timing *Timing, resultErr error) {
	var captured bytes.Buffer
	scanner := bufio.NewScanner(io.TeeReader(io.LimitReader(reader, (8<<20)+1), &captured))
	scanner.Buffer(make([]byte, 4096), (8<<20)+1)
	timing = &Timing{}
	defer func() {
		raw = append([]byte(nil), captured.Bytes()...)
		timing.RequestMs = max(0, float64(now().Sub(started))/float64(time.Millisecond))
	}()
	invalid := func() error { return core.NewError(core.Unavailable, "invalid or incomplete Anthropic stream") }
	var response anthropicResponse
	var text, arguments, signature strings.Builder
	active := -1
	begun, done, outputReported := false, false, false
	var first, last time.Time
	fragments := 0
	data := []string{}
	process := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		var event struct {
			Type    string          `json:"type"`
			Index   int             `json:"index"`
			Message json.RawMessage `json:"message"`
			Block   anthropicBlock  `json:"content_block"`
			Delta   struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage map[string]json.RawMessage `json:"usage"`
		}
		if done || json.Unmarshal([]byte(payload), &event) != nil {
			return invalid()
		}
		generated := false
		switch event.Type {
		case "ping":
			return nil
		case "error":
			return invalid()
		case "message_start":
			if begun || json.Unmarshal(event.Message, &response) != nil || response.ID == "" || response.Type != "message" || response.Role != Assistant || len(response.Content) != 0 || response.Usage == nil || response.Usage.Input == nil {
				return invalid()
			}
			begun = true
		case "content_block_start":
			if !begun || active != -1 || event.Index != len(response.Content) || event.Index >= 256 {
				return invalid()
			}
			switch event.Block.Type {
			case "text", "tool_use", "thinking", "redacted_thinking":
			default:
				return invalid()
			}
			active = event.Index
			response.Content = append(response.Content, event.Block)
			text.Reset()
			arguments.Reset()
			signature.Reset()
			text.WriteString(event.Block.Text)
			if event.Block.Thinking != nil {
				text.WriteString(*event.Block.Thinking)
			}
			signature.WriteString(event.Block.Signature)
		case "content_block_delta":
			if active < 0 || event.Index != active {
				return invalid()
			}
			block := &response.Content[active]
			switch event.Delta.Type {
			case "text_delta":
				if block.Type != "text" {
					return invalid()
				}
				text.WriteString(event.Delta.Text)
				generated = event.Delta.Text != ""
			case "thinking_delta":
				if block.Type != "thinking" {
					return invalid()
				}
				text.WriteString(event.Delta.Thinking)
				generated = event.Delta.Thinking != ""
			case "signature_delta":
				if block.Type != "thinking" {
					return invalid()
				}
				signature.WriteString(event.Delta.Signature)
			case "input_json_delta":
				if block.Type != "tool_use" {
					return invalid()
				}
				arguments.WriteString(event.Delta.PartialJSON)
				generated = event.Delta.PartialJSON != ""
			default:
				return invalid()
			}
		case "content_block_stop":
			if active < 0 || event.Index != active {
				return invalid()
			}
			block := &response.Content[active]
			switch block.Type {
			case "text":
				block.Text = text.String()
			case "thinking":
				value := text.String()
				block.Thinking = &value
				block.Signature = signature.String()
			case "tool_use":
				if arguments.Len() > 0 {
					block.Input = json.RawMessage(arguments.String())
				}
				if !json.Valid(block.Input) {
					return invalid()
				}
			}
			active = -1
		case "message_delta":
			if !begun || active != -1 {
				return invalid()
			}
			if event.Delta.StopReason != "" {
				response.StopReason = event.Delta.StopReason
			}
			// Usage deltas are cumulative, and may repeat input/cache counts.
			usage, _ := json.Marshal(response.Usage)
			var merged map[string]json.RawMessage
			_ = json.Unmarshal(usage, &merged)
			for key, value := range event.Usage {
				merged[key] = value
				if key == "output_tokens" {
					outputReported = true
				}
			}
			usage, _ = json.Marshal(merged)
			if json.Unmarshal(usage, &response.Usage) != nil {
				return invalid()
			}
		case "message_stop":
			if !begun || active != -1 || response.StopReason == "" || !outputReported {
				return invalid()
			}
			done = true
		default: // Anthropic permits new event types; they cannot establish completion.
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
	if !done {
		return nil, nil, timing, invalid()
	}
	if !first.IsZero() {
		ms := max(0, float64(first.Sub(started))/float64(time.Millisecond))
		timing.FirstTokenMs = &ms
		if fragments > 1 && last.After(first) {
			ms := float64(last.Sub(first)) / float64(time.Millisecond)
			timing.GenerationMs = &ms
		}
	}
	completion, resultErr = json.Marshal(response)
	return
}
