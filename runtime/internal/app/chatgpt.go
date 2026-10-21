package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/chatgpt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/oauthbroker"
	"molr.ai/runtime/internal/persistence"
	"net/url"
	"strings"
)

type chatgptProvider struct {
	store    persistence.Store
	identity *auth.Manager
	vault    *credential.Vault
	client   *chatgpt.Client
}

func (s *chatgptProvider) hostID(ctx context.Context) (string, error) {
	var id string
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		err := tx.Get(ctx, "system.chatgpt", "host", &id)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		var b [16]byte
		if _, err = rand.Read(b[:]); err != nil {
			return err
		}
		b[6] = (b[6] & 15) | 64
		b[8] = (b[8] & 63) | 128
		id = fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		return tx.Put(ctx, "system.chatgpt", "host", id)
	})
	return id, err
}
func (s *chatgptProvider) selected(ctx context.Context, p core.Principal, q oauthbroker.StartRequest) (resource.Resource, resource.Binding, error) {
	if err := s.identity.RequireAdministrator(ctx, p); err != nil {
		return resource.Resource{}, resource.Binding{}, err
	}
	if p.SessionID == "" || p.TokenID != "" {
		return resource.Resource{}, resource.Binding{}, core.NewError(core.Forbidden, "interactive administrator required")
	}
	if len(q.Name) > 256 || len(q.Verification) > 4096 {
		return resource.Resource{}, resource.Binding{}, core.Invalid("connection", "invalid OAuth request")
	}
	if q.ConnectionID == "" {
		return resource.Resource{}, resource.Binding{}, nil
	}
	repo := resource.NewRepository(s.store)
	r, err := repo.Get(ctx, p.OwnerID, q.ConnectionID)
	if err != nil {
		return r, resource.Binding{}, err
	}
	b, err := repo.Binding(ctx, p.OwnerID, r.ID)
	if err != nil {
		return r, b, err
	}
	if r.Kind != resource.Model || r.Provider != "openai-compatible" || b.ChatGPT == nil || r.Version != q.ExpectedVersion || b.Endpoint != chatgpt.Endpoint {
		return r, b, core.NewError(core.Conflict, "ChatGPT connection changed")
	}
	return r, b, nil
}
func (s *chatgptProvider) AuthorizationURL(ctx context.Context, p core.Principal, a oauthbroker.Authorization) (string, error) {
	_, b, err := s.selected(ctx, p, a.Request)
	if err != nil {
		return "", err
	}
	if err = s.identity.CheckVerification(ctx, p, auth.Verification{Token: core.NewSecret(a.Request.Verification)}, "credential.put"); err != nil {
		return "", err
	}
	host, err := s.hostID(ctx)
	if err != nil {
		return "", err
	}
	clientID := "dynamic_agent_client"
	if b.ChatGPT != nil {
		clientID = b.ChatGPT.ClientID
	}
	q := url.Values{"client_id": {clientID}, "ext_agent_host_id": {host}, "response_type": {"code"}, "redirect_uri": {a.RedirectURI}, "scope": {chatgpt.Scopes}, "resource": {chatgpt.Resource}, "state": {a.State}, "nonce": {a.Nonce}, "code_challenge_method": {"S256"}, "code_challenge": {a.Challenge}}
	if b.ChatGPT == nil {
		q.Set("agent_name_hint", "Molr")
	} else if b.CredentialID != "" {
		err = s.vault.WithOwnerSecret(ctx, p, b.CredentialID, "chatgpt", func(secret core.Secret) error {
			var session chatgpt.Session
			if json.Unmarshal([]byte(secret.Reveal()), &session) != nil {
				return core.NewError(core.Unavailable, "invalid ChatGPT session")
			}
			if session.ClientID != clientID || session.Subject != b.ChatGPT.Subject {
				return core.NewError(core.Forbidden, "ChatGPT registration mismatch")
			}
			if session.IDToken != "" {
				q.Set("id_token_hint", session.IDToken)
			}
			if session.Email != "" {
				q.Set("login_hint", session.Email)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return chatgpt.Origin + "/api/accounts/authorize?" + q.Encode(), nil
}
func (s *chatgptProvider) Complete(ctx context.Context, p core.Principal, g oauthbroker.Grant) (core.ID, error) {
	r, b, err := s.selected(ctx, p, g.Request)
	if err != nil {
		return "", err
	}
	if g.Issuer != "" && g.Issuer != chatgpt.Origin {
		return "", core.Invalid("issuer", "unexpected OAuth issuer")
	}
	clientID := g.ClientID
	if b.ChatGPT != nil {
		if clientID != "" && clientID != b.ChatGPT.ClientID {
			return "", core.NewError(core.Forbidden, "ChatGPT client changed")
		}
		clientID = b.ChatGPT.ClientID
	}
	if clientID == "" || clientID == "dynamic_agent_client" || len(clientID) > 256 {
		return "", core.Invalid("client_id", "issued registration required")
	}
	proof := auth.Verification{Token: core.NewSecret(g.Request.Verification)}
	if err = s.identity.CheckVerification(ctx, p, proof, "credential.put"); err != nil {
		return "", err
	}
	// Persist the issued registration before exchange, visibly disconnected until verified.
	if r.ID == "" {
		hash := sha256.Sum256([]byte(g.Nonce))
		id := core.ID("chatgpt_" + hex.EncodeToString(hash[:16]))
		name := strings.TrimSpace(g.Request.Name)
		if name == "" {
			name = "OpenAI · ChatGPT"
		}
		r = resource.Resource{Authentication: "chatgpt", ID: id, OwnerID: catalog.Scope, Kind: resource.Model, Name: name, Provider: "openai-compatible", Actions: []string{"read", "execute"}, State: resource.Disconnected, Version: 1}
		b = resource.Binding{ResourceID: id, Endpoint: chatgpt.Endpoint, PricingProvider: "openai", AllowedOrigins: []string{"https://api.openai.com"}, ChatGPT: &resource.ChatGPTConnection{ClientID: clientID}}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := s.identity.RequireAdministrator(ctx, p); err != nil {
				return err
			}
			return resource.NewRepository(tx.Store()).Insert(ctx, r, b)
		})
		if err != nil {
			return "", err
		}
	}
	session, err := s.client.Exchange(ctx, clientID, g.Code.Reveal(), g.Verifier.Reveal(), g.RedirectURI, g.Nonce, b.ChatGPT.Subject)
	if err != nil {
		return r.ID, err
	}
	data, _ := json.Marshal(session)
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.identity.RequireAdministrator(ctx, p); err != nil {
			return err
		}
		repo := resource.NewRepository(tx.Store())
		current, err := repo.Get(ctx, p.OwnerID, r.ID)
		if err != nil {
			return err
		}
		if current.Version != r.Version {
			return persistence.Conflict("ChatGPT connection changed")
		}
		version := core.Version(0)
		if b.CredentialID != "" {
			m, err := s.vault.Get(ctx, p, b.CredentialID)
			if err != nil {
				return err
			}
			version = m.Version
		}
		key, err := s.vault.Put(ctx, p, credential.PutRequest{ID: b.CredentialID, Mutation: core.Mutation{IdempotencyKey: g.Nonce, ExpectedVersion: version}, Provider: "chatgpt", Label: r.Name, Value: core.NewSecret(string(data)), Verification: proof})
		if err != nil {
			return err
		}
		b.CredentialID = key.ID
		b.ChatGPT = &resource.ChatGPTConnection{ClientID: session.ClientID, Subject: session.Subject, Email: session.Email}
		r.State = resource.Available
		r.Version++
		if err = repo.Save(ctx, r, current.Version); err != nil {
			return err
		}
		return repo.SaveBinding(ctx, b)
	})
	return r.ID, err
}

type chatgptDisconnectReceipt struct {
	ID      core.ID
	Version core.Version
	Revoked bool
}

func (s *chatgptProvider) disconnect(ctx context.Context, p core.Principal, q resource.DeleteProviderRequest) (bool, error) {
	if err := s.identity.RequireAdministrator(ctx, p); err != nil {
		return false, err
	}
	if q.ID.Validate() != nil {
		return false, core.Invalid("id", "connection ID required")
	}
	receiptKey := persistence.Key(p.OwnerID, core.ID(q.Mutation.IdempotencyKey))
	saved, loadErr := persistence.Load[chatgptDisconnectReceipt](ctx, s.store, "chatgpt.disconnects", receiptKey)
	if loadErr == nil {
		if saved.ID != q.ID || saved.Version != q.Mutation.ExpectedVersion {
			return false, persistence.Conflict("disconnect retry changed")
		}
		return saved.Revoked, nil
	}
	if !persistence.Missing(loadErr) {
		return false, loadErr
	}
	r, b, err := s.selected(ctx, p, oauthbroker.StartRequest{ConnectionID: q.ID, ExpectedVersion: q.Mutation.ExpectedVersion})
	if err != nil {
		return false, err
	}
	if err = s.identity.CheckVerification(ctx, p, q.Verification, "credential.delete"); err != nil {
		return false, err
	}
	revoked := b.CredentialID == ""
	clear := func() error {
		return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := s.identity.RequireAdministrator(ctx, p); err != nil {
				return err
			}
			repo := resource.NewRepository(tx.Store())
			current, err := repo.Get(ctx, p.OwnerID, r.ID)
			if err != nil {
				return err
			}
			if current.Version != r.Version {
				return persistence.Conflict("ChatGPT connection changed")
			}
			if b.CredentialID != "" {
				key, err := s.vault.Get(ctx, p, b.CredentialID)
				if err != nil {
					return err
				}
				if err = s.vault.Delete(ctx, p, credential.DeleteRequest{ID: key.ID, Mutation: core.Mutation{IdempotencyKey: q.Mutation.IdempotencyKey, ExpectedVersion: key.Version}, Verification: q.Verification}); err != nil {
					return err
				}
			}
			b.CredentialID = ""
			r.State = resource.Disconnected
			r.Version++
			if err = repo.Save(ctx, r, current.Version); err != nil {
				return err
			}
			if err = repo.SaveBinding(ctx, b); err != nil {
				return err
			}
			return tx.Put(ctx, "chatgpt.disconnects", receiptKey, chatgptDisconnectReceipt{q.ID, q.Mutation.ExpectedVersion, revoked})
		})
	}
	if b.CredentialID == "" {
		err = clear()
	} else {
		// The vault's per-credential lock covers revocation AND local deletion, so
		// concurrent inference cannot rotate the refresh token between these steps.
		err = s.vault.WithOwnerSecret(ctx, p, b.CredentialID, "chatgpt", func(secret core.Secret) error { revoked = s.client.Revoke(ctx, secret) == nil; return clear() })
	}
	return revoked, err
}
