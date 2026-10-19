// Package oauthbroker coordinates short-lived, session-bound native OAuth flows.
// Providers own token exchange, identity/scope checks and encrypted persistence.
package oauthbroker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"strconv"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
)

const Lifetime = 5 * time.Minute

// Authorization contains no secrets. Providers must use these exact values.
type Authorization struct{ RedirectURI, State, Nonce, Challenge string }

// Grant stays inside the Runtime; never serialize it into a page or log.
type Grant struct {
	RedirectURI             string
	Code, Verifier          core.Secret
	Nonce, ClientID, Issuer string
}
type Provider interface {
	AuthorizationURL(context.Context, core.Principal, Authorization) (string, error)
	// Complete must validate issuer, identity, nonce and granted scopes as applicable,
	// recheck current access before committing, and encrypt credentials at rest.
	// Return only a non-secret connection ID. Refresh ownership belongs to Runtime.
	Complete(context.Context, core.Principal, Grant) (core.ID, error)
}
type StartRequest struct{ Provider, RedirectURI string }
type Started struct {
	ID, AuthorizationURL, State string
	ExpiresAt                   time.Time
}
type CompleteRequest struct{ ID, State, Code, ClientID, Issuer, Error string }
type Result struct {
	ID, Status   string
	ConnectionID core.ID `json:",omitempty"`
}
type attempt struct {
	p        core.Principal
	provider Provider
	auth     Authorization
	verifier string
	expires  time.Time
	result   Result
}
type Broker struct {
	mu        sync.Mutex
	providers map[string]Provider
	attempts  map[string]*attempt
	validate  func(context.Context, core.Principal) error
	now       func() time.Time
}

func New(providers map[string]Provider, validate func(context.Context, core.Principal) error) *Broker {
	copy := make(map[string]Provider, len(providers))
	for id, provider := range providers {
		copy[id] = provider
	}
	return &Broker{providers: copy, attempts: map[string]*attempt{}, validate: validate, now: time.Now}
}
func (b *Broker) check(ctx context.Context, p core.Principal) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if p.SessionID == "" || b.validate == nil {
		return core.NewError(core.Forbidden, "interactive session required")
	}
	return b.validate(ctx, p)
}
func secret() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535 && u.Scheme == "http" && u.Host == "127.0.0.1:"+strconv.Itoa(port) && u.Path == "/auth/callback" && u.RawPath == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}
func (b *Broker) Start(ctx context.Context, p core.Principal, input StartRequest) (Started, error) {
	if err := b.check(ctx, p); err != nil {
		return Started{}, err
	}
	provider := b.providers[input.Provider]
	if provider == nil {
		return Started{}, core.Invalid("provider", "OAuth provider unavailable")
	}
	if !validRedirect(input.RedirectURI) {
		return Started{}, core.Invalid("redirect_uri", "numeric loopback callback required")
	}
	id, verifier := secret(), secret()
	hash := sha256.Sum256([]byte(verifier))
	a := &attempt{p: p, provider: provider, auth: Authorization{input.RedirectURI, secret(), secret(), base64.RawURLEncoding.EncodeToString(hash[:])}, verifier: verifier, expires: b.now().Add(Lifetime), result: Result{ID: id, Status: "preparing"}}
	b.mu.Lock()
	for key, old := range b.attempts {
		if !b.now().Before(old.expires) && old.result.Status != "completing" {
			delete(b.attempts, key)
			continue
		}
		if old.p.SessionID == p.SessionID && (old.result.Status == "preparing" || old.result.Status == "pending" || old.result.Status == "completing") {
			b.mu.Unlock()
			return Started{}, core.NewError(core.Conflict, "OAuth attempt already active")
		}
	}
	if len(b.attempts) >= 128 {
		b.mu.Unlock()
		return Started{}, core.NewError(core.RateLimited, "OAuth attempt limit reached")
	}
	b.attempts[id] = a
	b.mu.Unlock()
	raw, err := provider.AuthorizationURL(ctx, p, a.auth)
	u, parseErr := url.Parse(raw)
	valid := err == nil && parseErr == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && len(raw) <= 8192
	if valid {
		q := u.Query()
		for key, expected := range map[string]string{"redirect_uri": a.auth.RedirectURI, "state": a.auth.State, "nonce": a.auth.Nonce, "code_challenge": a.auth.Challenge, "code_challenge_method": "S256", "response_type": "code"} {
			if len(q[key]) != 1 || q.Get(key) != expected {
				valid = false
			}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !valid || ctx.Err() != nil || !b.now().Before(a.expires) {
		delete(b.attempts, id)
		return Started{}, core.NewError(core.Unavailable, "OAuth preparation failed")
	}
	a.result.Status = "pending"
	return Started{id, raw, a.auth.State, a.expires}, nil
}
func (b *Broker) lookup(p core.Principal, id string) (*attempt, error) {
	a := b.attempts[id]
	if a == nil || a.p.OwnerID != p.OwnerID || a.p.SessionID != p.SessionID || !b.now().Before(a.expires) {
		return nil, core.NewError(core.NotFound, "OAuth attempt unavailable")
	}
	return a, nil
}
func (b *Broker) Status(ctx context.Context, p core.Principal, id string) (Result, error) {
	if err := b.check(ctx, p); err != nil {
		return Result{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	a, err := b.lookup(p, id)
	if err != nil {
		return Result{}, err
	}
	return a.result, nil
}
func (b *Broker) Cancel(ctx context.Context, p core.Principal, id string) (Result, error) {
	if err := b.check(ctx, p); err != nil {
		return Result{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	a, err := b.lookup(p, id)
	if err != nil {
		return Result{}, err
	}
	// Once exchange starts, cancellation cannot promise to undo a committed grant.
	if a.result.Status == "pending" {
		a.result.Status = "cancelled"
		a.verifier = ""
	}
	return a.result, nil
}
func (b *Broker) Complete(ctx context.Context, p core.Principal, input CompleteRequest) (Result, error) {
	if err := b.check(ctx, p); err != nil {
		return Result{}, err
	}
	if len(input.Code) > 4096 || len(input.ClientID) > 256 || len(input.Issuer) > 2048 || len(input.Error) > 128 || (input.Code == "") == (input.Error == "") {
		return Result{}, core.Invalid("callback", "invalid OAuth response")
	}
	b.mu.Lock()
	a, err := b.lookup(p, input.ID)
	if err != nil {
		b.mu.Unlock()
		return Result{}, err
	}
	if subtle.ConstantTimeCompare([]byte(input.State), []byte(a.auth.State)) != 1 {
		b.mu.Unlock()
		return Result{}, core.NewError(core.Forbidden, "OAuth state mismatch")
	}
	if a.result.Status != "pending" {
		result := a.result
		b.mu.Unlock()
		return result, nil
	}
	if input.Error != "" {
		a.result.Status = "cancelled"
		a.verifier = ""
		result := a.result
		b.mu.Unlock()
		return result, nil
	}
	a.result.Status = "completing"
	grant := Grant{a.auth.RedirectURI, core.NewSecret(input.Code), core.NewSecret(a.verifier), a.auth.Nonce, input.ClientID, input.Issuer}
	a.verifier = ""
	b.mu.Unlock()
	connection, completeErr := a.provider.Complete(ctx, p, grant)
	b.mu.Lock()
	defer b.mu.Unlock()
	if completeErr != nil || connection.Validate() != nil {
		a.result.Status = "failed"
	} else {
		a.result.Status = "connected"
		a.result.ConnectionID = connection
	}
	return a.result, nil
}
