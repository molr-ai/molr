package credential

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"sync"
	"time"
)

type Options struct {
	// ResolveProvider renews registered OAuth material and returns a bearer token.
	// Empty updated material means no write; ordinary credentials pass through.
	ResolveProvider      func(context.Context, Access, Credential, core.Secret) (updated, bearer core.Secret, err error)
	RequireAdministrator func(context.Context, core.Principal) error
	Now                  func() time.Time
	MasterKey            []byte
	ValidatePrincipal    func(context.Context, core.Principal) error
	CheckScope           func(context.Context, core.Principal, string) error
	CheckVerification    func(context.Context, core.Principal, auth.Verification, string) error
	// AuthorizeAccess verifies persisted resource/provider/credential/origin binding,
	// current grants and execution validity without external I/O. Missing
	// authorization denies all secret use. The trusted provider adapter enforces
	// resolved/connected addresses and redirects before sending the credential.
	AuthorizeAccess func(context.Context, Access) error
	// RefreshProvider is a fixed trusted provider adapter, never caller-supplied code.
	// It runs outside a storage transaction. A nil adapter means refresh unsupported.
	RefreshProvider func(context.Context, Access, core.Secret) (core.Secret, error)
}
type Vault struct {
	sessionLocks [64]sync.Mutex
	store        persistence.Store
	repo         Repository
	options      Options
	keyID        string
}

func NewService(store persistence.Store, o Options) (*Vault, error) {
	if store == nil || len(o.MasterKey) != 32 || o.ValidatePrincipal == nil || o.CheckVerification == nil {
		return nil, core.Invalid("credential", "store, 32-byte master key and live verification required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	o.MasterKey = append([]byte(nil), o.MasterKey...)
	sum := sha256.Sum256(o.MasterKey)
	return &Vault{store: store, repo: NewRepository(store), options: o, keyID: hex.EncodeToString(sum[:8])}, nil
}
func (v *Vault) owner(ctx context.Context, p core.Principal, scope string) error {
	if v.options.RequireAdministrator != nil {
		if err := v.options.RequireAdministrator(ctx, p); err != nil {
			return err
		}
	}
	if e := p.RequireOwner(); e != nil {
		return e
	}
	if e := v.options.ValidatePrincipal(ctx, p); e != nil {
		return e
	}
	if p.TokenID != "" {
		if v.options.CheckScope == nil {
			return core.NewError(core.Forbidden, "token scope unavailable")
		}
		return v.options.CheckScope(ctx, p, scope)
	}
	return nil
}
func (v *Vault) encrypt(metadata Credential, secret core.Secret) (EncryptedValue, error) {
	block, e := aes.NewCipher(v.options.MasterKey)
	if e != nil {
		return EncryptedValue{}, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return EncryptedValue{}, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return EncryptedValue{}, e
	}
	aad := string(metadata.OwnerID) + "/" + string(metadata.ID) + "/" + metadata.Provider + "/1"
	ciphertext := g.Seal(nonce, nonce, []byte(secret.Reveal()), []byte(aad))
	return EncryptedValue{Metadata: metadata, Ciphertext: ciphertext, KeyID: v.keyID, FormatVersion: 1}, nil
}
func (v *Vault) decrypt(value EncryptedValue) (core.Secret, error) {
	if value.KeyID != v.keyID || value.FormatVersion != 1 {
		return core.Secret{}, core.NewError(core.FailedPrecondition, "credential key/version unavailable")
	}
	block, e := aes.NewCipher(v.options.MasterKey)
	if e != nil {
		return core.Secret{}, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return core.Secret{}, e
	}
	if len(value.Ciphertext) < g.NonceSize() {
		return core.Secret{}, core.NewError(core.Internal, "invalid credential envelope")
	}
	m := value.Metadata
	aad := string(m.OwnerID) + "/" + string(m.ID) + "/" + m.Provider + "/1"
	plaintext, e := g.Open(nil, value.Ciphertext[:g.NonceSize()], value.Ciphertext[g.NonceSize():], []byte(aad))
	if e != nil {
		return core.Secret{}, core.Wrap(core.Internal, "credential decryption failed", e)
	}
	return core.NewSecret(string(plaintext)), nil
}

type putDigest struct {
	ID                           core.ID
	Provider, Label, ValueDigest string
	Version                      core.Version
}
type mutationReceipt struct {
	Digest   string
	Metadata Credential
}

func inputDigest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func secretDigest(value core.Secret) string {
	sum := sha256.Sum256([]byte(value.Reveal()))
	return hex.EncodeToString(sum[:])
}
func credentialID() core.ID {
	var b [24]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("operating system entropy unavailable")
	}
	return core.ID(base64.RawURLEncoding.EncodeToString(b[:]))
}
func (v *Vault) Put(ctx context.Context, p core.Principal, r PutRequest) (Credential, error) {
	if e := r.Mutation.Validate(r.ID != ""); e != nil {
		return Credential{}, e
	}
	if r.Provider == "" || len(r.Provider) > 128 || len(r.Label) > 256 || r.Value.Empty() || len(r.Value.Reveal()) > 65536 {
		return Credential{}, core.Invalid("credential", "provider and bounded secret required")
	}
	want := inputDigest(putDigest{r.ID, r.Provider, r.Label, secretDigest(r.Value), r.Mutation.ExpectedVersion})
	var out Credential
	e := v.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := v.owner(ctx, p, "manage"); e != nil {
			return e
		}
		if e := v.options.CheckVerification(ctx, p, r.Verification, "credential.put"); e != nil {
			return e
		}
		key := string(p.OwnerID) + "/credential.put/" + r.Mutation.IdempotencyKey
		var saved mutationReceipt
		e := tx.Get(ctx, "security.idempotency", key, &saved)
		if e == nil {
			if saved.Digest != want {
				return persistence.Conflict("credential retry changed payload")
			}
			out = saved.Metadata
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		id := r.ID
		if id == "" {
			id = credentialID()
		}
		catalogOwner := catalog.Scope
		out = Credential{ID: id, OwnerID: catalogOwner, Provider: r.Provider, Label: r.Label, Version: r.Mutation.ExpectedVersion + 1}
		encrypted, e := v.encrypt(out, r.Value)
		if e != nil {
			return e
		}
		if e = v.repo.Save(ctx, encrypted, r.Mutation.ExpectedVersion); e != nil {
			return e
		}
		if e = v.audit(ctx, p, "credential.put", out.ID); e != nil {
			return e
		}
		return tx.Put(ctx, "security.idempotency", key, mutationReceipt{want, out})
	})
	return out, e
}
func (v *Vault) Get(ctx context.Context, p core.Principal, id core.ID) (Credential, error) {
	var out Credential
	err := v.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := v.owner(ctx, p, "read"); err != nil {
			return err
		}
		value, err := v.repo.Get(ctx, p.OwnerID, id)
		out = value.Metadata
		return err
	})
	return out, err
}
func (v *Vault) List(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[Credential], error) {
	var out core.Page[Credential]
	err := v.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := v.owner(ctx, p, "read"); err != nil {
			return err
		}
		var err error
		out, err = v.repo.List(ctx, p.OwnerID, q)
		return err
	})
	return out, err
}
func (v *Vault) Delete(ctx context.Context, p core.Principal, r DeleteRequest) error {
	if e := r.Mutation.Validate(true); e != nil {
		return e
	}
	return v.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := v.owner(ctx, p, "manage"); e != nil {
			return e
		}
		if e := v.options.CheckVerification(ctx, p, r.Verification, "credential.delete"); e != nil {
			return e
		}
		key := string(p.OwnerID) + "/credential.delete/" + r.Mutation.IdempotencyKey
		want := inputDigest(struct {
			ID      core.ID
			Version core.Version
		}{r.ID, r.Mutation.ExpectedVersion})
		var saved mutationReceipt
		e := tx.Get(ctx, "security.idempotency", key, &saved)
		if e == nil {
			if saved.Digest != want {
				return persistence.Conflict("credential delete retry changed payload")
			}
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		if e = v.repo.Delete(ctx, p.OwnerID, r.ID, r.Mutation.ExpectedVersion); e != nil {
			return e
		}
		if e = v.audit(ctx, p, "credential.delete", r.ID); e != nil {
			return e
		}
		return tx.Put(ctx, "security.idempotency", key, mutationReceipt{Digest: want})
	})
}
func (v *Vault) access(ctx context.Context, a Access, refresh bool) (EncryptedValue, error) {
	if e := a.Validate(); e != nil {
		return EncryptedValue{}, e
	}
	p, e := a.EffectivePrincipal()
	if e != nil {
		return EncryptedValue{}, e
	}
	var out EncryptedValue
	e = v.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := v.options.ValidatePrincipal(ctx, p); e != nil {
			return e
		}
		if p.TokenID != "" {
			scope := "credential.use"
			if refresh {
				scope = "credential.refresh"
			}
			if v.options.CheckScope == nil {
				return core.NewError(core.Forbidden, "credential scope unavailable")
			}
			if e := v.options.CheckScope(ctx, p, scope); e != nil {
				return e
			}
		}
		if v.options.AuthorizeAccess == nil {
			return core.NewError(core.Forbidden, "credential binding authorization unavailable")
		}
		if e := v.options.AuthorizeAccess(ctx, a); e != nil {
			return e
		}
		var err error
		out, err = v.repo.Get(ctx, p.OwnerID, a.CredentialID)
		if err != nil {
			return err
		}
		if !refresh && out.Metadata.ExpiresAt != nil && !v.options.Now().Before(*out.Metadata.ExpiresAt) {
			return core.NewError(core.Forbidden, "credential expired")
		}
		return nil
	})
	return out, e
}
func (v *Vault) WithOwnerSecret(ctx context.Context, p core.Principal, id core.ID, provider string, use func(core.Secret) error) error {
	if p.SessionID == "" || p.TokenID != "" || use == nil {
		return core.NewError(core.Forbidden, "owner session required")
	}
	sum := sha256.Sum256([]byte(id))
	lock := &v.sessionLocks[int(sum[0])%len(v.sessionLocks)]
	lock.Lock()
	defer lock.Unlock()

	var value EncryptedValue
	err := v.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := v.owner(ctx, p, "credential.use"); err != nil {
			return err
		}
		var err error
		value, err = v.repo.Get(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		if value.Metadata.Provider != provider || (value.Metadata.ExpiresAt != nil && !v.options.Now().Before(*value.Metadata.ExpiresAt)) {
			return core.NewError(core.Forbidden, "credential unavailable")
		}
		return nil
	})
	if err != nil {
		return err
	}
	secret, err := v.decrypt(value)
	if err != nil {
		return err
	}
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	return use(secret)
}

func (v *Vault) WithSecret(ctx context.Context, a Access, use func(core.Secret) error) error {
	if use == nil {
		return core.Invalid("callback", "secret consumer required")
	}
	secret, e := v.resolvedSecret(ctx, a)
	if e != nil {
		return e
	}
	if e = persistence.CheckContext(ctx); e != nil {
		return e
	}
	return use(secret)
}
func (v *Vault) resolvedSecret(ctx context.Context, a Access) (core.Secret, error) {
	// A bounded stripe lock covers read, rotation and atomic save, never inference.
	sum := sha256.Sum256([]byte(a.CredentialID))
	lock := &v.sessionLocks[int(sum[0])%len(v.sessionLocks)]
	lock.Lock()
	defer lock.Unlock()
	value, err := v.access(ctx, a, false)
	if err != nil {
		return core.Secret{}, err
	}
	secret, err := v.decrypt(value)
	if err != nil {
		return core.Secret{}, err
	}
	if v.options.ResolveProvider == nil {
		return secret, nil
	}
	updated, bearer, err := v.options.ResolveProvider(ctx, a, value.Metadata, secret)
	if err != nil {
		return core.Secret{}, err
	}
	if !updated.Empty() {
		err = v.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if _, err := v.access(ctx, a, true); err != nil {
				return err
			}
			encrypted, err := v.encrypt(value.Metadata, updated)
			if err != nil {
				return err
			}
			return v.repo.Save(ctx, encrypted, value.Metadata.Version)
		})
		if err != nil {
			return core.Secret{}, err
		}
	}
	return bearer, nil
}

func (v *Vault) Refresh(ctx context.Context, a Access) error {
	value, e := v.access(ctx, a, true)
	if e != nil {
		return e
	}
	if v.options.RefreshProvider == nil {
		return core.NewError(core.FailedPrecondition, "provider does not support refresh")
	}
	secret, e := v.decrypt(value)
	if e != nil {
		return e
	}
	updated, e := v.options.RefreshProvider(ctx, a, secret)
	if e != nil {
		return e
	}
	if updated.Empty() {
		return core.NewError(core.FailedPrecondition, "provider returned empty credential")
	}
	return v.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, e = v.access(ctx, a, true); e != nil {
			return e
		}
		encrypted, e := v.encrypt(value.Metadata, updated)
		if e != nil {
			return e
		}
		if e = v.repo.Save(ctx, encrypted, value.Metadata.Version); e != nil {
			return e
		}
		p, e := a.EffectivePrincipal()
		if e != nil {
			return e
		}
		return v.audit(ctx, p, "credential.refresh", a.CredentialID)
	})
}

var _ Manager = (*Vault)(nil)
var _ Broker = (*Vault)(nil)

func (v *Vault) audit(ctx context.Context, p core.Principal, action string, target core.ID) error {
	id := credentialID()
	record := struct {
		ID, OwnerID, SubjectID core.ID
		ActorKind              core.ActorKind
		Action                 string
		Target                 core.Object
		Outcome                string
		At                     time.Time
	}{id, p.OwnerID, p.SubjectID, p.Kind, action, core.Object{Kind: "credential", ID: target}, "succeeded", v.options.Now()}
	return v.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.audit", persistence.Key(p.OwnerID, id), record)
	})
}
