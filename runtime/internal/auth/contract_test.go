package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Bind this factory to real auth and persistence implementations. Each fixture
// starts with an empty account store and a controlled clock. Code generates a
// TOTP from the enrollment URI; it must not bypass Complete/ConfirmMFA.
type contractFixture struct {
	Service       Service
	Bootstrap     Bootstrap
	Repository    Repository
	Browser       BrowserGuard
	Now           time.Time
	Advance       func(time.Duration)
	Code          func(MFAEnrollment) core.Secret
	TrustedOrigin string
	PreLoginCSRF  Authentication
	PreLoginToken core.Secret
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "auth", newContractFixture)
}
func initialized(t *testing.T, f contractFixture) (core.Principal, SessionIssued) {
	t.Helper()
	ctx := testkit.Context(t)
	_, err := f.Bootstrap.Initialize(ctx, InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	r, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil || r.Challenge != nil {
		t.Fatal("non-MFA login must issue exactly one session")
	}
	if r.Session.Token.Empty() || r.Session.CSRF.Empty() || r.Session.Token.Reveal() == r.Session.CSRF.Reveal() {
		t.Fatal("session and distinct CSRF tokens required")
	}
	p, err := f.Service.Authenticate(ctx, Authentication{Session: r.Session.Token})
	testkit.RequireNoError(t, err)
	if p.Kind != core.ActorOwner || p.SessionID != r.Session.Session.ID {
		t.Fatal("wrong authenticated identity")
	}
	return p, *r.Session
}
func verify(t *testing.T, f contractFixture, p core.Principal, purpose string) Verification {
	t.Helper()
	v, err := f.Service.Reauthenticate(testkit.Context(t), p, ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: purpose})
	testkit.RequireNoError(t, err)
	if v.Token.Empty() {
		t.Fatal("empty verification")
	}
	return v
}
func TestTDDBootstrapSingleUseAndRecovery(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	ready, err := f.Bootstrap.Initialized(ctx)
	testkit.RequireNoError(t, err)
	if ready {
		t.Fatal("empty store initialized")
	}
	p, s := initialized(t, f)
	ready, err = f.Bootstrap.Initialized(ctx)
	testkit.RequireNoError(t, err)
	if !ready {
		t.Fatal("account not initialized")
	}
	_, err = f.Bootstrap.Initialize(ctx, InitializeRequest{Username: "attacker", Password: core.NewSecret("other-password")})
	if err == nil {
		t.Fatal("second bootstrap replaced owner")
	}
	token, err := f.Service.CreateToken(ctx, p, CreateTokenRequest{Name: "before recovery", Scopes: []string{"read"}, ExpiresAt: f.Now.Add(time.Hour), Verification: verify(t, f, p, "token.create")})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Bootstrap.Recover(ctx, RecoveryRequest{Password: core.NewSecret("recovered-password-123!"), ResetMFA: true}))
	for _, a := range []Authentication{{Session: s.Token}, {Bearer: token.Value}} {
		_, err = f.Service.Authenticate(ctx, a)
		testkit.RequireCode(t, err, core.Unauthenticated)
	}
	old, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	if err == nil || old.Session != nil {
		t.Fatal("recovery retained old password")
	}
	r, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("recovered-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil {
		t.Fatal("recovered owner cannot log in")
	}
}
func TestTDDAuthenticationSessionLifecycle(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, s := initialized(t, f)
	page, err := f.Service.Sessions(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].ID != s.Session.ID {
		t.Fatal("session listing disagrees with login")
	}
	testkit.RequireNoError(t, f.Service.RevokeSession(ctx, p, s.Session.ID))
	_, err = f.Service.Authenticate(ctx, Authentication{Session: s.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
	r, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil {
		t.Fatal("session missing")
	}
	p, err = f.Service.Authenticate(ctx, Authentication{Session: r.Session.Token})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.Logout(ctx, p))
	_, err = f.Service.Authenticate(ctx, Authentication{Session: r.Session.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
func TestTDDAuthenticationExpiryAndInvalidCredentials(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	_, s := initialized(t, f)
	for _, a := range []Authentication{{}, {Session: s.Token, Bearer: core.NewSecret("other")}, {Session: core.NewSecret("forged")}} {
		_, err := f.Service.Authenticate(ctx, a)
		testkit.RequireCode(t, err, core.Unauthenticated)
	}
	_, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("wrong")})
	testkit.RequireCode(t, err, core.Unauthenticated)
	f.Advance(s.Session.ExpiresAt.Sub(f.Now) + time.Second)
	_, err = f.Service.Authenticate(ctx, Authentication{Session: s.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
func TestTDDVerificationBindingSingleUseAndPasswordChange(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, _ := initialized(t, f)
	v := verify(t, f, p, "credential.put")
	if err := f.Service.CheckVerification(ctx, p, v, "token.create"); err == nil {
		t.Fatal("verification usable for another purpose")
	}
	other := p
	other.SessionID = "other-session"
	if err := f.Service.CheckVerification(ctx, other, v, "credential.put"); err == nil {
		t.Fatal("verification usable by another session")
	}
	testkit.RequireNoError(t, f.Service.CheckVerification(ctx, p, v, "credential.put"))
	if err := f.Service.CheckVerification(ctx, p, v, "credential.put"); err == nil {
		t.Fatal("verification replay accepted")
	}
	testkit.RequireNoError(t, f.Service.ChangePassword(ctx, p, ChangePasswordRequest{Verification: verify(t, f, p, "password.change"), NewPassword: core.NewSecret("new-strong-password-123!")}))
	_, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireCode(t, err, core.Unauthenticated)
	r, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("new-strong-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil {
		t.Fatal("new password unusable")
	}
}
func TestTDDMFALifecycleAndChallengeReplay(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, _ := initialized(t, f)
	status, err := f.Service.SecurityStatus(ctx, p)
	testkit.RequireNoError(t, err)
	if status.MFAEnabled {
		t.Fatal("MFA enabled before enrollment")
	}
	enrollment, err := f.Service.BeginMFAEnrollment(ctx, p, verify(t, f, p, "mfa.enroll"))
	testkit.RequireNoError(t, err)
	codes, err := f.Service.ConfirmMFAEnrollment(ctx, p, ConfirmMFARequest{EnrollmentID: enrollment.ID, Code: f.Code(enrollment)})
	testkit.RequireNoError(t, err)
	if len(codes.Codes) == 0 {
		t.Fatal("no recovery codes")
	}
	status, err = f.Service.SecurityStatus(ctx, p)
	testkit.RequireNoError(t, err)
	if !status.MFAEnabled {
		t.Fatal("MFA status did not reflect enrollment")
	}
	r, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Challenge == nil || r.Session != nil {
		t.Fatal("MFA bypassed during login")
	}
	_, err = f.Service.Authenticate(ctx, Authentication{Session: r.Challenge.ID})
	testkit.RequireCode(t, err, core.Unauthenticated)
	response := MFAChallengeResponse{Challenge: r.Challenge.ID, Code: codes.Codes[0]}
	s, err := f.Service.CompleteMFA(ctx, response)
	testkit.RequireNoError(t, err)
	if s.Token.Empty() {
		t.Fatal("MFA completion omitted session")
	}
	_, err = f.Service.CompleteMFA(ctx, response)
	if err == nil {
		t.Fatal("MFA challenge reused")
	}
	p, err = f.Service.Authenticate(ctx, Authentication{Session: s.Token})
	testkit.RequireNoError(t, err)
	r, err = f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Challenge == nil {
		t.Fatal("challenge missing")
	}
	_, err = f.Service.CompleteMFA(ctx, MFAChallengeResponse{Challenge: r.Challenge.ID, Code: codes.Codes[0]})
	if err == nil {
		t.Fatal("recovery code reused on another challenge")
	}
	f.Advance(time.Minute)
	v, err := f.Service.Reauthenticate(ctx, p, ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), MFACode: f.Code(enrollment), Purpose: "mfa.disable"})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.DisableMFA(ctx, p, v))
	status, err = f.Service.SecurityStatus(ctx, p)
	testkit.RequireNoError(t, err)
	if status.MFAEnabled {
		t.Fatal("MFA status did not reflect disable")
	}
	r, err = f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil || r.Challenge != nil {
		t.Fatal("disabled MFA still requires challenge")
	}
}
func TestSecurityStatusRequiresLiveOwnerSession(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, _ := initialized(t, f)
	_, err := f.Service.SecurityStatus(ctx, core.Principal{})
	if err == nil {
		t.Fatal("anonymous security status allowed")
	}
	withoutSession := p
	withoutSession.SessionID = ""
	_, err = f.Service.SecurityStatus(ctx, withoutSession)
	if err == nil {
		t.Fatal("security status requires browser session")
	}
	testkit.RequireNoError(t, f.Service.Logout(ctx, p))
	_, err = f.Service.SecurityStatus(ctx, p)
	if err == nil {
		t.Fatal("revoked session can read security status")
	}
}
func TestTDDTokenLifecycleAndManagementAuthorization(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, _ := initialized(t, f)
	r, err := f.Service.CreateToken(ctx, p, CreateTokenRequest{Name: "reader", Scopes: []string{"read"}, ExpiresAt: f.Now.Add(time.Hour), Verification: verify(t, f, p, "token.create")})
	testkit.RequireNoError(t, err)
	if r.Value.Empty() {
		t.Fatal("no token secret")
	}
	tp, err := f.Service.Authenticate(ctx, Authentication{Bearer: r.Value})
	testkit.RequireNoError(t, err)
	if tp.TokenID != r.Token.ID || tp.SessionID != "" {
		t.Fatal("bearer identity mismatch")
	}
	list, err := f.Service.Tokens(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(list.Items) != 1 || list.Items[0].ID != r.Token.ID {
		t.Fatal("token metadata not listed")
	}
	testkit.RequireNoError(t, f.Service.RevokeToken(ctx, p, r.Token.ID))
	_, err = f.Service.Authenticate(ctx, Authentication{Bearer: r.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	assistant := core.Principal{OwnerID: p.OwnerID, Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "execution"}
	if _, err = f.Service.Tokens(ctx, assistant, core.PageRequest{}); err == nil {
		t.Fatal("assistant listed owner tokens")
	}
	if _, err = f.Service.Sessions(ctx, assistant, core.PageRequest{}); err == nil {
		t.Fatal("assistant listed sessions")
	}
	if _, err = f.Service.Reauthenticate(ctx, assistant, ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: "token.create"}); err == nil {
		t.Fatal("assistant reauthenticated as owner")
	}
}
func TestTDDBrowserGuardSessionAndOriginBinding(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	_, s := initialized(t, f)
	a := Authentication{Session: s.Token}
	testkit.RequireNoError(t, f.Browser.CheckCSRF(ctx, a, s.CSRF, f.TrustedOrigin))
	for _, tc := range []struct {
		a      Authentication
		token  core.Secret
		origin string
	}{{a, core.Secret{}, f.TrustedOrigin}, {a, s.CSRF, "https://attacker.example"}, {Authentication{Session: core.NewSecret("other-session")}, s.CSRF, f.TrustedOrigin}, {a, s.Token, f.TrustedOrigin}} {
		if err := f.Browser.CheckCSRF(ctx, tc.a, tc.token, tc.origin); err == nil {
			t.Fatal("invalid CSRF/origin accepted")
		}
	}
	testkit.RequireNoError(t, f.Browser.CheckCSRF(ctx, f.PreLoginCSRF, f.PreLoginToken, f.TrustedOrigin))
	if err := f.Browser.CheckCSRF(ctx, f.PreLoginCSRF, f.PreLoginToken, "https://attacker.example"); err == nil {
		t.Fatal("login CSRF accepted")
	}
}
func TestTDDRepositoryAccountAndCredentials(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := f.Repository
	_, err := r.Account(ctx)
	testkit.RequireCode(t, err, core.NotFound)
	account := StoredAccount{Account: Account{ID: "owner", Username: "owner", CreatedAt: f.Now}, PasswordHash: core.NewSecret("encoded-hash"), RecoveryDigests: []string{"recovery-digest"}, Version: 1}
	testkit.RequireNoError(t, r.InsertAccount(ctx, account))
	if err := r.InsertAccount(ctx, account); err == nil {
		t.Fatal("second account insert accepted")
	}
	got, err := r.Account(ctx)
	testkit.RequireNoError(t, err)
	if got.Account.ID != account.Account.ID || got.Version != 1 {
		t.Fatal("account mismatch")
	}
	got.Account.Username = "renamed"
	testkit.RequireNoError(t, r.SaveAccount(ctx, got, 1))
	if err := r.SaveAccount(ctx, got, 1); err == nil {
		t.Fatal("stale account version accepted")
	}
	s := StoredSession{Session: Session{ID: "session", AccountID: "owner", CreatedAt: f.Now, ExpiresAt: f.Now.Add(time.Hour)}, TokenDigest: "session-digest", CSRFDigest: "csrf-digest"}
	testkit.RequireNoError(t, r.SaveSession(ctx, s))
	stored, err := r.FindSession(ctx, s.TokenDigest)
	testkit.RequireNoError(t, err)
	if stored.Session.ID != s.Session.ID {
		t.Fatal("session lookup mismatch")
	}
	sessions, err := r.Sessions(ctx, "owner", core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(sessions.Items) != 1 {
		t.Fatal("session omitted")
	}
	token := StoredToken{Token: Token{ID: "token", Name: "test", CreatedAt: f.Now, ExpiresAt: f.Now.Add(time.Hour)}, AccountID: "owner", Digest: "token-digest"}
	testkit.RequireNoError(t, r.SaveToken(ctx, token))
	tok, err := r.FindToken(ctx, token.Digest)
	testkit.RequireNoError(t, err)
	if tok.Token.ID != token.Token.ID {
		t.Fatal("token lookup mismatch")
	}
	tokens, err := r.Tokens(ctx, "owner", core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(tokens.Items) != 1 {
		t.Fatal("token omitted")
	}
	testkit.RequireNoError(t, r.RevokeSession(ctx, "owner", s.Session.ID, f.Now))
	stored, err = r.FindSession(ctx, s.TokenDigest)
	testkit.RequireNoError(t, err)
	if stored.RevokedAt == nil {
		t.Fatal("session revocation not persisted")
	}
	testkit.RequireNoError(t, r.RevokeToken(ctx, "owner", token.Token.ID, f.Now))
	tok, err = r.FindToken(ctx, token.Digest)
	testkit.RequireNoError(t, err)
	if tok.RevokedAt == nil {
		t.Fatal("token revocation not persisted")
	}
	s.Session.ID = "second-session"
	s.TokenDigest = "second-session-digest"
	token.Token.ID = "second-token"
	token.Digest = "second-token-digest"
	testkit.RequireNoError(t, r.SaveSession(ctx, s))
	testkit.RequireNoError(t, r.SaveToken(ctx, token))
	testkit.RequireNoError(t, r.RevokeAll(ctx, "owner", f.Now))
	stored, err = r.FindSession(ctx, s.TokenDigest)
	testkit.RequireNoError(t, err)
	tok, err = r.FindToken(ctx, token.Digest)
	testkit.RequireNoError(t, err)
	if stored.RevokedAt == nil || tok.RevokedAt == nil {
		t.Fatal("recovery left active credentials")
	}
}
func TestTDDRepositorySingleUseChallengeAndRecovery(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := f.Repository
	testkit.RequireNoError(t, r.InsertAccount(ctx, StoredAccount{Account: Account{ID: "owner", Username: "owner"}, Version: 1, RecoveryDigests: []string{"digest"}}))
	testkit.RequireNoError(t, r.PutChallenge(ctx, StoredChallenge{Digest: "challenge", AccountID: "owner", ExpiresAt: f.Now.Add(time.Minute)}))
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := r.ConsumeChallenge(ctx, "challenge", f.Now)
			results <- err == nil && c.AccountID == "owner"
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for ok := range results {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("challenge successful consumptions=%d", wins)
	}
	testkit.RequireNoError(t, r.PutChallenge(ctx, StoredChallenge{Digest: "expired", AccountID: "owner", ExpiresAt: f.Now.Add(-time.Second)}))
	if _, err := r.ConsumeChallenge(ctx, "expired", f.Now); err == nil {
		t.Fatal("expired challenge consumed")
	}
	ok, err := r.ConsumeRecoveryCode(ctx, "other-owner", "digest")
	testkit.RequireNoError(t, err)
	if ok {
		t.Fatal("cross-owner recovery accepted")
	}
	ok, err = r.ConsumeRecoveryCode(ctx, "owner", "digest")
	testkit.RequireNoError(t, err)
	if !ok {
		t.Fatal("valid recovery rejected")
	}
	ok, err = r.ConsumeRecoveryCode(ctx, "owner", "digest")
	testkit.RequireNoError(t, err)
	if ok {
		t.Fatal("recovery replay accepted")
	}
}

// All service calls above receive bounded contexts; cancellation must also be
// honored before credential verification or persistence work begins.
func TestTDDAuthenticationCanceled(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Service.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("secret")})
	if err == nil {
		t.Fatal("canceled login succeeded")
	}
}

func TestTDDTokenExpiryAndVerificationExpiry(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	p, _ := initialized(t, f)
	issued, err := f.Service.CreateToken(ctx, p, CreateTokenRequest{Name: "short-lived", Scopes: []string{"read"}, ExpiresAt: f.Now.Add(time.Minute), Verification: verify(t, f, p, "token.create")})
	testkit.RequireNoError(t, err)
	v := verify(t, f, p, "credential.put")
	f.Advance(24 * time.Hour)
	if err := f.Service.CheckVerification(ctx, p, v, "credential.put"); err == nil {
		t.Fatal("expired verification accepted")
	}
	_, err = f.Service.Authenticate(ctx, Authentication{Bearer: issued.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestTDDBootstrapConcurrentInitialization(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"owner-a", "owner-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Bootstrap.Initialize(ctx, InitializeRequest{Username: name, Password: core.NewSecret("strong-initial-password-123!")})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("atomic initialization winners=%d", wins)
	}
}

func TestTDDServiceManagementRejectsAssistant(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	owner, s := initialized(t, f)
	p := core.Principal{OwnerID: owner.OwnerID, Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "execution"}
	check := func(err error) {
		t.Helper()
		if err == nil {
			t.Fatal("assistant performed owner-only management")
		}
	}
	check(f.Service.RevokeSession(ctx, p, s.Session.ID))
	check(f.Service.Logout(ctx, p))
	check(f.Service.ChangePassword(ctx, p, ChangePasswordRequest{NewPassword: core.NewSecret("other-password")}))
	_, err := f.Service.BeginMFAEnrollment(ctx, p, Verification{})
	check(err)
	_, err = f.Service.ConfirmMFAEnrollment(ctx, p, ConfirmMFARequest{EnrollmentID: "enrollment", Code: core.NewSecret("123456")})
	check(err)
	check(f.Service.DisableMFA(ctx, p, Verification{}))
	_, err = f.Service.CreateToken(ctx, p, CreateTokenRequest{Name: "escalate", Scopes: []string{"manage"}, ExpiresAt: f.Now.Add(time.Hour)})
	check(err)
	check(f.Service.RevokeToken(ctx, p, "owner-token"))
	check(f.Service.CheckVerification(ctx, p, Verification{}, "token.create"))
	// Rejected session revocation must not revoke the real owner's session.
	_, err = f.Service.Authenticate(ctx, Authentication{Session: s.Token})
	testkit.RequireNoError(t, err)
}
