package model

import "testing"

func TestModelContextWindowPrecedence(t *testing.T) {
	for _, tc := range []struct {
		model Model
		want  int
	}{
		{Model{}, 262144}, {Model{ProviderContextLimit: 128000}, 128000}, {Model{ContextLimit: 64000, ProviderContextLimit: 128000}, 64000}, {Model{EffectiveContextLimit: 32000, ContextLimit: 64000}, 32000},
	} {
		if got := tc.model.ContextWindow(); got != tc.want {
			t.Fatalf("%+v = %d, want %d", tc.model, got, tc.want)
		}
	}
}

func TestModelOutputWindowPrecedence(t *testing.T) {
	for _, tc := range []struct {
		model Model
		want  int
	}{
		{Model{}, 32768},
		{Model{ProviderOutputLimit: 8192}, 8192},
		{Model{OutputLimit: 4096, ProviderOutputLimit: 8192}, 4096},
		{Model{EffectiveOutputLimit: 1024, OutputLimit: 4096}, 1024},
	} {
		if got := tc.model.OutputWindow(); got != tc.want {
			t.Fatalf("%+v = %d, want %d", tc.model, got, tc.want)
		}
	}
}

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
