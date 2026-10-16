package app

import (
	"encoding/json"
	"math/big"
	"slices"

	"molr.ai/runtime/internal/business/resource"
)

// OpenRouter's catalog uses nested modalities and USD per-token decimal strings.
// Keep discovered prices separate from deliberate owner overrides.
type openRouterModel struct {
	ContextLength int `json:"context_length"`
	Architecture  struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string        `json:"supported_parameters"`
	Prices              json.RawMessage `json:"pricing"`
}

func (item openRouterModel) configure(m *resource.ModelConfig) bool {
	// Molr consumes text completions; embedding/image-only models are not agents.
	if !slices.Contains(item.Architecture.Output, "text") {
		return false
	}
	m.ProviderContextLimit = 0
	if item.ContextLength >= 256 && item.ContextLength <= 2_000_000 {
		m.ProviderContextLimit = item.ContextLength
	}
	m.SupportsTools = slices.Contains(item.SupportedParameters, "tools")
	m.SupportsImages = slices.Contains(item.Architecture.Input, "image")
	m.SupportsAudio = slices.Contains(item.Architecture.Input, "audio")
	var prices map[string]string
	// Conditional/structured tariffs cannot be represented by a flat ledger.
	// Decode per model so they never invalidate the entire provider catalog.
	if json.Unmarshal(item.Prices, &prices) == nil {
		m.ProviderPricing = openRouterPricing(prices)
	}
	return true
}

func openRouterPricing(prices map[string]string) *resource.ModelPricing {
	input, ok := openRouterRate(prices["prompt"])
	if !ok {
		return nil
	}
	output, ok := openRouterRate(prices["completion"])
	if !ok {
		return nil
	}
	cached := input // Absent cache discount means normal input pricing, never free.
	if value, exists := prices["input_cache_read"]; exists {
		cached, ok = openRouterRate(value)
		if !ok || cached > input {
			return nil
		}
	}
	// The current ledger models input/output/cache tokens only. Do not advertise
	// a complete tariff when the provider declares additional billable dimensions.
	for key, value := range prices {
		if key == "prompt" || key == "completion" || key == "input_cache_read" {
			continue
		}
		rate, ok := openRouterRate(value)
		if !ok || rate != 0 {
			return nil
		}
	}
	return &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: input, OutputMicrosPerMillion: output, CachedInputMicrosPerMillion: cached}
}

func openRouterRate(value string) (int64, bool) {
	if len(value) == 0 || len(value) > 64 {
		return 0, false
	}
	// Restrict exponent/decimal input before big.Rat parsing to bound allocations.
	for _, c := range value {
		if (c < '0' || c > '9') && c != '.' {
			return 0, false
		}
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() < 0 {
		return 0, false
	}
	r.Mul(r, big.NewRat(1_000_000_000_000, 1))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	if rem.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64(), q.IsInt64()
}
