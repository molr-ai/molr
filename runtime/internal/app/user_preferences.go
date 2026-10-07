package app

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type userPreferences struct {
	Timezone string            `json:"timezone,omitempty"`
	Language string            `json:"language"`
	Currency string            `json:"currency"`
	Sources  preferenceSources `json:"sources"`
}
type preferenceSources struct {
	Timezone string `json:"timezone"`
	Language string `json:"language"`
	Currency string `json:"currency"`
}

// Only these three owner preferences cross the model boundary. Do not include
// settings.Version or a clock: unrelated settings edits and elapsed time must
// not change this source. Missing timezone is unknown, not the UI's UTC fallback.
func ownerPreferencesContext(ctx context.Context, store persistence.Store, owner core.ID) (agent.ContextDocument, error) {
	preference, err := settings.NewRepository(store).Get(ctx, owner)
	if err != nil && !persistence.Missing(err) {
		return agent.ContextDocument{}, err
	}
	value := userPreferences{Language: "en", Currency: "USD", Sources: preferenceSources{Timezone: "unknown", Language: "default", Currency: "default"}}
	if err == nil {
		if preference.Timezone != "" && preference.Timezone != "Local" {
			if _, loadErr := time.LoadLocation(preference.Timezone); loadErr == nil {
				value.Timezone, value.Sources.Timezone = preference.Timezone, "user_settings"
			}
		}
		if preference.Language != "" {
			value.Language, value.Sources.Language = preference.Language, "user_settings"
		}
		if preference.Currency == "USD" || preference.Currency == "CNY" {
			value.Currency, value.Sources.Currency = preference.Currency, "user_settings"
		}
	}
	data, err := json.Marshal(value)
	return agent.ContextDocument{Kind: "user_preferences", Source: "runtime/user_preferences", Scope: "assistant", Content: string(data)}, err
}
