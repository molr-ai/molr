package plugins

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type Preference struct {
	Enabled bool
	Version core.Version
}
type PreferenceRequest struct {
	ID              string
	Digest          string
	Enabled         bool
	ExpectedVersion core.Version
}

func (s *Service) checkPlugin(ctx context.Context, p core.Principal, id string) error {
	if s.options.CheckPlugin != nil {
		return s.options.CheckPlugin(ctx, p, id)
	}
	return nil
}
func (s *Service) preference(ctx context.Context, owner core.ID, id string) (Preference, error) {
	v, err := persistence.Load[Preference](ctx, s.store, "plugins.preferences", persistence.Key(owner, Key(id)))
	if persistence.Missing(err) {
		return Preference{Enabled: true}, nil
	}
	return v, err
}
func (s *Service) available(ctx context.Context, p core.Principal, v Installation) error {
	if err := s.checkPlugin(ctx, p, v.ID); err != nil {
		return err
	}
	pref, err := s.preference(ctx, p.OwnerID, v.ID)
	if err != nil {
		return err
	}
	if !pref.Enabled {
		return core.NewError(core.Forbidden, "plugin disabled for this account")
	}
	return nil
}

// SavePreference controls only this account's use; shared installation and group grants remain administrative.
func (s *Service) SavePreference(ctx context.Context, p core.Principal, q PreferenceRequest) (Preference, error) {
	var out Preference
	if err := s.authorize(ctx, p, false); err != nil {
		return out, err
	}
	if s.options.AuthorizeSettings == nil {
		return out, core.NewError(core.Forbidden, "settings write unavailable")
	}
	if err := s.options.AuthorizeSettings(ctx, p); err != nil {
		return out, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, err := s.load(ctx, p.OwnerID, q.ID)
		if err != nil {
			return err
		}
		if v.Deleted || !v.Enabled || v.Digest != q.Digest {
			return core.NewError(core.Forbidden, "plugin unavailable")
		}
		if err := s.checkPlugin(ctx, p, q.ID); err != nil {
			return err
		}
		old, err := s.preference(ctx, p.OwnerID, q.ID)
		if err != nil {
			return err
		}
		if old.Version != q.ExpectedVersion {
			return core.NewError(core.Conflict, "plugin preference changed")
		}
		out = Preference{Enabled: q.Enabled, Version: old.Version + 1}
		if err := tx.Put(ctx, "plugins.preferences", persistence.Key(p.OwnerID, Key(q.ID)), out); err != nil {
			return err
		}
		return s.audit(ctx, p, "preference.update", q.ID)
	})
	if err == nil && !q.Enabled {
		s.cancelLocked(p.OwnerID, q.ID)
		for _, r := range s.httpRunning {
			if r.OwnerID == p.OwnerID && r.PluginID == q.ID {
				r.Cancel()
			}
		}
	}
	return out, err
}
