package app

import (
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/business/resource"
	"testing"
)

func TestContextLimitSourcesAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		config                      resource.ModelConfig
		manual, provider, effective int
		source                      string
	}{
		{"unset", resource.ModelConfig{}, 0, 0, 262144, "fallback"},
		{"provider", resource.ModelConfig{ProviderContextLimit: 128000, ContextVersion: 1, Discovered: true}, 0, 128000, 128000, "provider"},
		{"override", resource.ModelConfig{ContextLimit: 64000, ProviderContextLimit: 128000, ContextVersion: 1}, 64000, 128000, 64000, "manual"},
		{"legacy guessed discovery", resource.ModelConfig{ContextLimit: 32768, Discovered: true}, 0, 0, 262144, "fallback"},
		{"legacy provider", resource.ModelConfig{ContextLimit: 65536, Discovered: true}, 0, 65536, 65536, "provider"},
		{"legacy manual", resource.ModelConfig{ContextLimit: 32768}, 32768, 0, 32768, "manual"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := (adapters{}).resolvedModel(resource.Resource{}, resource.Binding{}, modelEntry{config: tc.config}).Model
			if m.ContextLimit != tc.manual || m.ProviderContextLimit != tc.provider || m.ContextWindow() != tc.effective || m.ContextLimitSource != tc.source {
				t.Fatalf("wrong resolution: %+v", m)
			}
		})
	}
	p := model.Provider{Model: model.Model{}, Pricing: &model.Pricing{Currency: "USD", InputMicrosPerMillion: 1000000}}
	money, err := estimateProvider(p, 0)
	need(t, err)
	if money.Micros != 262144 {
		t.Fatal("budget reserve ignored fallback", money)
	}
}
