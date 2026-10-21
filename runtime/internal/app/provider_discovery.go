package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/modelcatalog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

func (a adapters) modelCatalog() modelcatalog.Service {
	return modelcatalog.Service{Store: a.store, Pricing: a.pricing, Now: a.now, Group: groupPolicy}
}
func (a adapters) provider(ctx context.Context, p core.Principal, id core.ID) (model.Provider, error) {
	return a.modelCatalog().Resolve(ctx, p, id)
}
func (a adapters) models(ctx context.Context, p core.Principal) ([]model.Model, error) {
	return a.modelCatalog().List(ctx, p)
}
func (a adapters) ownerNetworkBinding(ctx context.Context, p core.Principal, id core.ID) (platform.NetworkBinding, error) {
	_, b, err := a.binding(ctx, p, id)
	if err != nil {
		return platform.NetworkBinding{}, err
	}
	if err := a.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Manage}); err != nil {
		return platform.NetworkBinding{}, err
	}
	o, err := origin(b.Endpoint)
	if err != nil {
		return platform.NetworkBinding{}, err
	}
	return platform.NetworkBinding{Origin: o}, nil
}
func fetchProviderModels(ctx context.Context, client *http.Client, endpoint string, secret core.Secret, preset string) ([]resource.ModelConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if preset == "anthropic" {
		return fetchAnthropicModels(ctx, client, endpoint, secret)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, core.Invalid("endpoint", "invalid endpoint")
	}
	u.Path = strings.TrimRight(strings.TrimSuffix(u.Path, "/chat/completions"), "/") + "/models"
	if preset == "siliconflow" {
		q := u.Query()
		q.Set("sub_type", "chat")
		u.RawQuery = q.Encode()
	}
	rows := []resource.ModelConfig{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, core.NewError(core.InvalidArgument, "invalid discovery endpoint")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	if !secret.Empty() {
		req.Header.Set("Authorization", "Bearer "+secret.Reveal())
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, core.Wrap(core.Unavailable, "model discovery failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, core.NewError(core.Unavailable, "provider model list unavailable")
	}
	maxBytes := int64(2 << 20)
	maxModels := 500
	if preset == "openrouter" {
		maxBytes, maxModels = 8<<20, resource.MaxProviderModels
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || int64(len(payload)) > maxBytes {
		return nil, core.NewError(core.Unavailable, "invalid model catalog")
	}
	var data struct {
		Data []struct {
			openRouterModel
			ID                  string   `json:"id"`
			InputModalities     []string `json:"input_modalities"`
			ContextWindow       int      `json:"context_window"`
			MaxModelLen         int      `json:"max_model_len"`
			MaxOutputTokens     int      `json:"max_output_tokens"`
			MaxCompletionTokens int      `json:"max_completion_tokens"`
			TopProvider         struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &data) != nil || data.Data == nil || len(data.Data) > maxModels {
		return nil, core.NewError(core.Unavailable, "invalid model catalog")
	}
	seen := map[string]bool{}
	for _, item := range data.Data {
		name := strings.TrimSpace(item.ID)
		if name == "" || len(name) > 256 {
			return nil, core.NewError(core.Unavailable, "invalid model identifier")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		// Compatible chat defaults are overridable via manual model configuration.
		m := resource.ModelConfig{Name: name, SupportsTools: true, Discovered: true}
		// vLLM and SGLang report their served context window as max_model_len.
		for _, limit := range []int{item.ContextWindow, item.MaxModelLen} {
			if limit >= 256 && limit <= 2_000_000 {
				m.ProviderContextLimit = limit
				break
			}
		}
		for _, limit := range []int{item.MaxOutputTokens, item.MaxCompletionTokens, item.TopProvider.MaxCompletionTokens} {
			if limit > 0 && limit <= model.MaxOutputLimit {
				m.ProviderOutputLimit = limit
				break
			}
		}
		for _, modality := range item.InputModalities {
			if modality == "image" {
				m.SupportsImages = true
			}
			if modality == "audio" {
				m.SupportsAudio = true
			}
		}
		if preset == "openrouter" {
			if !item.openRouterModel.configure(&m) {
				continue
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

func (a adapters) discoverModels(ctx context.Context, p core.Principal, b resource.Binding) ([]resource.ModelConfig, error) {
	o, err := origin(b.Endpoint)
	if err != nil {
		return nil, err
	}
	client := a.network().OwnerHTTPClient(p, b.ResourceID, o)
	defer client.CloseIdleConnections()
	var rows []resource.ModelConfig
	request := func(secret core.Secret) error {
		var err error
		if b.ChatGPT != nil {
			rows, err = fetchChatGPTModels(ctx, client, secret)
		} else {
			rows, err = fetchProviderModels(ctx, client, b.Endpoint, secret, b.PricingProvider)
		}
		return err
	}
	if b.CredentialID != "" {
		if a.broker == nil {
			return nil, core.NewError(core.Unavailable, "credential broker unavailable")
		}
		err = a.broker.WithSecret(ctx, credential.Access{Principal: p, ResourceID: b.ResourceID, CredentialID: b.CredentialID, Origin: o}, request)
	} else {
		err = request(core.Secret{})
	}
	return rows, err
}

func (a adapters) testModelConnection(ctx context.Context, p core.Principal, req resource.TestConnectionRequest) (resource.ConnectionTest, error) {
	o, err := origin(req.Binding.Endpoint)
	if err != nil {
		return resource.ConnectionTest{}, err
	}
	guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: a.privateAddresses(), OwnerBinding: func(ctx context.Context, actor core.Principal, _ core.ID) (platform.NetworkBinding, error) {
		if actor != p || actor.SessionID == "" {
			return platform.NetworkBinding{}, denied()
		}
		if err := a.policy.Require(ctx, actor, policy.CheckRequest{Target: core.Object{Kind: "resource"}, Action: policy.Manage}); err != nil {
			return platform.NetworkBinding{}, err
		}
		return platform.NetworkBinding{Origin: o}, nil
	}})
	client := guard.OwnerHTTPClient(p, "", o)
	defer client.CloseIdleConnections()
	var result resource.ConnectionTest
	request := func(secret core.Secret) error {
		models, err := fetchProviderModels(ctx, client, req.Binding.Endpoint, secret, req.Binding.PricingProvider)
		if err == nil {
			result.ModelCount = len(models)
		}
		return err
	}
	if req.Binding.CredentialID != "" {
		if a.broker == nil {
			return result, core.NewError(core.Unavailable, "credential broker unavailable")
		}
		err = a.broker.WithOwnerSecret(ctx, p, req.Binding.CredentialID, req.Provider, request)
	} else {
		err = request(req.APIKey)
	}
	return result, err
}
