package plugins

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"sort"
	"strings"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
)

type Connection struct {
	ID             core.ID
	OwnerID        core.ID
	PluginID       string
	Digest         string
	ProviderID     string
	Name           string
	Enabled        bool
	Deleted        bool
	Version        core.Version
	CredentialID   core.ID `json:",omitempty"`
	CallLimit      int64
	Concurrent     int
	TimeoutSeconds int
}
type ConnectionView struct {
	ID             core.ID
	PluginID       string
	ProviderID     string
	Name           string
	Enabled        bool
	Version        core.Version
	Configured     bool
	NeedsReview    bool
	Operations     []string
	CallLimit      int64
	Concurrent     int
	TimeoutSeconds int
	Calls          int64
	Active         int
}
type SaveConnection struct {
	PluginID       string
	Digest         string
	ID             core.ID
	Name           string
	ProviderID     string
	Enabled        bool
	APIKey         core.Secret
	Verification   auth.Verification
	Mutation       core.Mutation
	CallLimit      int64
	Concurrent     int
	TimeoutSeconds int
}
type ConnectionRequest struct {
	PluginID     string
	Digest       string
	ID           core.ID
	Mutation     core.Mutation
	Verification auth.Verification
}

func (s *Service) connection(ctx context.Context, owner, id core.ID) (Connection, error) {
	return catalog.Load[Connection](ctx, s.store, "plugins.connections", id)
}
func (s *Service) checkConnection(ctx context.Context, p core.Principal, c Connection) error {
	if s.options.CheckConnection == nil {
		return core.NewError(core.Forbidden, "connection authorization unavailable")
	}
	return s.options.CheckConnection(ctx, p, c.ID)
}
func (s *Service) usableConnection(ctx context.Context, p core.Principal, v Installation, id core.ID) (Connection, error) {
	c, err := s.connection(ctx, p.OwnerID, id)
	if err != nil {
		return c, err
	}
	if c.Deleted || !c.Enabled || c.PluginID != v.ID || c.Digest != v.Digest || v.Deleted || !v.Enabled || !granted(v, []string{"http.request"}) {
		return c, core.NewError(core.Forbidden, "plugin connection unavailable or requires review")
	}
	if _, ok := providerFor(v.Manifest, c.ProviderID); !ok {
		return c, invalid("unknown connection provider")
	}
	r, err := resource.NewRepository(s.store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return c, err
	}
	if r.Kind != resource.Connection || r.State != resource.Available {
		return c, core.NewError(core.Forbidden, "connection resource unavailable")
	}
	return c, s.checkConnection(ctx, p, c)
}

// ConnectionAccess is shared by the network guard and credential broker.
func (s *Service) ConnectionAccess(ctx context.Context, p core.Principal, id core.ID) (Connection, HTTPProvider, error) {
	c, err := s.connection(ctx, p.OwnerID, id)
	if err != nil {
		return c, HTTPProvider{}, err
	}
	v, err := s.active(ctx, p, c.PluginID, c.Digest)
	if err != nil {
		return c, HTTPProvider{}, err
	}
	c, err = s.usableConnection(ctx, p, v, id)
	provider, _ := providerFor(v.Manifest, c.ProviderID)
	return c, provider, err
}
func (s *Service) Connections(ctx context.Context, p core.Principal, id, digest string) ([]ConnectionView, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	v, err := s.load(ctx, p.OwnerID, id)
	if err != nil {
		return nil, err
	}
	if v.Deleted || v.Digest != digest {
		return nil, core.NewError(core.Conflict, "plugin changed")
	}
	admin := false
	if s.options.IsAdministrator != nil {
		admin, err = s.options.IsAdministrator(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	if !admin {
		if err := s.available(ctx, p, v); err != nil {
			return nil, err
		}
	}
	if !admin && !v.Enabled {
		return nil, core.NewError(core.Forbidden, "plugin disabled")
	}
	rows, err := persistence.Select[Connection](ctx, s.store, "plugins.connections", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", v.OwnerID), persistence.Eq("PluginID", id), persistence.Eq("Deleted", false)}})
	if err != nil {
		return nil, err
	}
	out := []ConnectionView{}
	for _, c := range rows {
		if c.OwnerID != v.OwnerID || c.PluginID != id || c.Deleted {
			continue
		}
		if err := s.checkConnection(ctx, p, c); err != nil {
			if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
				continue
			}
			return nil, err
		}
		provider, ok := providerFor(v.Manifest, c.ProviderID)
		review := c.Digest != v.Digest || !ok
		if !admin && (!c.Enabled || review || !granted(v, []string{"http.request"})) {
			continue
		}
		calls, active, err := s.connectionUsage(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		ops := []string{}
		for name := range provider.Operations {
			ops = append(ops, name)
		}
		sort.Strings(ops)
		out = append(out, ConnectionView{ID: c.ID, PluginID: c.PluginID, ProviderID: c.ProviderID, Name: c.Name, Enabled: c.Enabled, Version: c.Version, Configured: ok && (provider.Auth.Type == "none" || c.CredentialID != ""), NeedsReview: review, Operations: ops, CallLimit: c.CallLimit, Concurrent: c.Concurrent, TimeoutSeconds: c.TimeoutSeconds, Calls: calls, Active: active})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Service) SaveConnection(ctx context.Context, p core.Principal, q SaveConnection) (ConnectionView, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return ConnectionView{}, err
	}
	if err := q.Mutation.Validate(q.ID != ""); err != nil {
		return ConnectionView{}, err
	}
	if strings.TrimSpace(q.Name) == "" || len(q.Name) > 120 || q.CallLimit < 1 || q.CallLimit > 9007199254740991 || q.Concurrent < 1 || q.Concurrent > 16 || q.TimeoutSeconds < 1 || q.TimeoutSeconds > 60 || len(q.APIKey.Reveal()) > 8192 || strings.ContainsAny(q.APIKey.Reveal(), "\r\n") {
		return ConnectionView{}, invalid("invalid connection settings")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Connection
	changed := false
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, err := s.load(ctx, p.OwnerID, q.PluginID)
		if err != nil {
			return err
		}
		if v.Deleted || v.Digest != q.Digest {
			return core.NewError(core.Conflict, "plugin changed")
		}
		provider, ok := providerFor(v.Manifest, q.ProviderID)
		if !ok {
			return invalid("unknown provider")
		}
		payload, _ := json.Marshal(struct {
			ID                                  core.ID
			Plugin, Digest, Provider, Name, Key string
			Enabled                             bool
			Limit                               int64
			Concurrent, Timeout                 int
			Version                             core.Version
		}{q.ID, q.PluginID, q.Digest, q.ProviderID, q.Name, hash([]byte(q.APIKey.Reveal())), q.Enabled, q.CallLimit, q.Concurrent, q.TimeoutSeconds, q.Mutation.ExpectedVersion})
		key := persistence.Key(p.OwnerID, Key("connection.save/"+q.Mutation.IdempotencyKey))
		var receipt struct {
			Fingerprint string
			ID          core.ID
		}
		if err := tx.Get(ctx, "plugins.mutations", key, &receipt); err == nil {
			if receipt.Fingerprint != hash(payload) {
				return core.NewError(core.Conflict, "connection retry changed")
			}
			out, err = s.connection(ctx, p.OwnerID, receipt.ID)
			return err
		} else if !persistence.Missing(err) {
			return err
		}
		if q.ID == "" {
			count, err := tx.Count(ctx, "plugins.connections", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", v.OwnerID), persistence.Eq("PluginID", v.ID), persistence.Eq("Deleted", false)}, Limit: 32})
			if err != nil {
				return err
			}
			if count >= 32 {
				return invalid("plugin connection limit reached")
			}
		}
		c := Connection{ID: randomID(), OwnerID: v.OwnerID, PluginID: v.ID, ProviderID: q.ProviderID}
		if q.ID != "" {
			c, err = s.connection(ctx, p.OwnerID, q.ID)
			if err != nil {
				return err
			}
			if c.Deleted || c.PluginID != v.ID || c.ProviderID != q.ProviderID || c.Version != q.Mutation.ExpectedVersion {
				return core.NewError(core.Conflict, "connection changed; provider identity is immutable")
			}
		}
		if provider.Auth.Type == "none" {
			if !q.APIKey.Empty() {
				return invalid("provider does not use a credential")
			}
			if c.CredentialID != "" {
				return invalid("remove connection before changing its authentication type")
			}
		} else {
			if c.CredentialID == "" && q.APIKey.Empty() {
				return invalid("API key required")
			}
			if !q.APIKey.Empty() {
				if s.options.Credentials == nil {
					return core.NewError(core.Unavailable, "credential store unavailable")
				}
				var version core.Version
				if c.CredentialID != "" {
					meta, err := s.options.Credentials.Get(ctx, p, c.CredentialID)
					if err != nil {
						return err
					}
					version = meta.Version
				}
				cred, err := s.options.Credentials.Put(ctx, p, credential.PutRequest{Mutation: core.Mutation{IdempotencyKey: q.Mutation.IdempotencyKey, ExpectedVersion: version}, ID: c.CredentialID, Provider: "plugin-http:" + string(c.ID), Label: q.Name, Value: q.APIKey, Verification: q.Verification})
				if err != nil {
					return err
				}
				c.CredentialID = cred.ID
			}
		}
		c.Name = q.Name
		c.Enabled = q.Enabled
		c.Digest = v.Digest
		c.CallLimit = q.CallLimit
		c.Concurrent = q.Concurrent
		c.TimeoutSeconds = q.TimeoutSeconds
		c.Version++
		repo := resource.NewRepository(s.store)
		r := resource.Resource{ID: c.ID, OwnerID: c.OwnerID, Kind: resource.Connection, Name: c.Name, Provider: "plugin-http", Actions: []string{"read", "execute"}, State: resource.Available, Version: c.Version}
		if !c.Enabled {
			r.State = resource.Disconnected
		}
		if q.ID == "" {
			if err := repo.Insert(ctx, r, resource.Binding{ResourceID: c.ID}); err != nil {
				return err
			}
		} else if err := repo.Save(ctx, r, q.Mutation.ExpectedVersion); err != nil {
			return err
		}
		if err := tx.Put(ctx, "plugins.connections", persistence.Key(c.OwnerID, c.ID), c); err != nil {
			return err
		}
		if err := tx.Put(ctx, "plugins.mutations", key, struct {
			Fingerprint string
			ID          core.ID
		}{hash(payload), c.ID}); err != nil {
			return err
		}
		out = c
		changed = true
		return s.audit(ctx, p, "connection.save", v.ID)
	})
	if err != nil {
		return ConnectionView{}, err
	}
	if changed {
		s.cancelConnectionLocked(out.ID)
	}
	rows, err := s.Connections(ctx, p, q.PluginID, q.Digest)
	if err != nil {
		return ConnectionView{}, err
	}
	for _, v := range rows {
		if v.ID == out.ID {
			return v, nil
		}
	}
	return ConnectionView{}, core.NewError(core.NotFound, "connection unavailable")
}
func (s *Service) RemoveConnection(ctx context.Context, p core.Principal, q ConnectionRequest) error {
	if err := s.authorize(ctx, p, true); err != nil {
		return err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := s.connection(ctx, p.OwnerID, q.ID)
		if err != nil {
			return err
		}
		if c.PluginID != q.PluginID || c.Version != q.Mutation.ExpectedVersion {
			return core.NewError(core.Conflict, "connection changed")
		}
		if c.Deleted {
			return nil
		}
		if c.CredentialID != "" {
			if s.options.Credentials == nil {
				return core.NewError(core.Unavailable, "credential store unavailable")
			}
			meta, err := s.options.Credentials.Get(ctx, p, c.CredentialID)
			if err != nil {
				return err
			}
			if err := s.options.Credentials.Delete(ctx, p, credential.DeleteRequest{ID: c.CredentialID, Mutation: core.Mutation{IdempotencyKey: q.Mutation.IdempotencyKey, ExpectedVersion: meta.Version}, Verification: q.Verification}); err != nil {
				return err
			}
		}
		c.Deleted = true
		c.Enabled = false
		c.CredentialID = ""
		c.Version++
		repo := resource.NewRepository(s.store)
		r, err := repo.Get(ctx, p.OwnerID, c.ID)
		if err != nil {
			return err
		}
		r.State = resource.Disconnected
		r.Version++
		if err := repo.Save(ctx, r, q.Mutation.ExpectedVersion); err != nil {
			return err
		}
		if err := tx.Put(ctx, "plugins.connections", persistence.Key(c.OwnerID, c.ID), c); err != nil {
			return err
		}
		return s.audit(ctx, p, "connection.remove", c.PluginID)
	})
	if err == nil {
		s.cancelConnectionLocked(q.ID)
	}
	return err
}
func (s *Service) cancelConnectionLocked(id core.ID) {
	for _, run := range s.httpRunning {
		if run.ConnectionID == id {
			run.Cancel()
		}
	}
}
