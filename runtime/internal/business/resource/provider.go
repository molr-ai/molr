package resource

import (
	"context"
	"crypto/sha256"
	"net/url"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// ProviderConnection exposes editable connection metadata only to the owner.
func (s *service) ProviderConnection(ctx context.Context, p core.Principal, id core.ID) (ProviderConnection, error) {
	var result ProviderConnection
	err := s.read(ctx, p, id, policy.Manage, true, func(ctx context.Context) error {
		r, err := s.repo.Get(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		if r.Kind != Model {
			return invalid("provider")
		}
		b, err := s.repo.Binding(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		result = ProviderConnection{PricingProvider: b.PricingProvider, Resource: r, Endpoint: b.Endpoint, CredentialID: b.CredentialID}
		return nil
	})
	return result, err
}

// SaveProvider commits credential rotation and connection changes atomically.
func (s *service) SaveProvider(ctx context.Context, p core.Principal, req SaveProviderRequest) (Resource, error) {
	// Secret values and verification proofs must never enter mutation receipts.
	payload := struct {
		ID                              core.ID
		Name, Endpoint, PricingProvider string
		UseAPIKey                       bool
		KeyDigest                       [32]byte
	}{req.ID, req.Name, req.Endpoint, req.PricingProvider, req.UseAPIKey, sha256.Sum256([]byte(req.APIKey.Reveal()))}
	return mutate(ctx, s, p, "save-provider", req.ID, req.Mutation, payload, req.ID != "", true, func(ctx context.Context) (Resource, error) {
		if !validText(req.Name, 256) || (!req.UseAPIKey && !req.APIKey.Empty()) {
			return Resource{}, invalid("provider")
		}
		u, err := url.Parse(req.Endpoint)
		if err != nil || u.Host == "" {
			return Resource{}, invalid("endpoint")
		}
		var r Resource
		var binding Binding
		if req.ID != "" {
			r, err = s.repo.Get(ctx, p.OwnerID, req.ID)
			if err != nil {
				return r, err
			}
			if r.Kind != Model || r.Provider != "openai-compatible" || r.State != Available {
				return r, invalid("provider")
			}
			if r.Version != req.Mutation.ExpectedVersion {
				return r, conflict()
			}
			binding, err = s.repo.Binding(ctx, p.OwnerID, req.ID)
			if err != nil {
				return r, err
			}
		} else {
			r = Resource{ID: s.id(), OwnerID: p.OwnerID, Kind: Model, Provider: "openai-compatible", State: Available}
		}
		if binding.Endpoint != "" && binding.Endpoint != req.Endpoint {
			// A different endpoint has a different catalog; retain deliberate overrides.
			manual := []ModelConfig{}
			for _, m := range binding.Models {
				if !m.Discovered {
					manual = append(manual, m)
				}
			}
			binding.Models = manual
		}
		if binding.Endpoint != req.Endpoint || binding.PricingProvider != req.PricingProvider {
			for i := range binding.Models {
				binding.Models[i].ProviderPricing = nil
			}
		}
		binding.ResourceID, binding.Endpoint, binding.PricingProvider = r.ID, req.Endpoint, req.PricingProvider
		binding.AllowedOrigins = []string{u.Scheme + "://" + u.Host}
		provider := s.options.Providers[r.Provider]
		if provider.Validate == nil {
			return r, invalid("provider")
		}
		actions, err := provider.Validate(ctx, binding)
		if err != nil {
			return r, err
		}
		if req.UseAPIKey {
			if req.APIKey.Empty() && binding.CredentialID == "" {
				return r, invalid("api_key")
			}
			if !req.APIKey.Empty() {
				if s.options.Credentials == nil {
					return r, core.NewError(core.Unavailable, "credential storage unavailable")
				}
				id := binding.CredentialID
				var version core.Version
				if id != "" {
					// Do not rotate a key still referenced by another connection.
					bindings, err := persistence.Select[Binding](ctx, s.store, bindingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("CredentialID", id), {Field: "ResourceID", Op: "!=", Value: r.ID}}, Limit: 1})
					if err != nil {
						return r, err
					}
					for _, other := range bindings {
						if other.ResourceID != r.ID && other.CredentialID == id {
							id = ""
							break
						}
					}
				}
				if id != "" {
					meta, err := s.options.Credentials.Get(ctx, p, id)
					if err != nil {
						return r, err
					}
					version = meta.Version
				}
				key, err := s.options.Credentials.Put(ctx, p, credential.PutRequest{Mutation: core.Mutation{IdempotencyKey: req.Mutation.IdempotencyKey, ExpectedVersion: version}, ID: id, Provider: r.Provider, Label: req.Name, Value: req.APIKey, Verification: req.Verification})
				if err != nil {
					return r, err
				}
				binding.CredentialID = key.ID
			}
		} else {
			binding.CredentialID = ""
		}
		r.Name, r.Actions = req.Name, actions
		r.Version++
		if req.ID == "" {
			return r, s.repo.Insert(ctx, r, binding)
		}
		if err := s.repo.Save(ctx, r, req.Mutation.ExpectedVersion); err != nil {
			return r, err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return putBinding(ctx, tx, r.OwnerID, binding)
		})
		return r, err
	})
}
