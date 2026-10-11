package auth

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

func deviceDigest(id string) string {
	if id == "" {
		return ""
	}
	return digest(id)
}

func validateClient(c Client) error {
	if c.DeviceID != "" && !deviceIDPattern.MatchString(c.DeviceID) {
		return core.Invalid("device_id", "32–128 URL-safe random characters required")
	}
	switch c.ClientType {
	case "", "web", "ios", "android", "desktop", "cli":
	default:
		return core.Invalid("client_type", "unknown client type")
	}
	if len(c.DeviceName) > 128 || strings.ContainsFunc(c.DeviceName, unicode.IsControl) {
		return core.Invalid("device_name", "invalid device name")
	}
	return nil
}

func (m *Manager) deviceSession(ctx context.Context, owner core.ID, key string) (StoredSession, error) {
	if key == "" {
		return StoredSession{}, nil
	}
	rows, err := persistence.Select[StoredSession](ctx, m.store, "auth.sessions", persistence.Query{
		Where: []persistence.Filter{persistence.Eq("Session.AccountID", owner), persistence.Eq("DeviceDigest", key)},
		Limit: 1, Unordered: true,
	})
	if err != nil {
		return StoredSession{}, err
	}
	if len(rows) == 0 {
		return StoredSession{}, nil
	}
	return rows[0], nil
}

// Capture the device generation before authentication. MFA persists this
// snapshot, so completing an older challenge cannot replace a newer login.
func (m *Manager) prepareDevice(ctx context.Context, owner core.ID, c *Client) error {
	if err := validateClient(*c); err != nil {
		return err
	}
	c.deviceDigest = deviceDigest(c.DeviceID)
	old, err := m.deviceSession(ctx, owner, c.deviceDigest)
	c.expectedSession = old.Session.ID
	return err
}

func (m *Manager) issueSession(ctx context.Context, owner core.ID, c Client) (SessionIssued, error) {
	var out SessionIssued
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := m.deviceSession(ctx, owner, c.deviceDigest)
		if err != nil {
			return err
		}
		if old.Session.ID != c.expectedSession {
			return core.NewError(core.Conflict, "device signed in again; restart sign-in")
		}
		now := m.options.Now()
		created := now
		if old.Session.ID != "" {
			created = old.Session.CreatedAt
			if err := tx.Delete(ctx, "auth.sessions", persistence.Key(owner, old.Session.ID)); err != nil {
				return err
			}
		}
		name := strings.TrimSpace(c.DeviceName)
		if name == "" {
			name = "Unknown device"
		}
		out = SessionIssued{
			Session: Session{ID: newID(), AccountID: owner, DeviceName: name, ClientType: c.ClientType,
				CreatedAt: created, LastLoginAt: now, LastSeenAt: now, ExpiresAt: now.Add(m.options.SessionLifetime)},
			Token: core.NewSecret(randomSecret()), CSRF: core.NewSecret(randomSecret()),
		}
		if err := m.repo.SaveSession(ctx, StoredSession{Session: out.Session, DeviceDigest: c.deviceDigest,
			TokenDigest: digest(out.Token.Reveal()), CSRFDigest: digest(out.CSRF.Reveal())}); err != nil {
			return err
		}
		if err := tx.NotifyAfterCommit(ctx, principalSignal(owner)); err != nil {
			return err
		}
		return m.audit(ctx, owner, owner, "auth.login", "succeeded", core.Object{Kind: "session", ID: out.Session.ID})
	})
	if err != nil {
		return SessionIssued{}, err
	}
	return out, nil
}

func (m *Manager) activeSessionFilters(owner core.ID, now time.Time) []persistence.Filter {
	return []persistence.Filter{
		persistence.Eq("Session.AccountID", owner), persistence.Eq("RevokedAt", nil),
		{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(now)},
		{Field: "LastSeenAtKey", Op: ">", Value: persistence.TimeKey(now.Add(-m.options.SessionIdle))},
	}
}

func (m *Manager) activeSessions(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[Session], error) {
	rows, err := persistence.Page[StoredSession](ctx, m.store, "auth.sessions", persistence.Query{
		Where:   m.activeSessionFilters(p.OwnerID, m.options.Now()),
		OrderBy: "LastSeenAtKey", Descending: true, ThenBy: "Session.ID", ThenDescending: true,
	}, q, p.OwnerID, nil)
	out := core.Page[Session]{Items: []Session{}, NextCursor: rows.NextCursor}
	for _, row := range rows.Items {
		row.Session.Current = row.Session.ID == p.SessionID
		out.Items = append(out.Items, row.Session)
	}
	return out, err
}

func (m *Manager) RevokeOtherSessions(ctx context.Context, p core.Principal) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		now := m.options.Now()
		where := append(m.activeSessionFilters(p.OwnerID, now), persistence.Filter{Field: "Session.ID", Op: "!=", Value: p.SessionID})
		// Keyset batches keep memory bounded; the whole revocation commits atomically.
		after := ""
		for {
			rows, err := tx.Query(ctx, "auth.sessions", persistence.Query{Where: append(append([]persistence.Filter{}, where...), persistence.Filter{Field: "key", Op: ">", Value: after}), Limit: 500})
			if err != nil {
				return err
			}
			for _, row := range rows {
				s, err := persistence.Decode[StoredSession](row)
				if err != nil {
					return err
				}
				s.RevokedAt = &now
				if err := tx.Put(ctx, "auth.sessions", row.Key, s); err != nil {
					return err
				}
				after = row.Key
			}
			if len(rows) < 500 {
				break
			}
		}
		if err := tx.NotifyAfterCommit(ctx, principalSignal(p.OwnerID)); err != nil {
			return err
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.session.revoke_others", "succeeded", core.Object{Kind: "account", ID: p.OwnerID})
	})
}
