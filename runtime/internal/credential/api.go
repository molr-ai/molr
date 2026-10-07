// Package credential separates owner credential management from trusted adapter-only secret use.
package credential

import (
	"context"
	"net/url"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
)

type Credential struct {
	ID        core.ID
	OwnerID   core.ID
	Provider  string
	Label     string
	ExpiresAt *time.Time
	Version   core.Version
}
type PutRequest struct {
	Mutation     core.Mutation
	ID           core.ID
	Provider     string
	Label        string
	Value        core.Secret
	Verification auth.Verification
}
type DeleteRequest struct {
	Mutation     core.Mutation
	ID           core.ID
	Verification auth.Verification
}

// Manager is owner-only. Get/List return metadata, never secret material.
type Manager interface {
	Put(context.Context, core.Principal, PutRequest) (Credential, error)
	Get(context.Context, core.Principal, core.ID) (Credential, error)
	List(context.Context, core.Principal, core.PageRequest) (core.Page[Credential], error)
	Delete(context.Context, core.Principal, DeleteRequest) error
}

// Access is passed only to trusted fixed-provider adapters. ResourceID and
// Origin must match the persisted binding and current policy, including after
// redirects/DNS resolution. It is never part of the tool execution interface.
// Principal comes from authentication or a trusted, persisted service-token
// identity. Owner connection checks do not invent an assistant execution.
// Background refresh uses a live owner token restricted to credential.refresh
// and the persisted resource binding; it has no anonymous/system bypass.
// Execution is optional for owners (zero value), required for assistants and
// must identify exactly the same principal. A legacy execution-only request is
// accepted through EffectivePrincipal, but receives the same live broker checks.
type Access struct {
	Principal    core.Principal
	Execution    core.Execution
	ResourceID   core.ID
	CredentialID core.ID
	Origin       string
}

// EffectivePrincipal resolves the trusted actor while rejecting ambiguous or
// mismatched identities. It validates shape only; Broker must verify the actor,
// execution, persisted binding and current permissions before exposing secrets.
func (a Access) EffectivePrincipal() (core.Principal, error) {
	p := a.Principal
	if p == (core.Principal{}) {
		p = a.Execution.Principal
	}
	if err := p.Validate(); err != nil {
		return core.Principal{}, err
	}
	if p.Kind == core.ActorAssistant {
		if err := a.Execution.Validate(); err != nil {
			return core.Principal{}, err
		}
		if p != a.Execution.Principal {
			return core.Principal{}, core.NewError(core.Forbidden, "execution principal mismatch")
		}
	} else if a.Execution != (core.Execution{}) {
		return core.Principal{}, core.NewError(core.Forbidden, "owner access cannot claim assistant execution")
	}
	return p, nil
}

// Validate checks request shape; it never grants access based on supplied IDs.
// The origin must be an exact HTTP(S) origin, without credentials or a path.
// DNS, redirects and connected-address checks belong to the trusted adapter.
func (a Access) Validate() error {
	if _, err := a.EffectivePrincipal(); err != nil {
		return err
	}
	if a.ResourceID.Validate() != nil || a.CredentialID.Validate() != nil {
		return core.Invalid("credential_access", "resource and credential IDs required")
	}
	u, err := url.Parse(a.Origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return core.Invalid("origin", "exact HTTP(S) origin required")
	}
	return nil
}

type Broker interface {
	// WithOwnerSecret is only for fixed adapters testing unsaved connections.
	// It requires a live owner browser session and matching credential provider.
	// The adapter must enforce deployment network policy; it never exports the secret.
	WithOwnerSecret(context.Context, core.Principal, core.ID, string, func(core.Secret) error) error
	WithSecret(context.Context, Access, func(core.Secret) error) error
	Refresh(context.Context, Access) error
}

// EncryptedValue is storage-only; plaintext and master keys are never persisted
// alongside it. Cipher/version selection belongs to the implementation.
type EncryptedValue struct {
	Metadata      Credential
	Ciphertext    []byte
	KeyID         string
	FormatVersion uint32
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (EncryptedValue, error)
	List(context.Context, core.ID, core.PageRequest) (core.Page[Credential], error)
	Save(context.Context, EncryptedValue, core.Version) error
	Delete(context.Context, core.ID, core.ID, core.Version) error
}
