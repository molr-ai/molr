package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type PrincipalValidator interface {
	ValidatePrincipal(context.Context, core.Principal) error
}
type Options struct {
	AfterAccessChange      func(context.Context)
	ValidateResources      func(context.Context, core.Principal, []ResourceReference) error
	Now                    func() time.Time
	MasterKey              []byte
	Origin                 string
	AllowedOrigins         []string
	LocalHTTP              bool
	LANHTTP                bool
	SessionLifetime        time.Duration
	SessionIdle            time.Duration
	VerificationLifetime   time.Duration
	LoginAttemptsPerMinute int
	ValidateAssistant      func(context.Context, core.Principal) error
}
type Manager struct {
	store     persistence.Store
	repo      Repository
	options   Options
	dummyHash string
}

func NewService(store persistence.Store, o Options) (*Manager, error) {
	if store == nil || len(o.MasterKey) != 32 {
		return nil, core.Invalid("auth", "store and 32-byte master key required")
	}
	if o.LoginAttemptsPerMinute <= 0 {
		o.LoginAttemptsPerMinute = 5
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.SessionLifetime <= 0 {
		o.SessionLifetime = 30 * 24 * time.Hour
	}
	if o.SessionIdle <= 0 {
		o.SessionIdle = 24 * time.Hour
	}
	if o.VerificationLifetime <= 0 {
		o.VerificationLifetime = 5 * time.Minute
	}
	u, e := url.Parse(o.Origin)
	if e != nil || !validBrowserOrigin(u, o.LocalHTTP, o.LANHTTP) || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, core.Invalid("origin", "HTTPS origin required")
	}
	o.AllowedOrigins = append([]string(nil), o.AllowedOrigins...)
	for _, origin := range o.AllowedOrigins {
		u, e := url.Parse(origin)
		if e != nil || !validBrowserOrigin(u, o.LocalHTTP, o.LANHTTP) || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "*") {
			return nil, core.Invalid("allowed_origins", "explicit HTTPS origins required")
		}
	}
	o.MasterKey = append([]byte(nil), o.MasterKey...)
	dummy, e := hashPassword(randomSecret())
	if e != nil {
		return nil, e
	}
	m := &Manager{store: store, repo: NewRepository(store), options: o, dummyHash: dummy}
	if err := m.checkAccountStorage(context.Background()); err != nil {
		return nil, err
	}
	return m, nil
}
func denied() error { return core.NewError(core.Unauthenticated, "authentication required") }
func (m *Manager) owner(ctx context.Context, p core.Principal) error {
	if e := p.RequireOwner(); e != nil {
		return e
	}
	return m.ValidatePrincipal(ctx, p)
}
func (m *Manager) ownerSession(ctx context.Context, p core.Principal) error {
	if e := m.owner(ctx, p); e != nil {
		return e
	}
	if p.SessionID == "" {
		return core.NewError(core.Forbidden, "owner session required")
	}
	return nil
}
func (m *Manager) ValidatePrincipal(ctx context.Context, p core.Principal) error {
	if e := p.Validate(); e != nil {
		return e
	}
	return m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			if persistence.Missing(e) {
				return denied()
			}
			return e
		}
		if a.Account.ID != p.OwnerID || a.Account.Disabled {
			return denied()
		}
		if p.Kind == core.ActorAssistant {
			if m.options.ValidateAssistant == nil {
				return core.NewError(core.Forbidden, "assistant validation unavailable")
			}
			return m.options.ValidateAssistant(ctx, p)
		}
		now := m.options.Now()
		if p.SessionID != "" {
			var s StoredSession
			if e := tx.Get(ctx, "auth.sessions", persistence.Key(p.OwnerID, p.SessionID), &s); e != nil {
				if persistence.Missing(e) {
					return denied()
				}
				return e
			}
			if s.RevokedAt != nil || !now.Before(s.Session.ExpiresAt) || !now.Before(s.Session.LastSeenAt.Add(m.options.SessionIdle)) {
				return denied()
			}
			return nil
		}
		var token StoredToken
		if e := tx.Get(ctx, "auth.tokens", persistence.Key(p.OwnerID, p.TokenID), &token); e != nil {
			if persistence.Missing(e) {
				return denied()
			}
			return e
		}
		if token.RevokedAt != nil || !now.Before(token.Token.ExpiresAt) {
			return denied()
		}
		return nil
	})
}
func (m *Manager) CheckScope(ctx context.Context, p core.Principal, scope string) error {
	if e := m.ValidatePrincipal(ctx, p); e != nil {
		return e
	}
	if p.Kind != core.ActorOwner {
		return core.NewError(core.Forbidden, "owner scope required")
	}
	if p.SessionID != "" {
		return nil
	}
	token, e := persistence.Load[StoredToken](ctx, m.store, "auth.tokens", persistence.Key(p.OwnerID, p.TokenID))
	if e != nil {
		return e
	}
	for _, allowed := range token.Token.Scopes {
		if allowed == scope || allowed == "*" {
			return nil
		}
	}
	return core.NewError(core.Forbidden, "token scope denied")
}
func (m *Manager) Initialized(ctx context.Context) (bool, error) {
	_, e := m.repo.Account(ctx)
	if persistence.Missing(e) {
		return false, nil
	}
	return e == nil, e
}
func (m *Manager) Initialize(ctx context.Context, r InitializeRequest) (Account, error) {
	if e := persistence.CheckContext(ctx); e != nil {
		return Account{}, e
	}
	if strings.TrimSpace(r.Username) != r.Username || r.Username == "" || len(r.Username) > 128 {
		return Account{}, core.Invalid("username", "non-empty username required")
	}
	hash, e := hashPassword(r.Password.Reveal())
	if e != nil {
		return Account{}, e
	}
	a := Account{ID: newID(), Username: r.Username, DisplayName: r.Username, GroupID: AdminGroupID, Version: 1, CreatedAt: m.options.Now()}
	e = m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if exists, err := m.Initialized(ctx); err != nil {
			return err
		} else if exists {
			return persistence.Conflict("owner already initialized")
		}
		if err := m.repo.InsertAccount(ctx, StoredAccount{Account: a, PasswordHash: core.NewSecret(hash), Version: 1}); err != nil {
			return err
		}
		return m.audit(ctx, a.ID, a.ID, "auth.initialize", "succeeded", core.Object{Kind: "account", ID: a.ID})
	})
	return a, e
}
func (m *Manager) Recover(ctx context.Context, r RecoveryRequest) error {
	hash, e := hashPassword(r.Password.Reveal())
	if e != nil {
		return e
	}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, e := m.repo.Account(ctx)
		if e != nil {
			return e
		}
		a.PasswordHash = core.NewSecret(hash)
		if r.ResetMFA {
			a.Account.MFAEnabled = false
			a.EncryptedTOTP = nil
			a.RecoveryDigests = nil
		}
		if e = m.repo.SaveAccount(ctx, a, a.Version); e != nil {
			return e
		}
		if e = m.repo.RevokeAll(ctx, a.Account.ID, m.options.Now()); e != nil {
			return e
		}
		records, e := tx.Scan(ctx, "auth.challenges")
		if e != nil {
			return e
		}
		for _, r := range records {
			c, e := persistence.Decode[StoredChallenge](r)
			if e != nil {
				return e
			}
			if c.AccountID == a.Account.ID {
				if e = tx.Delete(ctx, "auth.challenges", r.Key); e != nil {
					return e
				}
			}
		}
		return m.audit(ctx, a.Account.ID, a.Account.ID, "auth.recover", "succeeded", core.Object{Kind: "account", ID: a.Account.ID})
	})
}

type loginLimit struct {
	Window   time.Time
	Attempts int
}

func (m *Manager) limit(ctx context.Context, key string, window time.Duration) error {
	var result error
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var l loginLimit
		e := tx.Get(ctx, "auth.limits", digest(key), &l)
		if e != nil && !persistence.Missing(e) {
			return e
		}
		now := m.options.Now()
		if !now.Before(l.Window.Add(window)) {
			l = loginLimit{Window: now}
		}
		l.Attempts++
		if l.Attempts > m.options.LoginAttemptsPerMinute {
			result = core.NewError(core.RateLimited, "authentication rate limited")
		}
		return tx.Put(ctx, "auth.limits", digest(key), l)
	})
	if e != nil {
		return e
	}
	return result
}
func (m *Manager) Login(ctx context.Context, r LoginRequest) (LoginResult, error) {
	if e := persistence.CheckContext(ctx); e != nil {
		return LoginResult{}, e
	}
	if e := m.limit(ctx, "login-account:"+r.Username, time.Minute); e != nil {
		return LoginResult{}, e
	}
	// A long address window quarantines an abusive source for ten minutes so it
	// cannot keep the account bucket saturated and lock out the owner.
	if e := m.limit(ctx, "login-address:"+r.Client.Address, 10*time.Minute); e != nil {
		return LoginResult{}, e
	}
	a, e := m.repo.AccountByUsername(ctx, r.Username)
	if e != nil {
		if !persistence.Missing(e) {
			return LoginResult{}, e
		}
		_ = checkPassword(m.dummyHash, r.Password.Reveal())
		return LoginResult{}, denied()
	}
	if r.Username != a.Account.Username || a.Account.Disabled {
		_ = checkPassword(m.dummyHash, r.Password.Reveal())
		return LoginResult{}, denied()
	}
	if !checkPassword(a.PasswordHash.Reveal(), r.Password.Reveal()) {
		if err := m.audit(ctx, a.Account.ID, "", "auth.login", "denied", core.Object{Kind: "account", ID: a.Account.ID}); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, denied()
	}
	var out LoginResult
	e = m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, e := m.repo.AccountByID(ctx, a.Account.ID)
		if e != nil {
			return e
		}
		if current.Version != a.Version {
			return denied()
		}
		if a.Account.MFAEnabled {
			raw := randomSecret()
			expiry := m.options.Now().Add(5 * time.Minute)
			if e = m.repo.PutChallenge(ctx, StoredChallenge{Digest: digest(raw), AccountID: a.Account.ID, Purpose: "login", ExpiresAt: expiry}); e != nil {
				return e
			}
			out.Challenge = &Challenge{ID: core.NewSecret(raw), ExpiresAt: expiry}
			return nil
		}
		s, e := m.issueSession(ctx, a.Account.ID, r.Client)
		out.Session = &s
		return e
	})
	return out, e
}
func (m *Manager) issueSession(ctx context.Context, owner core.ID, c Client) (SessionIssued, error) {
	now := m.options.Now()
	out := SessionIssued{Session: Session{ID: newID(), AccountID: owner, DeviceName: c.DeviceName, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(m.options.SessionLifetime)}, Token: core.NewSecret(randomSecret()), CSRF: core.NewSecret(randomSecret())}
	e := m.repo.SaveSession(ctx, StoredSession{Session: out.Session, TokenDigest: digest(out.Token.Reveal()), CSRFDigest: digest(out.CSRF.Reveal())})
	if e == nil {
		e = m.audit(ctx, owner, owner, "auth.login", "succeeded", core.Object{Kind: "session", ID: out.Session.ID})
	}
	return out, e
}
func (m *Manager) Authenticate(ctx context.Context, a Authentication) (core.Principal, error) {
	if e := a.Validate(); e != nil {
		return core.Principal{}, e
	}
	var p core.Principal
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if !a.Session.Empty() {
			s, e := m.repo.FindSession(ctx, digest(a.Session.Reveal()))
			if e != nil {
				if persistence.Missing(e) {
					return denied()
				}
				return e
			}
			p = core.Principal{OwnerID: s.Session.AccountID, SubjectID: s.Session.AccountID, Kind: core.ActorOwner, SessionID: s.Session.ID}
			if e = m.ValidatePrincipal(ctx, p); e != nil {
				return e
			}
			s.Session.LastSeenAt = m.options.Now()
			return m.repo.SaveSession(ctx, s)
		}
		s, e := m.repo.FindToken(ctx, digest(a.Bearer.Reveal()))
		if e != nil {
			if persistence.Missing(e) {
				return denied()
			}
			return e
		}
		p = core.Principal{OwnerID: s.AccountID, SubjectID: s.AccountID, Kind: core.ActorOwner, TokenID: s.Token.ID}
		if e = m.ValidatePrincipal(ctx, p); e != nil {
			return e
		}
		now := m.options.Now()
		s.Token.LastUsedAt = &now
		return m.repo.SaveToken(ctx, s)
	})
	if e != nil {
		return core.Principal{}, e
	}
	return p, nil
}
func (m *Manager) verifyMFA(ctx context.Context, a StoredAccount, code core.Secret) error {
	secret, e := openSeal(m.options.MasterKey, a.EncryptedTOTP, "totp:"+string(a.Account.ID))
	if e != nil {
		return e
	}
	if validTOTP(string(secret), code.Reveal(), m.options.Now()) {
		return m.consumeTOTP(ctx, a.Account.ID, string(secret), code.Reveal())
	}
	ok, e := m.repo.ConsumeRecoveryCode(ctx, a.Account.ID, digest(code.Reveal()))
	if e != nil {
		return e
	}
	if !ok {
		return denied()
	}
	return nil
}
func (m *Manager) CompleteMFA(ctx context.Context, r MFAChallengeResponse) (SessionIssued, error) {
	var result SessionIssued
	var failure error
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var c StoredChallenge
		if e := tx.Get(ctx, "auth.challenges", digest(r.Challenge.Reveal()), &c); e != nil {
			if persistence.Missing(e) {
				return denied()
			}
			return e
		}
		if c.Purpose != "login" || !m.options.Now().Before(c.ExpiresAt) || c.Attempts >= 5 {
			return denied()
		}
		if c.LoginLinkDigest != "" {
			var link storedLoginLink
			if e := tx.Get(ctx, loginLinksBucket, c.LoginLinkDigest, &link); e != nil {
				return denied()
			}
			if link.AccountID != c.AccountID || link.Link.UsedAt == nil || link.Link.RevokedAt != nil || !m.options.Now().Before(link.Link.ExpiresAt) {
				return denied()
			}
		}

		c.Attempts++
		a, e := m.repo.AccountByID(ctx, c.AccountID)
		if e != nil {
			return e
		}
		if a.Account.ID != c.AccountID || a.Account.Disabled || !a.Account.MFAEnabled {
			return denied()
		}
		if e = m.verifyMFA(ctx, a, r.Code); e != nil {
			failure = e
			return tx.Put(ctx, "auth.challenges", c.Digest, c)
		}
		if e = tx.Delete(ctx, "auth.challenges", c.Digest); e != nil {
			return e
		}
		result, e = m.issueSession(ctx, a.Account.ID, r.Client)
		return e
	})
	if e != nil {
		return SessionIssued{}, e
	}
	return result, failure
}
func (m *Manager) Reauthenticate(ctx context.Context, p core.Principal, r ReauthenticateRequest) (Verification, error) {
	if e := m.ownerSession(ctx, p); e != nil {
		return Verification{}, e
	}
	if r.Purpose == "" || len(r.Purpose) > 128 {
		return Verification{}, core.Invalid("purpose", "required")
	}
	if e := m.limit(ctx, "reauth:"+string(p.SessionID), time.Minute); e != nil {
		return Verification{}, e
	}
	var out Verification
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.ownerSession(ctx, p); e != nil {
			return e
		}
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			return e
		}
		if !checkPassword(a.PasswordHash.Reveal(), r.Password.Reveal()) {
			return denied()
		}
		if a.Account.MFAEnabled {
			if e = m.verifyMFA(ctx, a, r.MFACode); e != nil {
				return e
			}
		}
		raw := randomSecret()
		out = Verification{Token: core.NewSecret(raw)}
		return m.repo.PutChallenge(ctx, StoredChallenge{Digest: digest(raw), AccountID: p.OwnerID, SessionID: p.SessionID, Purpose: "verify:" + r.Purpose, ExpiresAt: m.options.Now().Add(m.options.VerificationLifetime)})
	})
	return out, e
}
func (m *Manager) CheckVerification(ctx context.Context, p core.Principal, v Verification, purpose string) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.ownerSession(ctx, p); e != nil {
			return e
		}
		if v.Token.Empty() {
			account, err := m.repo.AccountByID(ctx, p.OwnerID)
			if err != nil {
				return err
			}
			if !account.Account.ReauthenticationEnabled {
				for _, optional := range optionalVerificationPurposes {
					if purpose == optional {
						return nil
					}
				}
			}
			return core.NewError(core.FailedPrecondition, "identity verification required")
		}
		var c StoredChallenge
		if e := tx.Get(ctx, "auth.challenges", digest(v.Token.Reveal()), &c); e != nil {
			if persistence.Missing(e) {
				return denied()
			}
			return e
		}
		if c.AccountID != p.OwnerID || c.SessionID != p.SessionID || c.Purpose != "verify:"+purpose {
			return denied()
		}
		_, e := m.repo.ConsumeChallenge(ctx, c.Digest, m.options.Now())
		return e
	})
}
func (m *Manager) SecurityStatus(ctx context.Context, p core.Principal) (SecurityStatus, error) {
	var status SecurityStatus
	err := m.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		account, err := m.repo.AccountByID(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		status.MFAEnabled = account.Account.MFAEnabled
		status.ReauthenticationEnabled = account.Account.ReauthenticationEnabled
		status.Version = account.Version
		status.UnprotectedPurposes = []string{}
		if !account.Account.ReauthenticationEnabled {
			status.UnprotectedPurposes = append(status.UnprotectedPurposes, optionalVerificationPurposes...)
		}
		return nil
	})
	return status, err
}

var optionalVerificationPurposes = []string{"credential.put", "credential.delete", "export"}

func (m *Manager) SetReauthentication(ctx context.Context, p core.Principal, req ReauthenticationSettings) (SecurityStatus, error) {
	var result SecurityStatus
	err := m.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := m.CheckVerification(ctx, p, req.Verification, "security.update"); err != nil {
			return err
		}
		account, err := m.repo.AccountByID(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		if account.Version != req.ExpectedVersion {
			return core.NewError(core.Conflict, "security settings changed")
		}
		account.Account.ReauthenticationEnabled = req.Enabled
		if err := m.repo.SaveAccount(ctx, account, req.ExpectedVersion); err != nil {
			return err
		}
		if err := m.audit(ctx, p.OwnerID, p.SubjectID, "auth.reauthentication.change", "succeeded", core.Object{Kind: "account", ID: p.OwnerID}); err != nil {
			return err
		}
		result, err = m.SecurityStatus(ctx, p)
		return err
	})
	return result, err
}

func (m *Manager) Sessions(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[Session], error) {
	if e := m.ownerSession(ctx, p); e != nil {
		return core.Page[Session]{}, e
	}
	return m.repo.Sessions(ctx, p.OwnerID, q)
}
func (m *Manager) RevokeSession(ctx context.Context, p core.Principal, id core.ID) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.ownerSession(ctx, p); e != nil {
			return e
		}
		if e := m.repo.RevokeSession(ctx, p.OwnerID, id, m.options.Now()); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.session.revoke", "succeeded", core.Object{Kind: "session", ID: id})
	})
}
func (m *Manager) Logout(ctx context.Context, p core.Principal) error {
	if e := m.ownerSession(ctx, p); e != nil {
		return e
	}
	return m.RevokeSession(ctx, p, p.SessionID)
}
func (m *Manager) ChangePassword(ctx context.Context, p core.Principal, r ChangePasswordRequest) error {
	if e := m.ownerSession(ctx, p); e != nil {
		return e
	}
	hash, e := hashPassword(r.NewPassword.Reveal())
	if e != nil {
		return e
	}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.CheckVerification(ctx, p, r.Verification, "password.change"); e != nil {
			return e
		}
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			return e
		}
		a.PasswordHash = core.NewSecret(hash)
		if e = m.repo.SaveAccount(ctx, a, a.Version); e != nil {
			return e
		}
		if e := m.repo.RevokeAll(ctx, p.OwnerID, m.options.Now()); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.password.change", "succeeded", core.Object{Kind: "account", ID: p.OwnerID})
	})
}
func (m *Manager) BeginMFAEnrollment(ctx context.Context, p core.Principal, v Verification) (MFAEnrollment, error) {
	var out MFAEnrollment
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.CheckVerification(ctx, p, v, "mfa.enroll"); e != nil {
			return e
		}
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			return e
		}
		if a.Account.MFAEnabled {
			return core.NewError(core.FailedPrecondition, "MFA already enabled")
		}
		key := make([]byte, 20)
		if _, e = rand.Read(key); e != nil {
			return e
		}
		secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
		id := newID()
		expiry := m.options.Now().Add(5 * time.Minute)
		encrypted, e := seal(m.options.MasterKey, []byte(secret), "enrollment:"+string(id))
		if e != nil {
			return e
		}
		if e = m.repo.PutChallenge(ctx, StoredChallenge{Digest: string(id), AccountID: p.OwnerID, SessionID: p.SessionID, Purpose: "enrollment", ExpiresAt: expiry, EncryptedPayload: encrypted}); e != nil {
			return e
		}
		u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/Molr:" + a.Account.Username}
		q := u.Query()
		q.Set("secret", secret)
		q.Set("issuer", "Molr")
		q.Set("algorithm", "SHA1")
		q.Set("digits", "6")
		q.Set("period", "30")
		u.RawQuery = q.Encode()
		out = MFAEnrollment{ID: id, ProvisioningURI: core.NewSecret(u.String()), ExpiresAt: expiry}
		return nil
	})
	return out, e
}
func (m *Manager) ConfirmMFAEnrollment(ctx context.Context, p core.Principal, r ConfirmMFARequest) (RecoveryCodes, error) {
	if e := m.ownerSession(ctx, p); e != nil {
		return RecoveryCodes{}, e
	}
	if e := m.limit(ctx, "mfa-enrollment:"+string(p.SessionID), time.Minute); e != nil {
		return RecoveryCodes{}, e
	}
	var out RecoveryCodes
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		// The short-lived, session-bound enrollment was created after password
		// verification; confirming its TOTP does not require a second password.
		c, e := m.repo.ConsumeChallenge(ctx, string(r.EnrollmentID), m.options.Now())
		if e != nil {
			return e
		}
		if c.AccountID != p.OwnerID || c.SessionID != p.SessionID || c.Purpose != "enrollment" {
			return denied()
		}
		secret, e := openSeal(m.options.MasterKey, c.EncryptedPayload, "enrollment:"+string(r.EnrollmentID))
		if e != nil {
			return e
		}
		if err := m.consumeTOTP(ctx, p.OwnerID, string(secret), r.Code.Reveal()); err != nil {
			return err
		}
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			return e
		}
		a.EncryptedTOTP, e = seal(m.options.MasterKey, secret, "totp:"+string(p.OwnerID))
		if e != nil {
			return e
		}
		a.Account.MFAEnabled = true
		a.RecoveryDigests = nil
		for i := 0; i < 10; i++ {
			raw := randomSecret()
			out.Codes = append(out.Codes, core.NewSecret(raw))
			a.RecoveryDigests = append(a.RecoveryDigests, digest(raw))
		}
		if e := m.repo.SaveAccount(ctx, a, a.Version); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.mfa.change", "succeeded", core.Object{Kind: "account", ID: p.OwnerID})
	})
	return out, e
}
func (m *Manager) DisableMFA(ctx context.Context, p core.Principal, v Verification) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.CheckVerification(ctx, p, v, "mfa.disable"); e != nil {
			return e
		}
		a, e := m.repo.AccountByID(ctx, p.OwnerID)
		if e != nil {
			return e
		}
		a.Account.MFAEnabled = false
		a.EncryptedTOTP = nil
		a.RecoveryDigests = nil
		if e := m.repo.SaveAccount(ctx, a, a.Version); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.mfa.change", "succeeded", core.Object{Kind: "account", ID: p.OwnerID})
	})
}
func (m *Manager) CreateToken(ctx context.Context, p core.Principal, r CreateTokenRequest) (TokenIssued, error) {
	if r.Name == "" || len(r.Name) > 128 || len(r.Scopes) == 0 || !r.ExpiresAt.After(m.options.Now()) || r.ExpiresAt.After(m.options.Now().Add(366*24*time.Hour)) {
		return TokenIssued{}, core.Invalid("token", "name, scopes and bounded expiry required")
	}
	for _, scope := range r.Scopes {
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n") {
			return TokenIssued{}, core.Invalid("scope", "invalid scope")
		}
	}
	var out TokenIssued
	e := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.CheckVerification(ctx, p, r.Verification, "token.create"); e != nil {
			return e
		}
		out = TokenIssued{Token: Token{ID: newID(), Name: r.Name, Scopes: append([]string(nil), r.Scopes...), CreatedAt: m.options.Now(), ExpiresAt: r.ExpiresAt}, Value: core.NewSecret(randomSecret())}
		if e := m.repo.SaveToken(ctx, StoredToken{Token: out.Token, AccountID: p.OwnerID, Digest: digest(out.Value.Reveal())}); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.token.create", "succeeded", core.Object{Kind: "token", ID: out.Token.ID})
	})
	return out, e
}
func (m *Manager) Tokens(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[Token], error) {
	if e := m.ownerSession(ctx, p); e != nil {
		return core.Page[Token]{}, e
	}
	return m.repo.Tokens(ctx, p.OwnerID, q)
}
func (m *Manager) RevokeToken(ctx context.Context, p core.Principal, id core.ID) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.ownerSession(ctx, p); e != nil {
			return e
		}
		if e := m.repo.RevokeToken(ctx, p.OwnerID, id, m.options.Now()); e != nil {
			return e
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "auth.token.revoke", "succeeded", core.Object{Kind: "token", ID: id})
	})
}

// PreLoginCSRF issues an expiring challenge-bound browser token before login.
// The adapter sends Challenge.ID in a Secure HttpOnly cookie and the distinct
// token to the same-origin client; neither challenge authenticates an account.
func (m *Manager) PreLoginCSRF(ctx context.Context) (Challenge, core.Secret, error) {
	raw := randomSecret()
	token := randomSecret()
	c := Challenge{ID: core.NewSecret(raw), ExpiresAt: m.options.Now().Add(5 * time.Minute)}
	e := m.repo.PutChallenge(ctx, StoredChallenge{Digest: digest(raw), Purpose: "csrf", ExpiresAt: c.ExpiresAt, EncryptedPayload: []byte(digest(token))})
	return c, core.NewSecret(token), e
}
func (m *Manager) CheckCSRF(ctx context.Context, a Authentication, token core.Secret, origin string) error {
	allowed := origin == m.options.Origin
	for _, configured := range m.options.AllowedOrigins {
		if origin == configured {
			allowed = true
		}
	}
	if !allowed || token.Empty() || a.Session.Empty() || !a.Bearer.Empty() {
		return core.NewError(core.Forbidden, "CSRF check failed")
	}
	return m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		s, e := m.repo.FindSession(ctx, digest(a.Session.Reveal()))
		if e == nil {
			p := core.Principal{OwnerID: s.Session.AccountID, SubjectID: s.Session.AccountID, Kind: core.ActorOwner, SessionID: s.Session.ID}
			if e = m.ValidatePrincipal(ctx, p); e != nil {
				return e
			}
			if subtle.ConstantTimeCompare([]byte(s.CSRFDigest), []byte(digest(token.Reveal()))) != 1 {
				return core.NewError(core.Forbidden, "CSRF check failed")
			}
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		var c StoredChallenge
		if e = tx.Get(ctx, "auth.challenges", digest(a.Session.Reveal()), &c); e != nil {
			return core.NewError(core.Forbidden, "CSRF check failed")
		}
		if c.Purpose != "csrf" || !m.options.Now().Before(c.ExpiresAt) || subtle.ConstantTimeCompare(c.EncryptedPayload, []byte(digest(token.Reveal()))) != 1 {
			return core.NewError(core.Forbidden, "CSRF check failed")
		}
		return nil
	})
}

var _ Service = (*Manager)(nil)
var _ Bootstrap = (*Manager)(nil)
var _ BrowserGuard = (*Manager)(nil)

// The persisted shape matches audit.Record but avoids coupling authentication
// to the audit reader. Security events commit with the protected mutation.
func (m *Manager) audit(ctx context.Context, owner, subject core.ID, action, outcome string, target core.Object) error {
	id := newID()
	record := struct {
		ID, OwnerID, SubjectID core.ID
		ActorKind              core.ActorKind
		Action                 string
		Target                 core.Object
		Outcome                string
		At                     time.Time
	}{id, owner, subject, core.ActorOwner, action, target, outcome, m.options.Now()}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.audit", persistence.Key(owner, id), record)
	})
}

func (m *Manager) consumeTOTP(ctx context.Context, owner core.ID, secret, code string) error {
	if len(code) != 6 {
		return denied()
	}
	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		at := m.options.Now().Add(offset)
		if subtle.ConstantTimeCompare([]byte(totp(secret, at)), []byte(code)) != 1 {
			continue
		}
		key := string(owner) + "/" + digest(secret+":"+at.Truncate(30*time.Second).UTC().Format(time.RFC3339))
		return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var used bool
			err := tx.Get(ctx, "auth.totp_used", key, &used)
			if err == nil {
				return denied()
			}
			if !persistence.Missing(err) {
				return err
			}
			return tx.Put(ctx, "auth.totp_used", key, true)
		})
	}
	return denied()
}

func validBrowserOrigin(u *url.URL, local bool, lan ...bool) bool {
	if len(lan) > 0 && lan[0] {
		ip, err := netip.ParseAddr(u.Hostname())
		return err == nil && ip.Zone() == "" && (ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback()) && u.Scheme == "http"
	}
	if !local {
		return u.Scheme == "https"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && ip.IsLoopback() && ip.Zone() == ""
}
