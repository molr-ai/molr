package oauthbroker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
)

type mockProvider struct {
	auth             Authorization
	calls            atomic.Int32
	fail             bool
	entered, release chan struct{}
}

func (m *mockProvider) AuthorizationURL(_ context.Context, _ core.Principal, a Authorization) (string, error) {
	m.auth = a
	q := url.Values{"redirect_uri": {a.RedirectURI}, "state": {a.State}, "nonce": {a.Nonce}, "code_challenge": {a.Challenge}, "code_challenge_method": {"S256"}, "response_type": {"code"}}
	return "https://provider.example/authorize?" + q.Encode(), nil
}
func (m *mockProvider) Complete(_ context.Context, _ core.Principal, g Grant) (core.ID, error) {
	m.calls.Add(1)
	if m.entered != nil {
		close(m.entered)
		<-m.release
	}
	hash := sha256.Sum256([]byte(g.Verifier.Reveal()))
	if m.fail || g.Code.Reveal() != "fixture-code" || g.RedirectURI != m.auth.RedirectURI || g.Nonce != m.auth.Nonce || base64.RawURLEncoding.EncodeToString(hash[:]) != m.auth.Challenge {
		return "", errors.New("private-provider-error-with-token")
	}
	return "fixture-connection", nil
}
func owner() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
}
func setup(t *testing.T) (*Broker, *mockProvider, Started) {
	t.Helper()
	m := &mockProvider{}
	b := New(map[string]Provider{"fixture": m}, func(context.Context, core.Principal) error { return nil })
	a, err := b.Start(t.Context(), owner(), StartRequest{Provider: "fixture", RedirectURI: "http://127.0.0.1:1455/auth/callback"})
	if err != nil {
		t.Fatal(err)
	}
	return b, m, a
}
func callback(a Started) CompleteRequest {
	return CompleteRequest{ID: a.ID, State: a.State, Code: "fixture-code"}
}
func TestExchangeAndReplay(t *testing.T) {
	b, m, a := setup(t)
	for range 2 {
		result, err := b.Complete(t.Context(), owner(), callback(a))
		if err != nil || result.Status != "connected" || result.ConnectionID != "fixture-connection" {
			t.Fatalf("completion: %+v %v", result, err)
		}
	}
	if m.calls.Load() != 1 {
		t.Fatal("code exchanged twice")
	}
	if b.attempts[a.ID].verifier != "" {
		t.Fatal("verifier retained after exchange")
	}
}
func TestStateIdentityAndRevocation(t *testing.T) {
	b, m, a := setup(t)
	bad := callback(a)
	bad.State = "wrong"
	if _, err := b.Complete(t.Context(), owner(), bad); err == nil {
		t.Fatal("wrong state accepted")
	}
	other := owner()
	other.SessionID = "other-session"
	if _, err := b.Complete(t.Context(), other, callback(a)); err == nil {
		t.Fatal("another session accepted")
	}
	other.OwnerID = "other"
	other.SubjectID = "other"
	if _, err := b.Status(t.Context(), other, a.ID); err == nil {
		t.Fatal("another owner read attempt")
	}
	b.validate = func(context.Context, core.Principal) error { return core.NewError(core.Unauthenticated, "revoked") }
	if _, err := b.Complete(t.Context(), owner(), callback(a)); err == nil {
		t.Fatal("revoked identity accepted")
	}
	if m.calls.Load() != 0 {
		t.Fatal("provider called for invalid identity")
	}
}
func TestCancelledDeniedExpiredAndFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "deny", "expire", "failure"} {
		t.Run(mode, func(t *testing.T) {
			b, m, a := setup(t)
			q := callback(a)
			switch mode {
			case "cancel":
				if _, err := b.Cancel(t.Context(), owner(), a.ID); err != nil {
					t.Fatal(err)
				}
			case "deny":
				q.Code = ""
				q.Error = "access_denied"
			case "expire":
				b.now = func() time.Time { return a.ExpiresAt }
			case "failure":
				m.fail = true
			}
			result, err := b.Complete(t.Context(), owner(), q)
			if mode == "expire" {
				if err == nil {
					t.Fatal("expired callback accepted")
				}
				return
			}
			want := "cancelled"
			if mode == "failure" {
				want = "failed"
			}
			if err != nil || result.Status != want {
				t.Fatalf("%+v %v", result, err)
			}
			if mode != "failure" && m.calls.Load() != 0 {
				t.Fatal("cancelled code exchanged")
			}
		})
	}
}
func TestConcurrentCompletion(t *testing.T) {
	b, m, a := setup(t)
	m.entered = make(chan struct{})
	m.release = make(chan struct{})
	done := make(chan Result)
	go func() { r, _ := b.Complete(t.Context(), owner(), callback(a)); done <- r }()
	<-m.entered
	result, err := b.Complete(t.Context(), owner(), callback(a))
	if err != nil || result.Status != "completing" {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = b.Cancel(t.Context(), owner(), a.ID)
	if err != nil || result.Status != "completing" {
		t.Fatal("cancellation misreported after exchange started")
	}
	close(m.release)
	if (<-done).Status != "connected" || m.calls.Load() != 1 {
		t.Fatal("concurrent exchange")
	}
}
func TestStartBoundaries(t *testing.T) {
	b, _, _ := setup(t)
	if _, err := b.Start(t.Context(), owner(), StartRequest{Provider: "fixture", RedirectURI: "http://127.0.0.1:1234/auth/callback"}); err == nil {
		t.Fatal("concurrent authorization accepted")
	}
	for _, uri := range []string{"http://localhost:1234/auth/callback", "http://192.168.1.2:1234/auth/callback", "https://127.0.0.1:1234/auth/callback", "http://127.0.0.1:1234/auth/callback?x=1", "http://127.0.0.1:0/auth/callback"} {
		if validRedirect(uri) {
			t.Fatal("invalid callback accepted", uri)
		}
	}
	p := owner()
	p.SessionID = ""
	p.TokenID = "token"
	if _, err := b.Start(t.Context(), p, StartRequest{Provider: "fixture", RedirectURI: "http://127.0.0.1:1234/auth/callback"}); err == nil {
		t.Fatal("bearer token accepted")
	}
	if _, err := b.Start(t.Context(), owner(), StartRequest{Provider: "unknown", RedirectURI: "http://127.0.0.1:1234/auth/callback"}); err == nil {
		t.Fatal("unregistered provider accepted")
	}
}
