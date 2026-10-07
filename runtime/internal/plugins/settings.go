package plugins

import (
	"context"
	"encoding/json"
	"slices"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type UserSettings struct {
	OwnerID     core.ID `json:"-"`
	PluginID    string
	Digest      string
	Version     core.Version
	Values      map[string]any
	NeedsReview bool
}
type SettingsRequest struct {
	Scope           string
	ID              string
	Digest          string
	ExpectedVersion core.Version
	Values          map[string]any
}

func (s *Service) settings(ctx context.Context, owner core.ID, v Installation) (UserSettings, error) {
	return s.settingsFor(ctx, owner, v, "")
}
func (s *Service) settingsFor(ctx context.Context, owner core.ID, v Installation, scope string) (UserSettings, error) {
	old, err := persistence.Load[UserSettings](ctx, s.store, "plugins.settings", persistence.Key(owner, Key(settingsKey(scope, v.ID))))
	if err != nil && !persistence.Missing(err) {
		return old, err
	}
	out := UserSettings{OwnerID: owner, PluginID: v.ID, Digest: v.Digest, Version: old.Version, Values: map[string]any{}, NeedsReview: old.Digest != "" && old.Digest != v.Digest}
	for name, f := range v.Manifest.Settings {
		if f.Default != nil {
			var value any
			_ = json.Unmarshal(f.Default, &value)
			out.Values[name] = value
		}
		if value, ok := old.Values[name]; ok {
			if f.Type == "connection" {
				if _, ok := value.(string); ok {
					out.Values[name] = value
				}
			} else if validSettingValue(f, value) {
				out.Values[name] = value
			}
		}
	}
	return out, nil
}
func (s *Service) Settings(ctx context.Context, p core.Principal, id, digest string, scopes ...string) (UserSettings, error) {
	scope := ""
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	if err := s.authorize(ctx, p, false); err != nil {
		return UserSettings{}, err
	}
	v, owner, err := s.settingsInstallation(ctx, p, id, digest, scope)
	if err != nil {
		return UserSettings{}, err
	}
	return s.settingsFor(ctx, owner, v, scope)
}
func (s *Service) SaveSettings(ctx context.Context, p core.Principal, q SettingsRequest) (UserSettings, error) {
	var out UserSettings
	if err := s.authorize(ctx, p, false); err != nil {
		return out, err
	}
	if s.options.AuthorizeSettings == nil {
		return out, core.NewError(core.Forbidden, "settings write unavailable")
	}
	if err := s.options.AuthorizeSettings(ctx, p); err != nil {
		return out, err
	}
	raw, err := json.Marshal(q.Values)
	if err != nil || len(raw) > 16<<10 || q.Values == nil {
		return out, invalid("bounded settings object required")
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, owner, err := s.settingsInstallation(ctx, p, q.ID, q.Digest, q.Scope)
		if err != nil {
			return err
		}
		old, err := s.settingsFor(ctx, owner, v, q.Scope)
		if err != nil {
			return err
		}
		if old.Version != q.ExpectedVersion {
			return core.NewError(core.Conflict, "plugin settings changed; reload before saving")
		}
		values := map[string]any{}
		for name, f := range v.Manifest.Settings {
			if f.Default != nil {
				var value any
				_ = json.Unmarshal(f.Default, &value)
				values[name] = value
			}
		}
		for name, value := range q.Values {
			f, ok := v.Manifest.Settings[name]
			if !ok {
				return invalid("unknown setting")
			}
			if f.Type == "connection" {
				id, ok := value.(string)
				if !ok {
					return invalid("connection ID required")
				}
				if id != "" {
					connectionInstallation := v
					if q.Scope == "admin" {
						connectionInstallation.Enabled = true
						connectionInstallation.Grants = append(append([]string{}, v.Grants...), "http.request")
					}
					c, err := s.usableConnection(ctx, p, connectionInstallation, core.ID(id))
					if err != nil {
						return err
					}
					if !slices.Contains(f.Providers, c.ProviderID) {
						return invalid("connection provider does not match setting")
					}
					provider, _ := providerFor(v.Manifest, c.ProviderID)
					for _, op := range f.RequiredOperations {
						if _, ok := provider.Operations[op]; !ok {
							return invalid("connection missing operation")
						}
					}
				}
			} else if !validSettingValue(f, value) {
				return invalid("invalid setting value")
			}
			values[name] = value
		}
		out = UserSettings{OwnerID: owner, PluginID: v.ID, Digest: v.Digest, Version: old.Version + 1, Values: values}
		if err := tx.Put(ctx, "plugins.settings", persistence.Key(owner, Key(settingsKey(q.Scope, v.ID))), out); err != nil {
			return err
		}
		return s.audit(ctx, p, "settings.update", v.ID)
	})
	return out, err
}
func providerFor(m Manifest, id string) (HTTPProvider, bool) {
	for _, p := range m.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return HTTPProvider{}, false
}

func (s *Service) settingsInstallation(ctx context.Context, p core.Principal, id, digest, scope string) (Installation, core.ID, error) {
	if scope != "" && scope != "admin" {
		return Installation{}, "", invalid("invalid settings scope")
	}
	if scope == "" {
		v, err := s.active(ctx, p, id, digest)
		if err == nil && len(v.Manifest.Settings) == 0 && v.Manifest.SettingsPage == nil {
			err = core.NewError(core.NotFound, "plugin has no user settings")
		}
		return v, p.OwnerID, err
	}
	if err := s.authorize(ctx, p, true); err != nil {
		return Installation{}, "", err
	}
	v, err := s.load(ctx, p.OwnerID, id)
	if err != nil {
		return v, "", err
	}
	if v.Deleted || v.Digest != digest {
		return v, "", core.NewError(core.Forbidden, "plugin unavailable")
	}
	if len(v.Manifest.AdminSettings) == 0 && v.Manifest.AdminSettingsPage == nil {
		return v, "", core.NewError(core.NotFound, "plugin has no administrator settings")
	}
	v.Manifest.Settings = v.Manifest.AdminSettings
	return v, v.OwnerID, nil
}

func settingsKey(scope, id string) string {
	if scope == "admin" {
		return "admin:" + id
	}
	return id
}
