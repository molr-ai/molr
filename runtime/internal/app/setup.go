package app

import (
	"bytes"
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type SetupState struct {
	Required    bool
	AssistantID core.ID
}
type SetupRequest struct {
	ModelID  core.ID
	Language string
	Timezone string
	Budget   core.AssistantBudget
	Pricing  *resource.ModelPricing
}

// InitializeSetup is local CLI only. Account and setup state commit together.
func (b *Bundle) InitializeSetup(ctx context.Context, req auth.InitializeRequest) (auth.Account, error) {
	var account auth.Account
	err := b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		account, err = b.Bootstrap.Initialize(ctx, req)
		if err != nil {
			return err
		}
		return tx.Put(ctx, "system.setup", string(account.ID), SetupState{Required: true})
	})
	return account, err
}
func (b *Bundle) setup(ctx context.Context, p core.Principal, raw json.RawMessage) (any, error) {
	manager := b.Services.Auth.(*auth.Manager)
	if err := manager.RequireAdministrator(ctx, p); err != nil {
		return nil, err
	}
	var result SetupState
	err := b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		err := tx.Get(ctx, "system.setup", string(p.OwnerID), &result)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(raw) == 0 || !result.Required {
			return nil
		}
		var req SetupRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			return core.Invalid("setup", "invalid request")
		}
		if req.Language != "zh" && req.Language != "en" {
			return core.Invalid("language", "zh or en required")
		}
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			return core.Invalid("timezone", "valid timezone required")
		}
		if err := req.Budget.Validate(); err != nil {
			return err
		}
		models, err := b.Services.Models.List(ctx, p)
		if err != nil {
			return err
		}
		found := false
		for _, m := range models {
			if m.ID != req.ModelID {
				continue
			}
			found = true
			if !m.SupportsTools {
				return core.Invalid("model", "tool calling model required")
			}
			if m.Pricing == nil {
				if req.Pricing == nil {
					return core.Invalid("pricing", "model pricing required")
				}
				r, err := b.Services.Resources.Get(ctx, p, m.ResourceID)
				if err != nil {
					return err
				}
				binding, err := resource.NewRepository(b.store).Binding(ctx, p.OwnerID, m.ResourceID)
				if err != nil {
					return err
				}
				for _, config := range binding.Models {
					if config.Name == m.Name {
						config.Pricing = req.Pricing
						config.Discovered = false
						if _, err := b.Services.Resources.ConfigureModel(ctx, p, resource.ConfigureModelRequest{ID: r.ID, Model: config, Mutation: core.Mutation{IdempotencyKey: "setup-model-price", ExpectedVersion: r.Version}}); err != nil {
							return err
						}
					}
				}
			}
		}
		if !found {
			return core.Invalid("model", "available model required")
		}
		prefs, err := b.Services.Settings.Get(ctx, p)
		if err != nil {
			return err
		}
		prefs.Language = req.Language
		prefs.Timezone = req.Timezone
		prefs.DefaultModelID = req.ModelID
		if _, err = b.Services.Settings.Update(ctx, p, settings.UpdateRequest{Settings: prefs, Mutation: core.Mutation{IdempotencyKey: "setup-preferences", ExpectedVersion: prefs.Version}}); err != nil {
			return err
		}
		a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Name: "Molr", ModelID: req.ModelID, Budget: req.Budget, Mutation: core.Mutation{IdempotencyKey: "setup-default-assistant"}})
		if err != nil {
			return err
		}
		result = SetupState{AssistantID: a.ID}
		return tx.Put(ctx, "system.setup", string(p.OwnerID), result)
	})
	return result, err
}
