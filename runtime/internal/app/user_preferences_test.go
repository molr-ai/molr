package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestOwnerPreferencesIsolationDefaultsAndUpdates(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	need(t, err)
	defer db.Close()
	repo := settings.NewRepository(db)
	read := func(owner core.ID) (userPreferences, string) {
		t.Helper()
		doc, err := ownerPreferencesContext(t.Context(), db, owner)
		need(t, err)
		var value userPreferences
		need(t, json.Unmarshal([]byte(doc.Content), &value))
		if doc.Kind != "user_preferences" || doc.Source != "runtime/user_preferences" || doc.Scope != "assistant" {
			t.Fatalf("invalid source: %+v", doc)
		}
		if strings.Contains(doc.Content, "private") || strings.Contains(doc.Content, "current_time") || strings.Contains(doc.Content, "Version") {
			t.Fatalf("unrelated data leaked: %s", doc.Content)
		}
		return value, doc.Content
	}
	defaults := userPreferences{Language: "en", Currency: "USD", Sources: preferenceSources{"unknown", "default", "default"}}
	if got, _ := read("missing"); got != defaults {
		t.Fatalf("missing settings = %+v", got)
	}
	pref := settings.Settings{OwnerID: "owner", Timezone: "Asia/Shanghai", Language: "zh", Currency: "CNY", DefaultModelID: "private-model", Version: 1}
	need(t, repo.Save(t.Context(), pref, 0))
	saved := preferenceSources{"user_settings", "user_settings", "user_settings"}
	if got, _ := read("owner"); got != (userPreferences{"Asia/Shanghai", "zh", "CNY", saved}) {
		t.Fatalf("saved preferences = %+v", got)
	}
	need(t, repo.Save(t.Context(), settings.Settings{OwnerID: "other", Timezone: "UTC", Language: "en", Currency: "USD", Version: 1}, 0))
	if got, _ := read("other"); got != (userPreferences{"UTC", "en", "USD", saved}) {
		t.Fatalf("other owner's preferences = %+v", got)
	}
	_, before := read("owner")
	pref.Version++
	pref.Theme = settings.Dark
	need(t, repo.Save(t.Context(), pref, pref.Version-1))
	if _, after := read("owner"); after != before {
		t.Fatal("unrelated settings edit changed context")
	}
	for _, field := range []string{"timezone", "language", "currency"} {
		switch field {
		case "timezone":
			pref.Timezone = "Europe/London"
		case "language":
			pref.Language = "en"
		case "currency":
			pref.Currency = "USD"
		}
		pref.Version++
		need(t, repo.Save(t.Context(), pref, pref.Version-1))
		got, after := read("owner")
		if after == before || got.Timezone != pref.Timezone || got.Language != pref.Language || got.Currency != pref.Currency {
			t.Fatalf("%s edit not reflected: %+v", field, got)
		}
		before = after
	}
	for _, zone := range []string{"", "Local", "Mars/Olympus"} {
		pref.Version++
		pref.Timezone = zone
		pref.Language = ""
		pref.Currency = ""
		need(t, repo.Save(t.Context(), pref, pref.Version-1))
		if got, _ := read("owner"); got != defaults {
			t.Fatalf("invalid/empty preferences = %+v", got)
		}
	}
	need(t, db.Close())
	if _, err := ownerPreferencesContext(t.Context(), db, "owner"); err == nil {
		t.Fatal("storage failure disguised as defaults")
	}
}
