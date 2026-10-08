package auth

import (
	"context"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const loginLinksBucket = "auth.login-links"

type LoginLink struct {
	ID         core.ID
	Name       string
	TargetPath string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	UsedAt     *time.Time
	RevokedAt  *time.Time
}
type CreateLoginLinkRequest struct {
	Name         string
	TargetPath   string
	ExpiresIn    time.Duration
	Verification Verification
}
type LoginLinkIssued struct {
	Link  LoginLink
	Value core.Secret
}
type RedeemLoginLinkRequest struct {
	Token  core.Secret
	Client Client
}
type PreviewLoginLinkRequest struct {
	Token  core.Secret
	Client Client
}
type LoginLinkPreview struct {
	ExpiresAt time.Time `json:"expires_at"`
}
type LoginLinkResult struct {
	LoginResult
	TargetPath string
}
type storedLoginLink struct {
	Link      LoginLink
	AccountID core.ID
	Digest    string
}

// Only known WebUI routes may be stored as post-login destinations.
func validLoginTarget(path string) bool {
	switch path {
	case "/account/usage", "/settings/usage", "/account", "/account/theme", "/account/memory", "/account/security", "/assistants", "/plans", "/resources", "/settings", "/settings/theme", "/extensions", "/settings/memory", "/settings/security", "/workbench":
		return true
	}
	return false
}
func (m *Manager) CreateLoginLink(ctx context.Context, p core.Principal, req CreateLoginLinkRequest) (LoginLinkIssued, error) {
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 || !validLoginTarget(req.TargetPath) || req.ExpiresIn < time.Minute || req.ExpiresIn > 24*time.Hour {
		return LoginLinkIssued{}, core.Invalid("login_link", "name, known target and expiry between one minute and 24 hours required")
	}
	var out LoginLinkIssued
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.CheckVerification(ctx, p, req.Verification, "login_link.create"); err != nil {
			return err
		}
		now := m.options.Now()
		out = LoginLinkIssued{Link: LoginLink{ID: newID(), Name: strings.TrimSpace(req.Name), TargetPath: req.TargetPath, CreatedAt: now, ExpiresAt: now.Add(req.ExpiresIn)}, Value: core.NewSecret(randomSecret())}
		key := digest(out.Value.Reveal())
		if err := tx.Put(ctx, loginLinksBucket, key, storedLoginLink{Link: out.Link, AccountID: p.OwnerID, Digest: key}); err != nil {
			return err
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.login_link.create", "succeeded", core.Object{Kind: "login_link", ID: out.Link.ID})
	})
	if err != nil {
		return LoginLinkIssued{}, err
	}
	return out, nil
}
func (m *Manager) LoginLinks(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[LoginLink], error) {
	var out core.Page[LoginLink]
	err := m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		records, err := persistence.Page[storedLoginLink](ctx, tx.Store(), loginLinksBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("AccountID", p.OwnerID)}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "Link.ID", ThenDescending: true}, q, p.OwnerID, nil)
		if err != nil {
			return err
		}
		links := []LoginLink{}
		for _, record := range records.Items {
			if record.AccountID == p.OwnerID {
				links = append(links, record.Link)
			}
		}
		sort.Slice(links, func(i, j int) bool {
			if links[i].CreatedAt.Equal(links[j].CreatedAt) {
				return links[i].ID > links[j].ID
			}
			return links[i].CreatedAt.After(links[j].CreatedAt)
		})
		out = core.Page[LoginLink]{Items: links, NextCursor: records.NextCursor}
		return err
	})
	return out, err
}
func (m *Manager) RevokeLoginLink(ctx context.Context, p core.Principal, id core.ID) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		records, err := persistence.Select[storedLoginLink](ctx, tx.Store(), loginLinksBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("AccountID", p.OwnerID), persistence.Eq("Link.ID", id)}, Limit: 1})
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.AccountID != p.OwnerID || record.Link.ID != id {
				continue
			}
			if record.Link.RevokedAt != nil {
				return nil
			}
			now := m.options.Now()
			record.Link.RevokedAt = &now
			if err := tx.Put(ctx, loginLinksBucket, record.Digest, record); err != nil {
				return err
			}
			return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.login_link.revoke", "succeeded", core.Object{Kind: "login_link", ID: id})
		}
		return core.NewError(core.NotFound, "login link not found")
	})
}

// PreviewLoginLink reads only display metadata; it never consumes the link or issues a session.
func (m *Manager) PreviewLoginLink(ctx context.Context, req PreviewLoginLinkRequest) (LoginLinkPreview, error) {
	if err := m.limit(ctx, "login-link-preview-address:"+req.Client.Address, time.Minute); err != nil {
		return LoginLinkPreview{}, err
	}
	if req.Token.Empty() || len(req.Token.Reveal()) > 128 {
		return LoginLinkPreview{}, denied()
	}
	var out LoginLinkPreview
	err := m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var record storedLoginLink
		if err := tx.Get(ctx, loginLinksBucket, digest(req.Token.Reveal()), &record); err != nil {
			if persistence.Missing(err) {
				return denied()
			}
			return err
		}
		if record.Link.UsedAt != nil || record.Link.RevokedAt != nil || !m.options.Now().Before(record.Link.ExpiresAt) {
			return denied()
		}
		account, err := m.repo.AccountByID(ctx, record.AccountID)
		if err != nil {
			return err
		}
		if account.Account.ID != record.AccountID || account.Account.Disabled {
			return denied()
		}
		out.ExpiresAt = record.Link.ExpiresAt
		return nil
	})
	return out, err
}

func (m *Manager) RedeemLoginLink(ctx context.Context, req RedeemLoginLinkRequest) (LoginLinkResult, error) {
	if err := m.limit(ctx, "login-link-address:"+req.Client.Address, time.Minute); err != nil {
		return LoginLinkResult{}, err
	}
	if req.Token.Empty() || len(req.Token.Reveal()) > 128 {
		return LoginLinkResult{}, denied()
	}
	var out LoginLinkResult
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := digest(req.Token.Reveal())
		var record storedLoginLink
		if err := tx.Get(ctx, loginLinksBucket, key, &record); err != nil {
			if persistence.Missing(err) {
				return denied()
			}
			return err
		}
		now := m.options.Now()
		if record.Link.UsedAt != nil || record.Link.RevokedAt != nil || !now.Before(record.Link.ExpiresAt) {
			return denied()
		}
		account, err := m.repo.AccountByID(ctx, record.AccountID)
		if err != nil {
			return err
		}
		if account.Account.ID != record.AccountID || account.Account.Disabled {
			return denied()
		}
		record.Link.UsedAt = &now
		if err := tx.Put(ctx, loginLinksBucket, key, record); err != nil {
			return err
		}
		out.TargetPath = record.Link.TargetPath
		if account.Account.MFAEnabled {
			raw := randomSecret()
			expiry := now.Add(5 * time.Minute)
			if record.Link.ExpiresAt.Before(expiry) {
				expiry = record.Link.ExpiresAt
			}
			if err := m.repo.PutChallenge(ctx, StoredChallenge{Digest: digest(raw), AccountID: record.AccountID, Purpose: "login", ExpiresAt: expiry, LoginLinkDigest: key}); err != nil {
				return err
			}
			out.Challenge = &Challenge{ID: core.NewSecret(raw), ExpiresAt: expiry}
		} else {
			issued, err := m.issueSession(ctx, record.AccountID, req.Client)
			if err != nil {
				return err
			}
			out.Session = &issued
		}
		return m.audit(ctx, record.AccountID, record.AccountID, "auth.login_link.redeem", "succeeded", core.Object{Kind: "login_link", ID: record.Link.ID})
	})
	if err != nil {
		return LoginLinkResult{}, err
	}
	return out, nil
}
