package chatgpt

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestChatGPTIdentityExchangeRefreshAndRevocation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nonce := "nonce"
	subject := "account"
	scope := Scopes
	badSignature := false
	refreshes := 0
	revoked := false
	sign := func() string {
		h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
		p, _ := json.Marshal(map[string]any{"iss": Origin, "sub": subject, "aud": "issued", "nonce": nonce, "exp": now.Add(time.Hour).Unix(), "email": "test@example.invalid"})
		body := h + "." + base64.RawURLEncoding.EncodeToString(p)
		sum := sha256.Sum256([]byte(body))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if badSignature {
			sig[0] ^= 1
		}
		return body + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	c := New()
	c.Now = func() time.Time { return now }
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != Origin {
			t.Fatal("unexpected origin")
		}
		var value any
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			value = map[string]any{"issuer": Origin, "jwks_uri": Origin + "/keys", "revocation_endpoint": Origin + "/revoke"}
		case "/keys":
			value = map[string]any{"keys": []any{map[string]any{"kid": "test", "kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB", "alg": "RS256"}}}
		case "/api/accounts/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "issued" || r.Form.Get("resource") != Resource {
				t.Error("incorrect token request")
			}
			refresh := r.Form.Get("grant_type") == "refresh_token"
			if refresh {
				refreshes++
				if r.Form.Get("refresh_token") != "refresh-one" || r.Form.Has("scope") {
					t.Error("incorrect refresh")
				}
			} else if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != "http://127.0.0.1:1234/auth/callback" {
				t.Error("incorrect exchange")
			}
			value = map[string]any{"access_token": "access-one", "refresh_token": "refresh-one", "id_token": sign(), "token_type": "Bearer", "scope": scope, "expires_in": 3600}
			if refresh {
				value.(map[string]any)["access_token"] = "access-two"
				value.(map[string]any)["refresh_token"] = "refresh-two"
			}
		case "/revoke":
			_ = r.ParseForm()
			revoked = r.Form.Get("client_id") == "issued" && r.Form.Get("token") == "refresh-two"
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		data, _ := json.Marshal(value)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})}
	exchange := func() (Session, error) {
		return c.Exchange(t.Context(), "issued", "code", "verifier", "http://127.0.0.1:1234/auth/callback", "nonce", "")
	}
	s, err := exchange()
	if err != nil {
		t.Fatal(err)
	}
	if s.Subject != "account" || s.Email != "test@example.invalid" {
		t.Fatal("identity lost")
	}
	raw, _ := json.Marshal(s)
	updated, bearer, err := c.Resolve(t.Context(), core.NewSecret(string(raw)))
	if err != nil || !updated.Empty() || bearer.Reveal() != "access-one" {
		t.Fatal("fresh token resolution failed", err)
	}
	now = now.Add(59*time.Minute + 30*time.Second)
	updated, bearer, err = c.Resolve(t.Context(), core.NewSecret(string(raw)))
	if err != nil || bearer.Reveal() != "access-two" || refreshes != 1 {
		t.Fatal("refresh failed", err)
	}
	if err = c.Revoke(t.Context(), updated); err != nil || !revoked {
		t.Fatal("revocation failed", err)
	}
	for _, change := range []func(){func() { nonce = "wrong" }, func() { nonce = "nonce"; badSignature = true }, func() { badSignature = false; scope = "openid profile email" }} {
		change()
		if _, err = exchange(); err == nil {
			t.Fatal("invalid identity or scope accepted")
		}
	}
	scope = Scopes
	subject = "different"
	if _, err = c.Exchange(t.Context(), "issued", "code", "verifier", "http://127.0.0.1:1234/auth/callback", "nonce", "account"); err == nil {
		t.Fatal("replaced account identity")
	}
	if _, err = c.Exchange(context.Background(), "dynamic_agent_client", "", "", "", "", ""); err == nil {
		t.Fatal("dynamic entrypoint accepted as client")
	}
}
func TestChatGPTRejectsUntrustedDiscoveryAndRefreshErrors(t *testing.T) {
	c := New()
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"secret"}`)), Header: http.Header{}}, nil
	})}
	data, _ := json.Marshal(Session{ClientID: "issued", Subject: "user", RefreshToken: "expired", AccessToken: "access"})
	_, _, err := c.Resolve(t.Context(), core.NewSecret(string(data)))
	if core.CodeOf(err) != core.FailedPrecondition || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsafe refresh error", err)
	}
	if err = c.request(t.Context(), "GET", "https://evil.example/keys", nil, nil); err == nil {
		t.Fatal("foreign discovery authority accepted")
	}
}
