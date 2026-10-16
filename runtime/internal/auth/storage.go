package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type accountRecord struct {
	Account         Account
	PasswordHash    string
	EncryptedTOTP   []byte
	RecoveryDigests []string
	Version         core.Version
}

func encodeAccount(a StoredAccount) accountRecord {
	return accountRecord{a.Account, a.PasswordHash.Reveal(), a.EncryptedTOTP, a.RecoveryDigests, a.Version}
}
func decodeAccount(a accountRecord) StoredAccount {
	return StoredAccount{a.Account, core.NewSecret(a.PasswordHash), a.EncryptedTOTP, a.RecoveryDigests, a.Version}
}

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store} }

// Account is the local bootstrap account, never the current HTTP identity.
func (r *repository) Account(ctx context.Context) (StoredAccount, error) {
	bootstrap, err := persistence.Load[struct{ AccountID core.ID }](ctx, r.store, "auth.bootstrap", "owner")
	if err != nil {
		return StoredAccount{}, err
	}
	return r.AccountByID(ctx, bootstrap.AccountID)
}
func (r *repository) AccountByID(ctx context.Context, id core.ID) (StoredAccount, error) {
	a, err := persistence.Load[accountRecord](ctx, r.store, "auth.accounts", string(id))
	a.Account.Version = a.Version
	return decodeAccount(a), err
}

func (r *repository) AccountByUsername(ctx context.Context, username string) (StoredAccount, error) {
	rows, err := persistence.Select[accountRecord](ctx, r.store, "auth.accounts", persistence.Query{Where: []persistence.Filter{persistence.Eq("Account.Username", username)}, Limit: 1, Unordered: true})
	if err != nil {
		return StoredAccount{}, err
	}
	for _, a := range rows {
		if a.Account.Username == username {
			a.Account.Version = a.Version
			return decodeAccount(a), nil
		}
	}
	return StoredAccount{}, core.NewError(core.NotFound, "account not found")
}
func (r *repository) InsertAccount(ctx context.Context, a StoredAccount) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		existing, err := tx.Query(ctx, "auth.accounts", persistence.Query{Limit: 1})
		if err != nil {
			return err
		}
		rows, err := persistence.Select[accountRecord](ctx, tx.Store(), "auth.accounts", persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("key", string(a.Account.ID))}, {persistence.Eq("Account.Username", a.Account.Username)}}})
		if err != nil {
			return err
		}
		for _, old := range rows {
			if old.Account.ID == a.Account.ID || old.Account.Username == a.Account.Username {
				return persistence.Conflict("account already exists")
			}
		}
		if a.Account.GroupID == "" {
			if len(existing) == 0 {
				a.Account.GroupID = AdminGroupID
			} else {
				return core.Invalid("group", "user group required")
			}
		}
		a.Version = 1
		a.Account.Version = 1
		if err := ensureGroups(ctx, tx); err != nil {
			return err
		}
		if len(existing) == 0 {
			if err := tx.Put(ctx, "auth.bootstrap", "owner", struct{ AccountID core.ID }{a.Account.ID}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "auth.accounts", string(a.Account.ID), encodeAccount(a))
	})
}
func (r *repository) SaveAccount(ctx context.Context, a StoredAccount, v core.Version) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := r.AccountByID(ctx, a.Account.ID)
		if err != nil {
			return err
		}
		if old.Version != v {
			return persistence.Conflict("account version mismatch")
		}
		rows, err := persistence.Select[accountRecord](ctx, tx.Store(), "auth.accounts", persistence.Query{Where: []persistence.Filter{persistence.Eq("Account.Username", a.Account.Username)}})
		if err != nil {
			return err
		}
		for _, other := range rows {
			if other.Account.ID != a.Account.ID && other.Account.Username == a.Account.Username {
				return persistence.Conflict("username already exists")
			}
		}
		a.Version = v + 1
		a.Account.Version = a.Version
		if err := tx.Put(ctx, "auth.accounts", string(a.Account.ID), encodeAccount(a)); err != nil {
			return err
		}
		if old.Account.Disabled != a.Account.Disabled {
			if err := tx.NotifyAfterCommit(ctx, AccountStateSignal(a.Account.ID)); err != nil {
				return err
			}
		}
		return tx.NotifyAfterCommit(ctx, principalSignal(a.Account.ID))
	})
}
func (r *repository) FindSession(ctx context.Context, digest string) (StoredSession, error) {
	items, e := persistence.Select[StoredSession](ctx, r.store, "auth.sessions", persistence.Query{Where: []persistence.Filter{persistence.Eq("TokenDigest", digest)}, Limit: 1, Unordered: true})
	if e != nil {
		return StoredSession{}, e
	}
	for _, s := range items {
		if s.TokenDigest == digest {
			return s, nil
		}
	}
	return StoredSession{}, core.NewError(core.NotFound, "session not found")
}
func (r *repository) SaveSession(ctx context.Context, s StoredSession) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "auth.sessions", persistence.Key(s.Session.AccountID, s.Session.ID), s)
	})
}
func (r *repository) Sessions(ctx context.Context, owner core.ID, p core.PageRequest) (core.Page[Session], error) {
	rows, err := persistence.Page[StoredSession](ctx, r.store, "auth.sessions", persistence.Query{Where: []persistence.Filter{persistence.Eq("Session.AccountID", owner)}}, p, owner, nil)
	out := core.Page[Session]{Items: []Session{}, NextCursor: rows.NextCursor}
	for _, row := range rows.Items {
		out.Items = append(out.Items, row.Session)
	}
	return out, err
}

func (r *repository) RevokeSession(ctx context.Context, owner, id core.ID, at time.Time) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var s StoredSession
		key := persistence.Key(owner, id)
		if e := tx.Get(ctx, "auth.sessions", key, &s); e != nil {
			return e
		}
		s.RevokedAt = &at
		if err := tx.Put(ctx, "auth.sessions", key, s); err != nil {
			return err
		}
		return tx.NotifyAfterCommit(ctx, principalSignal(owner))
	})
}
func (r *repository) FindToken(ctx context.Context, digest string) (StoredToken, error) {
	items, e := persistence.Select[StoredToken](ctx, r.store, "auth.tokens", persistence.Query{Where: []persistence.Filter{persistence.Eq("Digest", digest)}, Limit: 1, Unordered: true})
	if e != nil {
		return StoredToken{}, e
	}
	for _, s := range items {
		if s.Digest == digest {
			return s, nil
		}
	}
	return StoredToken{}, core.NewError(core.NotFound, "token not found")
}
func (r *repository) SaveToken(ctx context.Context, s StoredToken) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "auth.tokens", persistence.Key(s.AccountID, s.Token.ID), s)
	})
}
func (r *repository) Tokens(ctx context.Context, owner core.ID, p core.PageRequest) (core.Page[Token], error) {
	rows, err := persistence.Page[StoredToken](ctx, r.store, "auth.tokens", persistence.Query{Where: []persistence.Filter{persistence.Eq("AccountID", owner)}}, p, owner, nil)
	out := core.Page[Token]{Items: []Token{}, NextCursor: rows.NextCursor}
	for _, row := range rows.Items {
		out.Items = append(out.Items, row.Token)
	}
	return out, err
}

func (r *repository) RevokeToken(ctx context.Context, owner, id core.ID, at time.Time) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var s StoredToken
		key := persistence.Key(owner, id)
		if e := tx.Get(ctx, "auth.tokens", key, &s); e != nil {
			return e
		}
		s.RevokedAt = &at
		if err := tx.Put(ctx, "auth.tokens", key, s); err != nil {
			return err
		}
		return tx.NotifyAfterCommit(ctx, principalSignal(owner))
	})
}
func (r *repository) RevokeAll(ctx context.Context, owner core.ID, at time.Time) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		sessions, e := persistence.Select[StoredSession](ctx, tx.Store(), "auth.sessions", persistence.Query{Where: []persistence.Filter{persistence.Eq("Session.AccountID", owner)}})
		if e != nil {
			return e
		}
		for _, s := range sessions {
			if s.Session.AccountID == owner {
				s.RevokedAt = &at
				if e = tx.Put(ctx, "auth.sessions", persistence.Key(owner, s.Session.ID), s); e != nil {
					return e
				}
			}
		}
		tokens, e := persistence.Select[StoredToken](ctx, tx.Store(), "auth.tokens", persistence.Query{Where: []persistence.Filter{persistence.Eq("AccountID", owner)}})
		if e != nil {
			return e
		}
		for _, s := range tokens {
			if s.AccountID == owner {
				s.RevokedAt = &at
				if e = tx.Put(ctx, "auth.tokens", persistence.Key(owner, s.Token.ID), s); e != nil {
					return e
				}
			}
		}
		links, e := persistence.Select[storedLoginLink](ctx, tx.Store(), loginLinksBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("AccountID", owner)}})
		if e != nil {
			return e
		}
		for _, link := range links {
			if link.AccountID == owner {
				link.Link.RevokedAt = &at
				if e = tx.Put(ctx, loginLinksBucket, link.Digest, link); e != nil {
					return e
				}
			}
		}

		return tx.NotifyAfterCommit(ctx, principalSignal(owner))
	})
}
func (r *repository) PutChallenge(ctx context.Context, c StoredChallenge) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "auth.challenges", c.Digest, c) })
}
func (r *repository) ConsumeChallenge(ctx context.Context, digest string, now time.Time) (StoredChallenge, error) {
	var c StoredChallenge
	e := r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := tx.Get(ctx, "auth.challenges", digest, &c); e != nil {
			return e
		}
		if !now.Before(c.ExpiresAt) {
			return core.NewError(core.Unauthenticated, "challenge expired")
		}
		return tx.Delete(ctx, "auth.challenges", digest)
	})
	return c, e
}
func (r *repository) ConsumeRecoveryCode(ctx context.Context, owner core.ID, digest string) (bool, error) {
	ok := false
	e := r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, e := r.AccountByID(ctx, owner)
		if persistence.Missing(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if a.Account.ID != owner {
			return nil
		}
		for i, d := range a.RecoveryDigests {
			if d == digest {
				a.RecoveryDigests = append(a.RecoveryDigests[:i], a.RecoveryDigests[i+1:]...)
				ok = true
				return r.SaveAccount(ctx, a, a.Version)
			}
		}
		return nil
	})
	return ok, e
}
func pageSlice[T any](items []T, p core.PageRequest, binding string) (core.Page[T], error) {
	p, e := p.Normalize()
	if e != nil {
		return core.Page[T]{}, e
	}
	h := sha256.Sum256([]byte(binding))
	tag := hex.EncodeToString(h[:])
	start := 0
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		if e != nil {
			return core.Page[T]{}, core.Invalid("cursor", "invalid")
		}
		parts := strings.Split(string(b), ":")
		if len(parts) != 2 || parts[0] != tag {
			return core.Page[T]{}, core.Invalid("cursor", "wrong query")
		}
		start, e = strconv.Atoi(parts[1])
		if e != nil || start < 0 || start > len(items) {
			return core.Page[T]{}, core.Invalid("cursor", "invalid position")
		}
	}
	end := start + p.Limit
	if end > len(items) {
		end = len(items)
	}
	out := core.Page[T]{Items: items[start:end]}
	if end < len(items) {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d", tag, end)))
	}
	return out, nil
}
