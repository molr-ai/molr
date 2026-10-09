package debugtrace

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkTraceSanitization(b *testing.B) {
	for _, count := range []int{50, 500, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			inputs := make([]map[string]any, count)
			for i := range inputs {
				inputs[i] = map[string]any{"Role": "tool", "Text": strings.Repeat("synthetic trace content ", 8), "ToolOperationID": fmt.Sprint(i), "FileIDs": []string{}, "Calls": []any{}, "ReplyTo": fmt.Sprint(i), "ProviderState": map[string]any{"Source": map[string]string{"Protocol": "fixture", "ModelName": "synthetic-model"}, "Payload": json.RawMessage(`{"role":"assistant","reasoning":"fixture reasoning","extension":{"sequence":1}}`)}}
			}
			payload := map[string]any{"Inputs": inputs, "API_Key": "synthetic-sensitive-value", "Metadata": map[string]any{"Attempt": 1, "ModelID": "fixture"}}
			encoded, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(encoded)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				data, status := sanitize(payload, 4<<20)
				if status != "redacted" || len(data) == 0 {
					b.Fatal(status)
				}
			}
		})
	}
}
