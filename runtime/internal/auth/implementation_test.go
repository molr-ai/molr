package auth

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
		initial := now
		var mu sync.Mutex
		clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		manager, err := NewService(store, Options{Now: clock, MasterKey: make([]byte, 32), Origin: "https://molr.example"})
		testkit.RequireNoError(t, err)
		challenge, csrf, err := manager.PreLoginCSRF(testkit.Context(t))
		testkit.RequireNoError(t, err)
		return contractFixture{Service: manager, Bootstrap: manager, Repository: NewRepository(store), Browser: manager, Now: initial, Advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }, Code: func(e MFAEnrollment) core.Secret {
			u, err := url.Parse(e.ProvisioningURI.Reveal())
			testkit.RequireNoError(t, err)
			return core.NewSecret(totp(u.Query().Get("secret"), clock()))
		}, TrustedOrigin: "https://molr.example", PreLoginCSRF: Authentication{Session: challenge.ID}, PreLoginToken: csrf}
	}
}
func TestLivePrincipalValidationRejectsRevocationAndTokenScope(t *testing.T) {
	f := fixture(t)
	p, s := initialized(t, f)
	manager := f.Service.(*Manager)
	ctx := testkit.Context(t)
	testkit.RequireNoError(t, manager.ValidatePrincipal(ctx, p))
	issued, err := manager.CreateToken(ctx, p, CreateTokenRequest{Name: "scoped", Scopes: []string{"read"}, ExpiresAt: f.Now.Add(time.Hour), Verification: verify(t, f, p, "token.create")})
	testkit.RequireNoError(t, err)
	tokenPrincipal, err := manager.Authenticate(ctx, Authentication{Bearer: issued.Value})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, manager.CheckScope(ctx, tokenPrincipal, "read"))
	testkit.RequireCode(t, manager.CheckScope(ctx, tokenPrincipal, "write"), core.Forbidden)
	testkit.RequireNoError(t, manager.RevokeSession(ctx, p, s.Session.ID))
	testkit.RequireCode(t, manager.ValidatePrincipal(ctx, p), core.Unauthenticated)
}
func TestAuthRestartPreservesCredentialsWithoutPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	store, err := sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	opts := Options{MasterKey: make([]byte, 32), Origin: "https://molr.example"}
	m, err := NewService(store, opts)
	testkit.RequireNoError(t, err)
	_, err = m.Initialize(context.Background(), InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, store.Close())
	store, err = sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	defer store.Close()
	m, err = NewService(store, opts)
	testkit.RequireNoError(t, err)
	r, err := m.Login(context.Background(), LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireNoError(t, err)
	if r.Session == nil {
		t.Fatal("restart lost password hash")
	}
}

func TestExplicitBrowserOriginsRemainCSRFBound(t *testing.T) {
	f := fixture(t)
	_, session := initialized(t, f)
	original := f.Service.(*Manager)
	configured, err := NewService(original.store, Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", AllowedOrigins: []string{"https://client.example"}, Now: func() time.Time { return f.Now }})
	testkit.RequireNoError(t, err)
	a := Authentication{Session: session.Token}
	testkit.RequireNoError(t, configured.CheckCSRF(testkit.Context(t), a, session.CSRF, "https://client.example"))
	if err := original.CheckCSRF(testkit.Context(t), a, session.CSRF, "https://client.example"); err == nil {
		t.Fatal("unconfigured browser origin accepted")
	}
	if err := configured.CheckCSRF(testkit.Context(t), a, core.NewSecret("wrong"), "https://client.example"); err == nil {
		t.Fatal("allowed origin bypassed CSRF token binding")
	}
	if _, err := NewService(original.store, Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", AllowedOrigins: []string{"https://*.example"}}); err == nil {
		t.Fatal("wildcard browser origin accepted")
	}
}

func TestLoginAddressQuarantineOutlivesAccountWindow(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth-limits.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	manager, err := NewService(store, Options{Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }, MasterKey: make([]byte, 32), Origin: "https://molr.example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = manager.Initialize(ctx, InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")}); err != nil {
		t.Fatal(err)
	}
	attacker := Client{Address: "192.0.2.10"}
	// The attacker exhausts both buckets with wrong passwords.
	for i := 0; i < 5; i++ {
		if _, err = manager.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("wrong-password-attempt"), Client: attacker}); core.CodeOf(err) != core.Unauthenticated {
			t.Fatalf("expected denial, got %v", err)
		}
	}
	if _, err = manager.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), Client: attacker}); core.CodeOf(err) != core.RateLimited {
		t.Fatalf("expected rate limit on sixth attempt, got %v", err)
	}
	// Two minutes later the one-minute account window has reset, but the
	// ten-minute address quarantine must still hold the attacker back.
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	if _, err = manager.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("wrong-password-attempt"), Client: attacker}); core.CodeOf(err) != core.RateLimited {
		t.Fatalf("expected address quarantine to persist, got %v", err)
	}
	// The owner from a different address is unaffected by the quarantine and
	// by anything the attacker poured into the account bucket.
	owner := Client{Address: "198.51.100.20"}
	result, err := manager.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), Client: owner})
	if err != nil {
		t.Fatalf("owner login must succeed: %v", err)
	}
	if result.Session == nil {
		t.Fatal("owner session missing")
	}
	// After the quarantine expires the attacker may try again.
	mu.Lock()
	now = now.Add(9 * time.Minute)
	mu.Unlock()
	if _, err = manager.Login(ctx, LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), Client: attacker}); err != nil {
		t.Fatalf("quarantine must expire: %v", err)
	}
}
