package core

import "testing"

func TestCompressionTokens(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		window, configured, want int
	}{
		{"large default", 1024 * 1024, 0, 128 * 1024},
		{"medium default", 128 * 1024, 0, 104857},
		{"small fixed", 32 * 1024, 128 * 1024, 26214},
		{"lower boundary", 256 * 1024, 64 * 1024, 64 * 1024},
		{"upper boundary", 256 * 1024, 209715, 209715},
		{"model shrinks", 128 * 1024, 192 * 1024, 104857},
		{"model grows", 256 * 1024, 26214, 64 * 1024},
		{"fractional upper", 81921, 128 * 1024, 65536},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompressionTokens(tc.window, tc.configured); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
