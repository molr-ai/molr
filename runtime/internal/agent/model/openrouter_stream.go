package model

import (
	"bytes"
	"encoding/json"
	"strings"

	"molr.ai/runtime/internal/core"
)

type reasoningBlock struct {
	fields map[string]json.RawMessage
	text   map[string]*strings.Builder
}
type openRouterReasoningStream struct{ blocks []*reasoningBlock }

func (s *openRouterReasoningStream) append(data []byte) error {
	var parts []map[string]json.RawMessage
	invalid := func() error { return core.NewError(core.Unavailable, "invalid OpenRouter reasoning stream") }
	if json.Unmarshal(data, &parts) != nil {
		return invalid()
	}
	for _, part := range parts {
		if part == nil {
			return invalid()
		}
		var kind string
		if json.Unmarshal(part["type"], &kind) != nil {
			return invalid()
		}
		var block *reasoningBlock
		// Merge consecutive text/summary deltas only. Encrypted entries are
		// independent opaque blobs. Some upstreams reuse index=0 across types.
		if len(s.blocks) > 0 && (kind == "reasoning.text" || kind == "reasoning.summary") {
			last := s.blocks[len(s.blocks)-1]
			matches := true
			for _, key := range []string{"type", "id", "index", "format"} {
				a, b := last.fields[key], part[key]
				if len(a) > 0 && len(b) > 0 && string(a) != "null" && string(b) != "null" && !bytes.Equal(a, b) {
					matches = false
				}
			}
			if matches {
				block = last
			}
		}
		if block == nil {
			block = &reasoningBlock{fields: map[string]json.RawMessage{}, text: map[string]*strings.Builder{}}
			s.blocks = append(s.blocks, block)
		}
		for key, value := range part {
			if string(value) == "null" {
				if _, ok := block.fields[key]; !ok {
					block.fields[key] = value
				}
				continue
			}
			if key == "text" || key == "summary" || key == "signature" {
				var text string
				if json.Unmarshal(value, &text) != nil {
					return invalid()
				}
				if block.text[key] == nil {
					block.text[key] = &strings.Builder{}
				}
				block.text[key].WriteString(text)
			} else {
				block.fields[key] = value
			}
		}
	}
	return nil
}

func (s *openRouterReasoningStream) value() []map[string]json.RawMessage {
	result := make([]map[string]json.RawMessage, 0, len(s.blocks))
	for _, block := range s.blocks {
		for key, text := range block.text {
			block.fields[key], _ = json.Marshal(text.String())
		}
		result = append(result, block.fields)
	}
	return result
}
