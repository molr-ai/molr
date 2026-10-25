package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/chatgpt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
)

func buildCredentials(adapters adapters, identity *auth.Manager, key []byte, wiring *buildWiring) (*credential.Vault, *chatgptProvider, error) {
	store := adapters.store
	chatgptClient := chatgpt.New()
	vault, err := credential.NewService(store, credential.Options{ResolveProvider: func(ctx context.Context, a credential.Access, meta credential.Credential, secret core.Secret) (core.Secret, core.Secret, error) {
		if meta.Provider != "chatgpt" {
			return core.Secret{}, secret, nil
		}
		return chatgptClient.Resolve(ctx, secret)
	}, RequireAdministrator: identity.RequireAdministrator, MasterKey: key, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, CheckVerification: identity.CheckVerification, AuthorizeAccess: func(ctx context.Context, q credential.Access) error {
		p, err := q.EffectivePrincipal()
		if err != nil {
			return err
		}
		resourceValue, err := resource.NewRepository(store).Get(ctx, p.OwnerID, q.ResourceID)
		if err != nil {
			return err
		}
		if resourceValue.Kind != resource.Connection {
			return adapters.credentialAccess(ctx, q)
		}
		if resourceValue.Provider == "mcp" {
			if wiring.mcp == nil {
				return denied()
			}
			c, err := wiring.mcp.ConnectionAccess(ctx, p, q.ResourceID)
			if err != nil {
				return err
			}
			if c.CredentialID != q.CredentialID || c.Origin() != q.Origin {
				return denied()
			}
			value, err := credential.NewRepository(store).Get(ctx, p.OwnerID, q.CredentialID)
			if err != nil {
				return err
			}
			if value.Metadata.Provider != "mcp:"+string(c.ID) {
				return denied()
			}
			return nil
		}
		if wiring.plugins == nil {
			return core.NewError(core.Unavailable, "plugin connections unavailable")
		}
		c, provider, err := wiring.plugins.ConnectionAccess(ctx, p, q.ResourceID)
		if err != nil {
			return err
		}
		if c.CredentialID != q.CredentialID || provider.Origin != q.Origin {
			return denied()
		}
		value, err := credential.NewRepository(store).Get(ctx, p.OwnerID, q.CredentialID)
		if err != nil {
			return err
		}
		if value.Metadata.Provider != "plugin-http:"+string(c.ID) {
			return denied()
		}
		return nil
	}})
	if err != nil {
		return nil, nil, err
	}
	chatgptOAuth := &chatgptProvider{store: store, identity: identity, vault: vault, client: chatgptClient}
	return vault, chatgptOAuth, nil
}
