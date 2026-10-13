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
