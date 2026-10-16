package app

import (
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/pricing"
	"testing"
)

func TestProviderPresetSelectsProtocolIndependentlyOfPriceAndAddress(t *testing.T) {
	catalog, err := pricing.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a := adapters{pricing: catalog}
	for _, tc := range []struct {
		preset   string
		protocol model.Protocol
	}{
		{"openrouter", model.OpenRouterChat}, {"deepseek", model.DeepSeekChat}, {"openai", model.OpenAIChat}, {"bailian", model.OpenAIChat}, {"zhipu", model.OpenAIChat}, {"moonshot", model.OpenAIChat}, {"ollama", model.OpenAIChat}, {"vllm", model.OpenAIChat}, {"sglang", model.OpenAIChat}, {"custom", model.OpenAIChat}, {"", model.OpenAIChat},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			binding := resource.Binding{PricingProvider: tc.preset, Endpoint: "https://proxy.example/v1/chat/completions"}
			entry := modelEntry{id: "model"}
			entry.config.Name = "custom-name"
			provider := a.resolvedModel(resource.Resource{ID: "provider"}, binding, entry)
			if provider.Protocol != tc.protocol {
				t.Fatalf("protocol=%s want %s", provider.Protocol, tc.protocol)
			}
		})
	}
}

func TestUnspecifiedProviderProtocolRecognizesOnlyOfficialDeepSeekOrigin(t *testing.T) {
	catalog, err := pricing.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a := adapters{pricing: catalog}
	for _, tc := range []struct {
		endpoint, preset string
		want             model.Protocol
	}{
		{"https://api.deepseek.com/v1/chat/completions", "", model.DeepSeekChat},
		{"https://api.deepseek.com/chat/completions", "", model.DeepSeekChat},
		{"https://api.deepseek.com:443/v1/chat/completions", "", model.DeepSeekChat},
		{"https://api.deepseek.com/v1/chat/completions", "custom", model.OpenAIChat},
		{"https://api.deepseek.com/v1/chat/completions", "openai", model.OpenAIChat},
		{"https://api.deepseek.com.evil.example/v1/chat/completions", "", model.OpenAIChat},
		{"http://api.deepseek.com/v1/chat/completions", "", model.OpenAIChat},
		{"https://api.deepseek.com:8443/v1/chat/completions", "", model.OpenAIChat},
		{"https://user@api.deepseek.com/v1/chat/completions", "", model.OpenAIChat},
		{"https://proxy.example/v1/chat/completions", "", model.OpenAIChat},
	} {
		t.Run(tc.endpoint+"/"+tc.preset, func(t *testing.T) {
			p := a.resolvedModel(resource.Resource{ID: "provider"}, resource.Binding{PricingProvider: tc.preset, Endpoint: tc.endpoint}, modelEntry{id: "model"})
			if p.Protocol != tc.want {
				t.Fatalf("protocol=%s want %s", p.Protocol, tc.want)
			}
		})
	}
}
