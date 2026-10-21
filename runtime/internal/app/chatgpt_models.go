package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/chatgpt"
	"molr.ai/runtime/internal/core"
	"net/http"
	"slices"
	"strings"
	"time"
)

func fetchChatGPTModels(ctx context.Context, client *http.Client, secret core.Secret) ([]resource.ModelConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chatgpt.Resource+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret.Reveal())
	res, err := client.Do(req)
	if err != nil {
		return nil, core.NewError(core.Unavailable, "ChatGPT model catalog unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, core.NewError(core.Unavailable, "ChatGPT model catalog unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, core.NewError(core.Unavailable, "invalid ChatGPT catalog")
	}
	var catalog struct {
		Models []struct {
			Slug       string   `json:"slug"`
			Visibility string   `json:"visibility"`
			Context    int      `json:"context_window"`
			Modalities []string `json:"input_modalities"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &catalog) != nil || catalog.Models == nil || len(catalog.Models) > 500 {
		return nil, core.NewError(core.Unavailable, "invalid ChatGPT catalog")
	}
	out := []resource.ModelConfig{}
	seen := map[string]bool{}
	for _, m := range catalog.Models {
		if m.Visibility != "list" {
			continue
		}
		if strings.TrimSpace(m.Slug) == "" || len(m.Slug) > 256 {
			return nil, core.NewError(core.Unavailable, "invalid ChatGPT model")
		}
		if seen[m.Slug] {
			continue
		}
		seen[m.Slug] = true
		v := resource.ModelConfig{Name: m.Slug, SupportsTools: true, SupportsImages: slices.Contains(m.Modalities, "image"), Discovered: true}
		if m.Context >= 256 && m.Context <= 2_000_000 {
			v.ProviderContextLimit = m.Context
		}
		out = append(out, v)
	}
	return out, nil
}
