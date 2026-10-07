package auth

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
	"strings"
	"sync"
	"testing"
	"time"
)

func newLink(t *testing.T, f contractFixture, p core.Principal) LoginLinkIssued {
	t.Helper()
	link, err := f.Service.CreateLoginLink(testkit.Context(t), p, CreateLoginLinkRequest{Name: "Phone", TargetPath: "/settings/theme", ExpiresIn: time.Hour, Verification: verify(t, f, p, "login_link.create")})
	testkit.RequireNoError(t, err)
	return link
}
func TestLoginLinkLifecycleAndAtomicRedemption(t *testing.T) {
	f := fixture(t)
	p, original := initialized(t, f)
	ctx := testkit.Context(t)
	link := newLink(t, f, p)
	_, err := f.Service.Authenticate(ctx, Authentication{Bearer: link.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = f.Service.Authenticate(ctx, Authentication{Session: link.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	list, err := f.Service.LoginLinks(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(list.Items) != 1 || list.Items[0].ID != link.Link.ID {
		t.Fatal("missing link metadata")
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), link.Value.Reveal()) {
		t.Fatal("list leaked token")
	}
	manager := f.Service.(*Manager)
	testkit.RequireNoError(t, manager.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, loginLinksBucket)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(rows)
		if strings.Contains(string(raw), link.Value.Reveal()) {
			t.Fatal("plaintext token persisted")
		}
		return nil
	}))
	var wg sync.WaitGroup
	results := make(chan LoginLinkResult, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: link.Value})
			if err == nil {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	if len(results) != 1 {
		t.Fatal("link did not authorize exactly one session")
	}
	result := <-results
	if result.Session == nil || result.Challenge != nil || result.TargetPath != "/settings/theme" || result.Session.Token.Reveal() == original.Token.Reveal() {
		t.Fatal("invalid exchanged session")
	}
	_, err = f.Service.Authenticate(ctx, Authentication{Session: result.Session.Token})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.RevokeLoginLink(ctx, p, link.Link.ID))
	_, err = f.Service.Authenticate(ctx, Authentication{Session: result.Session.Token})
	testkit.RequireNoError(t, err)
	list, err = f.Service.LoginLinks(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if list.Items[0].UsedAt == nil || list.Items[0].RevokedAt == nil {
		t.Fatal("usage/revocation not recorded")
	}
}
func TestLoginLinkValidationExpiryAndManagement(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	ctx := testkit.Context(t)
	req := CreateLoginLinkRequest{Name: "test", TargetPath: "/assistants", ExpiresIn: time.Hour}
	_, err := f.Service.CreateLoginLink(ctx, p, req)
	if err == nil {
		t.Fatal("verification bypass")
	}
	req.Verification = verify(t, f, p, "token.create")
	_, err = f.Service.CreateLoginLink(ctx, p, req)
	if err == nil {
		t.Fatal("wrong-purpose verification accepted")
	}
	for _, target := range []string{"/settings/appearance", "/ui", "//evil.test", "/settings/../login", "https://evil.test", "/settings?token=x", "/\\evil"} {
		req.TargetPath = target
		_, err = f.Service.CreateLoginLink(ctx, p, req)
		testkit.RequireCode(t, err, core.InvalidArgument)
	}
	req.TargetPath = "/assistants"
	req.ExpiresIn = 25 * time.Hour
	_, err = f.Service.CreateLoginLink(ctx, p, req)
	testkit.RequireCode(t, err, core.InvalidArgument)
	link := newLink(t, f, p)
	noSession := p
	noSession.SessionID = ""
	_, err = f.Service.LoginLinks(ctx, noSession, core.PageRequest{})
	if err == nil {
		t.Fatal("management without browser session")
	}
	if err = f.Service.RevokeLoginLink(ctx, noSession, link.Link.ID); err == nil {
		t.Fatal("unauthorized revocation")
	}
	testkit.RequireNoError(t, f.Service.RevokeLoginLink(ctx, p, link.Link.ID))
	_, err = f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: link.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	expired := newLink(t, f, p)
	f.Advance(time.Hour)
	_, err = f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: expired.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
func TestLoginLinkPasswordChangeAndRecoveryRevokeOutstandingLinks(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "password", true: "recovery"}[recovery], func(t *testing.T) {
			f := fixture(t)
			p, _ := initialized(t, f)
			ctx := testkit.Context(t)
			link := newLink(t, f, p)
			if recovery {
				testkit.RequireNoError(t, f.Bootstrap.Recover(ctx, RecoveryRequest{Password: core.NewSecret("replacement-password-2026!")}))
			} else {
				testkit.RequireNoError(t, f.Service.ChangePassword(ctx, p, ChangePasswordRequest{Verification: verify(t, f, p, "password.change"), NewPassword: core.NewSecret("replacement-password-2026!")}))
			}
			_, err := f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: link.Value})
			testkit.RequireCode(t, err, core.Unauthenticated)
		})
	}
}
func TestLoginLinkMFAAndPendingChallengeRevocation(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	ctx := testkit.Context(t)
	first := newLink(t, f, p)
	second := newLink(t, f, p)
	third := newLink(t, f, p)
	enrollment, err := f.Service.BeginMFAEnrollment(ctx, p, verify(t, f, p, "mfa.enroll"))
	testkit.RequireNoError(t, err)
	codes, err := f.Service.ConfirmMFAEnrollment(ctx, p, ConfirmMFARequest{EnrollmentID: enrollment.ID, Code: f.Code(enrollment)})
	testkit.RequireNoError(t, err)
	result, err := f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: first.Value})
	testkit.RequireNoError(t, err)
	if result.Session != nil || result.Challenge == nil {
		t.Fatal("MFA bypassed")
	}
	_, err = f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: first.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = f.Service.CompleteMFA(ctx, MFAChallengeResponse{Challenge: result.Challenge.ID, Code: core.NewSecret("bad-code")})
	if err == nil {
		t.Fatal("wrong MFA accepted")
	}
	issued, err := f.Service.CompleteMFA(ctx, MFAChallengeResponse{Challenge: result.Challenge.ID, Code: codes.Codes[0]})
	testkit.RequireNoError(t, err)
	_, err = f.Service.Authenticate(ctx, Authentication{Session: issued.Token})
	testkit.RequireNoError(t, err)
	pending, err := f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: second.Value})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.RevokeLoginLink(ctx, p, second.Link.ID))
	_, err = f.Service.CompleteMFA(ctx, MFAChallengeResponse{Challenge: pending.Challenge.ID, Code: codes.Codes[1]})
	testkit.RequireCode(t, err, core.Unauthenticated)
	pending, err = f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: third.Value})
	testkit.RequireNoError(t, err)
	f.Advance(time.Minute)
	proof, err := f.Service.Reauthenticate(ctx, p, ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), MFACode: f.Code(enrollment), Purpose: "password.change"})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.ChangePassword(ctx, p, ChangePasswordRequest{Verification: proof, NewPassword: core.NewSecret("replacement-password-2026!")}))
	_, err = f.Service.CompleteMFA(ctx, MFAChallengeResponse{Challenge: pending.Challenge.ID, Code: codes.Codes[1]})
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestLoginLinkPreviewDoesNotConsumeAndRejectsInvalidLinks(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	ctx := testkit.Context(t)
	link := newLink(t, f, p)
	sessions, err := f.Service.Sessions(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	for range 2 {
		preview, err := f.Service.PreviewLoginLink(ctx, PreviewLoginLinkRequest{Token: link.Value})
		testkit.RequireNoError(t, err)
		if !preview.ExpiresAt.Equal(link.Link.ExpiresAt) {
			t.Fatal("incorrect preview expiry")
		}
	}
	links, err := f.Service.LoginLinks(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if links.Items[0].UsedAt != nil {
		t.Fatal("preview consumed link")
	}
	after, err := f.Service.Sessions(ctx, p, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(after.Items) != len(sessions.Items) {
		t.Fatal("preview issued session")
	}
	_, err = f.Service.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: link.Value})
	testkit.RequireNoError(t, err)
	_, err = f.Service.PreviewLoginLink(ctx, PreviewLoginLinkRequest{Token: link.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	revoked := newLink(t, f, p)
	testkit.RequireNoError(t, f.Service.RevokeLoginLink(ctx, p, revoked.Link.ID))
	for _, token := range []core.Secret{revoked.Value, core.NewSecret("unknown-token"), core.NewSecret("")} {
		f.Advance(time.Minute)
		_, err = f.Service.PreviewLoginLink(ctx, PreviewLoginLinkRequest{Token: token})
		testkit.RequireCode(t, err, core.Unauthenticated)
	}
	expired := newLink(t, f, p)
	f.Advance(time.Hour)
	_, err = f.Service.PreviewLoginLink(ctx, PreviewLoginLinkRequest{Token: expired.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
