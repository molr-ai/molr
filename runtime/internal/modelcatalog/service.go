// Package modelcatalog resolves configured models, pricing and user-group visibility.
package modelcatalog

import (
	"context"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Service struct {
	Store   persistence.Store
	Pricing *pricing.Catalog
	Now     func() time.Time
	Group   func(context.Context, persistence.Store, core.ID) (auth.UserGroup, error)
}

type Entry struct {
	ID     core.ID
	Config resource.ModelConfig
}

func Entries(r resource.Resource, b resource.Binding) []Entry {
	entries := make([]Entry, 0, len(b.Models))
	for _, m := range b.Models {
		entries = append(entries, Entry{resource.ModelID(r.ID, m.Name), m})
	}
	return entries
}

func tariff(r pricing.Rates, currency string) *model.Pricing {
	return &model.Pricing{Currency: currency, InputMicrosPerMillion: r.Input, OutputMicrosPerMillion: r.Output, CachedInputMicrosPerMillion: r.Cached}
}
func (s Service) Describe(r resource.Resource, b resource.Binding, entry Entry) model.Provider {
	m := entry.Config
	var price, reserve *model.Pricing
	var info *model.PricingInfo
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if q, ok := s.Pricing.Lookup(b.PricingProvider, b.Endpoint, m.Name, now); ok {
		price, reserve = tariff(q.Current, q.Currency), tariff(q.Reserve, q.Currency)
		for _, tier := range q.Tiers {
			price.Tiers = append(price.Tiers, model.PricingTier{InputTokensAbove: tier.InputTokensAbove, OutputTokensAbove: tier.OutputTokensAbove, InputMicrosPerMillion: tier.Rates.Input, OutputMicrosPerMillion: tier.Rates.Output, CachedInputMicrosPerMillion: tier.Rates.Cached})
		}
		info = &model.PricingInfo{Source: "builtin", Automatic: price, RuleID: q.RuleID, Version: q.Version, Period: q.Period, SourceURL: q.SourceURL, CalendarKnown: q.CalendarKnown}
	}
	if b.PricingProvider == "openrouter" && m.ProviderPricing != nil {
		p := m.ProviderPricing
		price = &model.Pricing{Currency: p.Currency, InputMicrosPerMillion: p.InputMicrosPerMillion, OutputMicrosPerMillion: p.OutputMicrosPerMillion, CachedInputMicrosPerMillion: p.CachedInputMicrosPerMillion}
		reserve = price
		info = &model.PricingInfo{Source: "provider", Automatic: price, SourceURL: "https://openrouter.ai/models"}
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
	if b.PricingProvider == "openrouter" {
		protocol = model.OpenRouterChat
	} else if b.PricingProvider == "deepseek" {
		protocol = model.DeepSeekChat
	} else if b.PricingProvider == "anthropic" {
		protocol = model.AnthropicMessages
	} else if b.PricingProvider == "gemini" {
		protocol = model.GeminiChat
	} else if b.PricingProvider == "minimax" {
		protocol = model.MiniMaxChat
	} else if b.PricingProvider == "volcengine" || b.PricingProvider == "siliconflow" || b.PricingProvider == "zai" {
		protocol = model.ReasoningChat
	} else if b.PricingProvider == "" {
		u, err := url.Parse(b.Endpoint)
		if err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.deepseek.com") && (u.Port() == "" || u.Port() == "443") && u.User == nil {
			protocol = model.DeepSeekChat
		}
	}
	if b.ChatGPT != nil {
		protocol = model.OpenAIResponses
		if info == nil {
			info = &model.PricingInfo{Source: "unknown"}
		}
		info.Basis = "subscription-reference"
	}
	manual, declared := m.ContextLimit, m.ProviderContextLimit
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
	return model.Provider{Protocol: protocol, Model: model.Model{ID: entry.ID, ResourceID: r.ID, Name: m.Name, SupportsTools: m.SupportsTools, SupportsImages: m.SupportsImages, SupportsAudio: m.SupportsAudio && protocol != model.OpenAIResponses && protocol != model.DeepSeekChat && protocol != model.AnthropicMessages, ContextLimit: manual, ProviderContextLimit: declared, EffectiveContextLimit: effective, ContextLimitSource: source, OutputLimit: m.OutputLimit, ProviderOutputLimit: m.ProviderOutputLimit, EffectiveOutputLimit: output, OutputLimitSource: outputSource, Pricing: price, PricingInfo: info}, Endpoint: b.Endpoint, CredentialID: b.CredentialID, Pricing: price, ReservePricing: reserve}
}
func (s Service) Resolve(ctx context.Context, p core.Principal, id core.ID) (out model.Provider, err error) {
	err = s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		catalogOwner := catalog.Scope
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
		for _, entry := range Entries(r, binding) {
			if entry.ID != id {
				continue
			}
			group, err := s.Group(ctx, tx.Store(), p.OwnerID)
			if err != nil {
				return err
			}
			allowed := group.Administrator
			for _, ref := range group.Resources {
				allowed = allowed || ref.Kind == "model" && ref.ID == id
			}
			if !allowed {
				return core.WithReason(core.NewError(core.Forbidden, "model unavailable for user group"), "group_resource_denied")
			}
			out = s.Describe(r, binding, entry)
			return nil
		}
		return core.NewError(core.NotFound, "model unavailable")
	})
	return out, err
}

func (s Service) List(ctx context.Context, p core.Principal) ([]model.Model, error) {
	out := []model.Model{}
	err := s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		store := tx.Store()
		catalogOwner := catalog.Scope
		rows, err := persistence.Select[resource.Resource](ctx, store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", catalogOwner), persistence.Eq("Kind", resource.Model), persistence.Eq("State", resource.Available)}})
		if err != nil || len(rows) == 0 {
			return err
		}
		// The group and catalog share this request's snapshot; later requests reload both.
		group, err := s.Group(ctx, store, p.OwnerID)
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
			for _, entry := range Entries(r, binding) {
				if group.Administrator || allowed[entry.ID] {
					out = append(out, s.Describe(r, binding, entry).Model)
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
