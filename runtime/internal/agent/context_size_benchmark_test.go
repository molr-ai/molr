package agent

import (
	"fmt"
	"molr.ai/runtime/internal/agent/model"
	"strings"
	"testing"
)

func BenchmarkContextSizeCheck(b *testing.B) {
	for _, size := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("%dMiB", size), func(b *testing.B) {
			inputs := make([]model.Input, size*64)
			text := strings.Repeat("context ", 2048)
			for i := range inputs {
				inputs[i] = model.Input{Role: model.Tool, Text: text, ReplyTo: fmt.Sprint(i)}
			}
			b.SetBytes(int64(len(inputs) * len(text)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := checkContextSize(inputs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
