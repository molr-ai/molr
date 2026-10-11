package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
)

type modelEntry struct {
	id     core.ID
	config resource.ModelConfig
}

func modelEntries(r resource.Resource, b resource.Binding) []modelEntry {
	entries := []modelEntry{}
	for _, m := range b.Models {
		entries = append(entries, modelEntry{resource.ModelID(r.ID, m.Name), m})
	}
	return entries
}
func tariff(r pricing.Rates) *model.Pricing {
	return &model.Pricing{Currency: "USD", InputMicrosPerMillion: r.Input, OutputMicrosPerMillion: r.Output, CachedInputMicrosPerMillion: r.Cached}
}
func (a adapters) resolvedModel(r resource.Resource, b resource.Binding, entry modelEntry) model.Provider {
	m := entry.config
	var price, reserve *model.Pricing
	var info *model.PricingInfo
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	if q, ok := a.pricing.Lookup(b.PricingProvider, b.Endpoint, m.Name, now); ok {
		price, reserve = tariff(q.Current), tariff(q.Reserve)
		info = &model.PricingInfo{Source: "builtin", Automatic: price, RuleID: q.RuleID, Version: q.Version, Period: q.Period, SourceURL: q.SourceURL, CalendarKnown: q.CalendarKnown}
	}
	if m.Pricing != nil {
		price = &model.Pricing{AudioInputMicrosPerMillion: m.Pricing.AudioInputMicrosPerMillion, Currency: m.Pricing.Currency, InputMicrosPerMillion: m.Pricing.InputMicrosPerMillion, OutputMicrosPerMillion: m.Pricing.OutputMicrosPerMillion, CachedInputMicrosPerMillion: m.Pricing.CachedInputMicrosPerMillion}
		reserve = price
		if info == nil {
			info = &model.PricingInfo{}
		}
		info.Source = "manual"
	}
	// PricingProvider is the persisted provider preset selected by the UI.
	// Explicit presets take priority, including custom OpenAI-compatible servers.
	// When unspecified, recognize only the exact official HTTPS origin; model
	// names and proxy hostnames cannot reliably identify a protocol.
	protocol := model.OpenAIChat
	if b.PricingProvider == "deepseek" {
		protocol = model.DeepSeekChat
	} else if b.PricingProvider == "" {
		u, err := url.Parse(b.Endpoint)
		if err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.deepseek.com") && (u.Port() == "" || u.Port() == "443") && u.User == nil {
			protocol = model.DeepSeekChat
		}
	}
	manual, declared := m.ContextLimit, m.ProviderContextLimit
	// Legacy discovery wrote a guessed 32K value. It is not a user override.
	if m.ContextVersion == 0 && m.Discovered {
		manual = 0
		if m.ContextLimit != 32768 {
			declared = m.ContextLimit
		}
	}
	effective, source := model.DefaultContextLimit, "fallback"
	if declared > 0 {
		effective, source = declared, "provider"
	}
	if manual > 0 {
		effective, source = manual, "manual"
	}
	output, outputSource := model.DefaultOutputLimit, "fallback"
	if m.ProviderOutputLimit > 0 {
		output, outputSource = m.ProviderOutputLimit, "provider"
	}
	if m.OutputLimit > 0 {
		output, outputSource = m.OutputLimit, "manual"
	}
	return model.Provider{Protocol: protocol, Model: model.Model{ID: entry.id, ResourceID: r.ID, Name: m.Name, SupportsTools: m.SupportsTools, SupportsImages: m.SupportsImages, SupportsAudio: m.SupportsAudio && protocol == model.OpenAIChat, ContextLimit: manual, ProviderContextLimit: declared, EffectiveContextLimit: effective, ContextLimitSource: source, OutputLimit: m.OutputLimit, ProviderOutputLimit: m.ProviderOutputLimit, EffectiveOutputLimit: output, OutputLimitSource: outputSource, Pricing: price, PricingInfo: info}, Endpoint: b.Endpoint, CredentialID: b.CredentialID, Pricing: price, ReservePricing: reserve}
}
func (a adapters) provider(ctx context.Context, p core.Principal, id core.ID) (out model.Provider, err error) {
	err = a.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		catalogOwner, err := persistence.CatalogOwner(ctx, tx.Store(), p.OwnerID)
		if err != nil {
			return err
		}
		repo := resource.NewRepository(tx.Store())
		binding, err := repo.BindingForModel(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		r, err := repo.Get(ctx, p.OwnerID, binding.ResourceID)
		if err != nil {
			return err
		}
		if r.OwnerID != catalogOwner || r.Kind != resource.Model || r.Provider != "openai-compatible" || r.State != resource.Available {
			return core.NewError(core.NotFound, "model unavailable")
		}
		for _, entry := range modelEntries(r, binding) {
			if entry.id != id {
				continue
			}
			if _, err := groupReference(ctx, tx.Store(), p.OwnerID, "model", id); err != nil {
				return err
			}
			out = a.resolvedModel(r, binding, entry)
			return nil
		}
		return core.NewError(core.NotFound, "model unavailable")
	})
	return out, err
}

func (a adapters) models(ctx context.Context, p core.Principal) ([]model.Model, error) {
	out := []model.Model{}
	err := a.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		store := tx.Store()
		catalogOwner, err := persistence.CatalogOwner(ctx, store, p.OwnerID)
		if err != nil {
			return err
		}
		rows, err := persistence.Select[resource.Resource](ctx, store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", catalogOwner), persistence.Eq("Kind", resource.Model), persistence.Eq("State", resource.Available)}})
		if err != nil || len(rows) == 0 {
			return err
		}
		// The group and catalog share this request's snapshot; later requests reload both.
		group, err := groupPolicy(ctx, store, p.OwnerID)
		if err != nil {
			return err
		}
		allowed := make(map[core.ID]bool)
		if !group.Administrator {
			for _, ref := range group.Resources {
				if ref.Kind == "model" {
					allowed[ref.ID] = true
				}
			}
		}
		repo := resource.NewRepository(store)
		for _, r := range rows {
			if r.OwnerID != catalogOwner || r.Kind != resource.Model || r.State != resource.Available {
				continue
			}
			binding, err := repo.Binding(ctx, p.OwnerID, r.ID)
			if err != nil {
				return err
			}
			for _, entry := range modelEntries(r, binding) {
				if group.Administrator || allowed[entry.id] {
					out = append(out, a.resolvedModel(r, binding, entry).Model)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
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
func fetchProviderModels(ctx context.Context, client *http.Client, endpoint string, secret core.Secret) ([]resource.ModelConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, core.Invalid("endpoint", "invalid endpoint")
	}
	u.Path = strings.TrimRight(strings.TrimSuffix(u.Path, "/chat/completions"), "/") + "/models"
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
	payload, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(payload) > 2<<20 {
		return nil, core.NewError(core.Unavailable, "invalid model catalog")
	}
	var data struct {
		Data []struct {
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
	if json.Unmarshal(payload, &data) != nil || data.Data == nil || len(data.Data) > 500 {
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
		m := resource.ModelConfig{Name: name, ContextVersion: 1, SupportsTools: true, Discovered: true}
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
		rows, err = fetchProviderModels(ctx, client, b.Endpoint, secret)
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
		models, err := fetchProviderModels(ctx, client, req.Binding.Endpoint, secret)
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
