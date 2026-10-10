package voicecodec

import (
	"os"
	"testing"
)

func BenchmarkWAVRepeated(b *testing.B) {
	data, err := os.ReadFile("testdata/tone.silk")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := WAV(b.Context(), data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := WAV(b.Context(), data); err != nil {
			b.Fatal(err)
		}
	}
}
