package auth

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func deviceLogin(t *testing.T, f contractFixture, c Client) SessionIssued {
	t.Helper()
	r, err := f.Service.Login(t.Context(), LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), Client: c})
	testkit.RequireNoError(t, err)
	if r.Session == nil {
		t.Fatal("missing session")
	}
	return *r.Session
}

func TestDeviceLoginReplacesCredentialsWithoutGrowingHistory(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	m.options.LoginAttemptsPerMinute = 100
	client := Client{DeviceID: strings.Repeat("a", 48), ClientType: "web", DeviceName: "Chrome / Windows"}
	first := deviceLogin(t, f, client)
	p, err := m.Authenticate(t.Context(), Authentication{Session: first.Token})
	testkit.RequireNoError(t, err)
	proof := verify(t, f, p, "login_link.create")
	watch, err := m.WatchPrincipal(p)
	testkit.RequireNoError(t, err)
	defer watch.Close()
	f.Advance(time.Hour)
	second := deviceLogin(t, f, client)
	if first.Session.ID == second.Session.ID || first.Token == second.Token || first.CSRF == second.CSRF {
		t.Fatal("credentials were reused")
	}
	if !second.Session.CreatedAt.Equal(first.Session.CreatedAt) || !second.Session.LastLoginAt.Equal(f.Now.Add(time.Hour)) {
		t.Fatal("login times were not refreshed")
	}
	select {
	case <-watch.Changes():
	default:
		t.Fatal("old streams were not notified")
	}
	_, err = watch.Check(t.Context())
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = m.Authenticate(t.Context(), Authentication{Session: first.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
	next, err := m.Authenticate(t.Context(), Authentication{Session: second.Token})
	testkit.RequireNoError(t, err)
	testkit.RequireCode(t, m.CheckVerification(t.Context(), next, proof, "login_link.create"), core.Unauthenticated)
	testkit.RequireCode(t, m.CheckCSRF(t.Context(), Authentication{Session: second.Token}, first.CSRF, f.TrustedOrigin), core.Forbidden)
	links := newLink(t, f, admin)
	issued, err := m.RedeemLoginLink(t.Context(), RedeemLoginLinkRequest{Token: links.Value, Client: client})
	testkit.RequireNoError(t, err)
	if issued.Session.Session.ID == second.Session.ID {
		t.Fatal("link did not rotate identity")
	}
	testkit.RequireNoError(t, m.RevokeSession(t.Context(), admin, issued.Session.Session.ID))
	third := deviceLogin(t, f, client)
	if !third.Session.CreatedAt.Equal(first.Session.CreatedAt) {
		t.Fatal("logout lost device identity")
	}
	rows, err := persistence.Select[StoredSession](t.Context(), m.store, "auth.sessions", persistence.Query{Where: []persistence.Filter{persistence.Eq("DeviceDigest", digest(client.DeviceID))}})
	testkit.RequireNoError(t, err)
	if len(rows) != 1 || rows[0].Session.ID != third.Session.ID {
		t.Fatal("device history accumulated")
	}
}

func TestDeviceSessionsAreAccountScopedAndOtherDevicesRevokeIndependently(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	m.options.LoginAttemptsPerMinute = 100
	web := Client{DeviceID: strings.Repeat("w", 48), ClientType: "web", DeviceName: "Safari / iPhone"}
	app := Client{DeviceID: strings.Repeat("i", 48), ClientType: "ios", DeviceName: "iPhone"}
	first, second := deviceLogin(t, f, web), deviceLogin(t, f, app)
	_, err := m.CreateUser(t.Context(), admin, CreateUserRequest{Username: "alice", GroupID: DefaultGroupID, Password: core.NewSecret("alice-unique-password-2026"), Mutation: core.Mutation{IdempotencyKey: "device-alice"}})
	testkit.RequireNoError(t, err)
	alice, err := m.Login(t.Context(), LoginRequest{Username: "alice", Password: core.NewSecret("alice-unique-password-2026"), Client: web})
	testkit.RequireNoError(t, err)
	p, err := m.Authenticate(t.Context(), Authentication{Session: first.Token})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, m.RevokeOtherSessions(t.Context(), p))
	page, err := m.Sessions(t.Context(), p, core.PageRequest{Limit: 1})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || !page.Items[0].Current || page.Items[0].ClientType != "web" || page.NextCursor != "" {
		t.Fatal("inactive sessions remained visible", page)
	}
	_, err = m.Authenticate(t.Context(), Authentication{Session: second.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = m.Authenticate(t.Context(), Authentication{Session: alice.Session.Token})
	testkit.RequireNoError(t, err)
	f.Advance(m.options.SessionIdle)
	_, err = m.Authenticate(t.Context(), Authentication{Session: first.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestDeviceMFAIsBoundAndStaleChallengesCannotReplaceNewLogin(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	m.options.LoginAttemptsPerMinute = 100
	client := Client{DeviceID: strings.Repeat("a", 48), ClientType: "ios", DeviceName: "iPhone"}
	old := deviceLogin(t, f, client)
	enrollment, err := m.BeginMFAEnrollment(t.Context(), admin, verify(t, f, admin, "mfa.enroll"))
	testkit.RequireNoError(t, err)
	codes, err := m.ConfirmMFAEnrollment(t.Context(), admin, ConfirmMFARequest{EnrollmentID: enrollment.ID, Code: f.Code(enrollment)})
	testkit.RequireNoError(t, err)
	begin := func() LoginResult {
		r, err := m.Login(t.Context(), LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), Client: client})
		testkit.RequireNoError(t, err)
		return r
	}
	one, two := begin(), begin()
	_, err = m.Authenticate(t.Context(), Authentication{Session: old.Token})
	testkit.RequireNoError(t, err)
	_, err = m.CompleteMFA(t.Context(), MFAChallengeResponse{Challenge: one.Challenge.ID, Code: codes.Codes[0], Client: Client{DeviceID: strings.Repeat("b", 48)}})
	testkit.RequireCode(t, err, core.Unauthenticated)
	issued, err := m.CompleteMFA(t.Context(), MFAChallengeResponse{Challenge: one.Challenge.ID, Code: codes.Codes[0], Client: client})
	testkit.RequireNoError(t, err)
	if issued.Session.DeviceName != "iPhone" || issued.Session.ClientType != "ios" {
		t.Fatal("MFA lost original device metadata")
	}
	_, err = m.CompleteMFA(t.Context(), MFAChallengeResponse{Challenge: two.Challenge.ID, Code: codes.Codes[1], Client: client})
	testkit.RequireCode(t, err, core.Conflict)
	_, err = m.Authenticate(t.Context(), Authentication{Session: issued.Token})
	testkit.RequireNoError(t, err)
}

func TestDeviceGenerationSerializesConcurrentIssuance(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	m := f.Service.(*Manager)
	client := Client{DeviceID: strings.Repeat("a", 48), ClientType: "web"}
	testkit.RequireNoError(t, m.prepareDevice(t.Context(), p.OwnerID, &client))
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() { _, err := m.issueSession(t.Context(), p.OwnerID, client, true); results <- err })
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if core.CodeOf(err) == core.Conflict {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("parallel logins both succeeded")
	}
}

func TestAuthCollectionBoundsAndPreservesLiveCredentials(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	m := f.Service.(*Manager)
	cutoff := f.Now.Add(-credentialRetention)
	old := cutoff.Add(-time.Hour)
	testkit.RequireNoError(t, m.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, row := range []StoredSession{
			{Session: Session{ID: "idle", AccountID: p.OwnerID, LastSeenAt: cutoff.Add(-m.options.SessionIdle), ExpiresAt: f.Now.Add(time.Hour)}},
			{Session: Session{ID: "absolute", AccountID: p.OwnerID, LastSeenAt: f.Now, ExpiresAt: cutoff}},
			{Session: Session{ID: "revoked", AccountID: p.OwnerID, LastSeenAt: f.Now, ExpiresAt: f.Now.Add(time.Hour)}, RevokedAt: &old},
			{Session: Session{ID: "recent", AccountID: p.OwnerID, LastSeenAt: f.Now, ExpiresAt: f.Now.Add(time.Hour)}, RevokedAt: &f.Now},
		} {
			if err := tx.Put(ctx, "auth.sessions", persistence.Key(p.OwnerID, row.Session.ID), row); err != nil {
				return err
			}
		}
		if err := tx.Put(ctx, loginLinksBucket, "old", storedLoginLink{Link: LoginLink{ExpiresAt: cutoff}}); err != nil {
			return err
		}
		if err := tx.Put(ctx, loginLinksBucket, "pending", storedLoginLink{Link: LoginLink{ExpiresAt: f.Now.Add(time.Minute), UsedAt: &f.Now, PendingMFAUntil: timePtr(f.Now.Add(time.Minute))}}); err != nil {
			return err
		}
		for key, expiry := range map[string]time.Time{"expired": f.Now, "pending": f.Now.Add(time.Minute)} {
			if err := tx.Put(ctx, "auth.challenges", key, StoredChallenge{ExpiresAt: expiry}); err != nil {
				return err
			}
		}
		return nil
	}))
	total := 0
	for {
		n, err := m.Collect(t.Context(), 2)
		testkit.RequireNoError(t, err)
		if n > 2 {
			t.Fatal("unbounded cleanup")
		}
		total += n
		if n < 2 {
			break
		}
	}
	if total != 5 {
		t.Fatalf("deleted %d, wanted 5", total)
	}
	testkit.RequireNoError(t, m.ValidatePrincipal(t.Context(), p))
	for _, item := range []struct{ bucket, key string }{
		{"auth.sessions", persistence.Key(p.OwnerID, "recent")}, {loginLinksBucket, "pending"}, {"auth.challenges", "pending"},
	} {
		testkit.RequireNoError(t, m.store.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			var row map[string]any
			return tx.Get(ctx, item.bucket, item.key, &row)
		}))
	}
}

func timePtr(at time.Time) *time.Time { return &at }
