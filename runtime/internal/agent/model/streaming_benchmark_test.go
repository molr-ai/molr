package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func fragmentedStream(count int, field string) (string, string) {
	var body strings.Builder
	emit := func(delta any, finish string) {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		raw, _ := json.Marshal(map[string]any{"id": "fragmented", "choices": []any{choice}})
		body.WriteString("data: ")
		body.Write(raw)
		body.WriteString("\n\n")
	}
	part := strings.Repeat("abcdefgh", 8)
	if field == "tools" {
		emit(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "publish", "arguments": "{\"text\":\""}}}}, "")
	}
	for i := 0; i < count; i++ {
		if field == "tools" {
			emit(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]string{"arguments": part}}}}, "")
		} else {
			emit(map[string]string{field: part}, "")
		}
	}
	finish := "stop"
	if field == "tools" {
		emit(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]string{"arguments": "\"}"}}}}, "")
		finish = "tool_calls"
	}
	emit(map[string]string{}, finish)
	body.WriteString("data: {\"id\":\"fragmented\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":100}}\n\ndata: [DONE]\n\n")
	return body.String(), strings.Repeat(part, count)
}

func BenchmarkChatStreamAssembly(b *testing.B) {
	for _, field := range []string{"content", "reasoning_content", "tools"} {
		for _, count := range []int{256, 1024, 4096} {
			b.Run(fmt.Sprintf("%s/%d", field, count), func(b *testing.B) {
				body, _ := fragmentedStream(count, field)
				start := time.Unix(1, 0)
				now := func() time.Time { return start }
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				b.ResetTimer()
				for b.Loop() {
					raw, completion, _, err := readChatStream(strings.NewReader(body), start, now)
					if err != nil || len(raw) != len(body) || len(completion) == 0 {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
