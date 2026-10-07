// Package auth defines owner authentication, revocable sessions and local bootstrap contracts.
package auth

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

// Service authenticates credentials, not resource access. All management
// methods require a current owner session; assistant principals are forbidden.
type Service interface {
	CreateLoginLink(context.Context, core.Principal, CreateLoginLinkRequest) (LoginLinkIssued, error)
	LoginLinks(context.Context, core.Principal, core.PageRequest) (core.Page[LoginLink], error)
	RevokeLoginLink(context.Context, core.Principal, core.ID) error
	PreviewLoginLink(context.Context, PreviewLoginLinkRequest) (LoginLinkPreview, error)
	RedeemLoginLink(context.Context, RedeemLoginLinkRequest) (LoginLinkResult, error)
	Login(context.Context, LoginRequest) (LoginResult, error)
	CompleteMFA(context.Context, MFAChallengeResponse) (SessionIssued, error)
	Authenticate(context.Context, Authentication) (core.Principal, error)
	SecurityStatus(context.Context, core.Principal) (SecurityStatus, error)
	SetReauthentication(context.Context, core.Principal, ReauthenticationSettings) (SecurityStatus, error)
	Reauthenticate(context.Context, core.Principal, ReauthenticateRequest) (Verification, error)
	CheckVerification(context.Context, core.Principal, Verification, string) error
	Sessions(context.Context, core.Principal, core.PageRequest) (core.Page[Session], error)
	RevokeSession(context.Context, core.Principal, core.ID) error
	Logout(context.Context, core.Principal) error
	ChangePassword(context.Context, core.Principal, ChangePasswordRequest) error
	BeginMFAEnrollment(context.Context, core.Principal, Verification) (MFAEnrollment, error)
	ConfirmMFAEnrollment(context.Context, core.Principal, ConfirmMFARequest) (RecoveryCodes, error)
	DisableMFA(context.Context, core.Principal, Verification) error
	CreateToken(context.Context, core.Principal, CreateTokenRequest) (TokenIssued, error)
	Tokens(context.Context, core.Principal, core.PageRequest) (core.Page[Token], error)
	RevokeToken(context.Context, core.Principal, core.ID) error
}

// Bootstrap is wired to the local CLI only, never public HTTP or Agent tools.
// Initialize is single-use and atomic. Recover revokes ALL sessions and tokens.
type Bootstrap interface {
	Initialized(context.Context) (bool, error)
	Initialize(context.Context, InitializeRequest) (Account, error)
	Recover(context.Context, RecoveryRequest) error
}
type Account struct {
	Avatar                  string // Empty, preset:<id>, or a bounded PNG/JPEG thumbnail data URL.
	DisplayName             string
	GroupID                 core.ID
	Disabled                bool
	Version                 core.Version
	ID                      core.ID
	Username                string
	MFAEnabled              bool
	ReauthenticationEnabled bool
	CreatedAt               time.Time
}
type SecurityStatus struct {
	MFAEnabled              bool
	ReauthenticationEnabled bool
	UnprotectedPurposes     []string
	Version                 core.Version
}
type ReauthenticationSettings struct {
	Enabled         bool
	ExpectedVersion core.Version
	Verification    Verification
}
type Client struct {
	Address    string
	UserAgent  string
	DeviceName string
}
type InitializeRequest struct {
	Username string
	Password core.Secret
}
type RecoveryRequest struct {
	Password core.Secret
	ResetMFA bool
}
type LoginRequest struct {
	Username string
	Password core.Secret
	Client   Client
}

// Exactly one of Session and Challenge is populated. MFA challenges expire,
// are single-use and never authorize API access.
type LoginResult struct {
	Session   *SessionIssued
	Challenge *Challenge
}
type Challenge struct {
	ID        core.Secret
	ExpiresAt time.Time
}
type MFAChallengeResponse struct {
	Challenge core.Secret
	Code      core.Secret
	Client    Client
}
type Authentication struct {
	Session core.Secret
	Bearer  core.Secret
}

func (a Authentication) Validate() error {
	if a.Session.Empty() == a.Bearer.Empty() {
		return core.NewError(core.Unauthenticated, "exactly one credential required")
	}
	return nil
}

type Session struct {
	ID         core.ID
	AccountID  core.ID
	DeviceName string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// Raw session tokens go only to Secure HttpOnly cookies; CSRF is a distinct token.
type SessionIssued struct {
	Session Session
	Token   core.Secret
	CSRF    core.Secret
}
type ReauthenticateRequest struct {
	Password core.Secret
	MFACode  core.Secret
	Purpose  string
}

// Verification is short-lived, single-use and bound to session and purpose.
type Verification struct{ Token core.Secret }
type ChangePasswordRequest struct {
	Verification Verification
	NewPassword  core.Secret
}
type MFAEnrollment struct {
	ID              core.ID
	ProvisioningURI core.Secret
	ExpiresAt       time.Time
}
type ConfirmMFARequest struct {
	EnrollmentID core.ID
	Code         core.Secret
}
type RecoveryCodes struct{ Codes []core.Secret }
type CreateTokenRequest struct {
	Name         string
	Scopes       []string
	ExpiresAt    time.Time
	Verification Verification
}
type Token struct {
	ID         core.ID
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
}
type TokenIssued struct {
	Token Token
	Value core.Secret
}
