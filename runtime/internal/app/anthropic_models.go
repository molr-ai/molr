package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"net/http"
	"net/url"
	"strings"
)

func fetchAnthropicModels(ctx context.Context, client *http.Client, endpoint string, secret core.Secret) ([]resource.ModelConfig, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, core.Invalid("endpoint", "invalid endpoint")
	}
	u.Path = strings.TrimRight(strings.TrimSuffix(u.Path, "/messages"), "/") + "/models"
	rows := []resource.ModelConfig{}
	cursors := map[string]bool{}
	seen := map[string]bool{}
	remaining := int64(2 << 20)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", buildinfo.UserAgent())
		model.SetProviderHeaders(req.Header, model.AnthropicMessages, secret)
		response, err := client.Do(req)
		if err != nil {
			return nil, core.Wrap(core.Unavailable, "model discovery failed", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, remaining+1))
		response.Body.Close()
		remaining -= int64(len(payload))
		if readErr != nil || remaining < 0 || response.StatusCode != 200 {
			return nil, core.NewError(core.Unavailable, "provider model list unavailable")
		}
		var page struct {
			Data []struct {
				ID        string `json:"id"`
				MaxInput  int    `json:"max_input_tokens"`
				MaxOutput int    `json:"max_tokens"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(payload, &page) != nil || page.Data == nil || len(rows)+len(page.Data) > 500 {
			return nil, core.NewError(core.Unavailable, "invalid model catalog")
		}
		for _, item := range page.Data {
			if strings.TrimSpace(item.ID) == "" || len(item.ID) > 256 || seen[item.ID] {
				return nil, core.NewError(core.Unavailable, "invalid model identifier")
			}
			seen[item.ID] = true
			m := resource.ModelConfig{Name: item.ID, ContextVersion: 1, SupportsTools: true, SupportsImages: true, Discovered: true}
			if item.MaxInput >= 256 && item.MaxInput <= 2_000_000 {
				m.ProviderContextLimit = item.MaxInput
			}
			if item.MaxOutput > 0 && item.MaxOutput <= model.MaxOutputLimit {
				m.ProviderOutputLimit = item.MaxOutput
			}
			rows = append(rows, m)
		}
		if !page.HasMore {
			return rows, nil
		}
		if len(page.Data) == 0 || page.LastID == "" || cursors[page.LastID] || len(cursors) >= 20 {
			return nil, core.NewError(core.Unavailable, "invalid model catalog pagination")
		}
		cursors[page.LastID] = true
		query := u.Query()
		query.Set("after_id", page.LastID)
		u.RawQuery = query.Encode()
	}
}
