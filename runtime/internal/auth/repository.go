package auth

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

// StoredAccount is private persistence state; password hashes are never returned
// in Account. TOTP material is encrypted; recovery values are one-way digests.
type StoredAccount struct {
	Account         Account
	PasswordHash    core.Secret
	EncryptedTOTP   []byte
	RecoveryDigests []string
	Version         core.Version
}
type StoredSession struct {
	DeviceDigest      string
	Session           Session
	TokenDigest       string
	CSRFDigest        string
	ReauthenticatedAt time.Time
	RevokedAt         *time.Time
}
type StoredToken struct {
	Token     Token
	AccountID core.ID
	Digest    string
	RevokedAt *time.Time
}
type StoredChallenge struct {
	RememberMe       bool
	DeviceDigest     string
	ClientType       string
	DeviceName       string
	ExpectedSession  core.ID
	LoginLinkDigest  string
	Digest           string
	AccountID        core.ID
	Purpose          string
	SessionID        core.ID
	ExpiresAt        time.Time
	Attempts         int
	EncryptedPayload []byte
}

// Repository methods participate in a storage transaction. Challenge and recovery
// code consumption must be atomic; failed validation must not authenticate.
type Repository interface {
	Account(context.Context) (StoredAccount, error)
	AccountByID(context.Context, core.ID) (StoredAccount, error)
	AccountByUsername(context.Context, string) (StoredAccount, error)
	InsertAccount(context.Context, StoredAccount) error
	SaveAccount(context.Context, StoredAccount, core.Version) error
	FindSession(context.Context, string) (StoredSession, error)
	SaveSession(context.Context, StoredSession) error
	Sessions(context.Context, core.ID, core.PageRequest) (core.Page[Session], error)
	RevokeSession(context.Context, core.ID, core.ID, time.Time) error
	FindToken(context.Context, string) (StoredToken, error)
	SaveToken(context.Context, StoredToken) error
	Tokens(context.Context, core.ID, core.PageRequest) (core.Page[Token], error)
	RevokeToken(context.Context, core.ID, core.ID, time.Time) error
	RevokeAll(context.Context, core.ID, time.Time) error
	PutChallenge(context.Context, StoredChallenge) error
	ConsumeChallenge(context.Context, string, time.Time) (StoredChallenge, error)
	ConsumeRecoveryCode(context.Context, core.ID, string) (bool, error)
}

// BrowserGuard validates CSRF and origin in the HTTP adapter, including login.
// The opaque token is bound to the current session or pre-login challenge.
type BrowserGuard interface {
	CheckCSRF(context.Context, Authentication, core.Secret, string) error
}
