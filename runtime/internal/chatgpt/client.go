// Package chatgpt implements the fixed public Sign in with ChatGPT protocol.
// Sessions are secrets: persist them only inside the credential vault.
package chatgpt

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

const Origin = "https://auth.openai.com"
const Resource = "https://api.openai.com/v1"
const Endpoint = Resource + "/responses"
const Scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

type Session struct {
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type Client struct {
	HTTP *http.Client
	Now  func() time.Time
}

func New() *Client { return &Client{HTTP: platform.PublicHTTPClient(Origin), Now: time.Now} }
func (c *Client) request(ctx context.Context, method, path string, form url.Values, out any) error {
	// Discovery URLs are checked too; neither pages nor claims select a server.
	u, err := url.Parse(path)
	if err != nil {
		return core.Invalid("oauth", "invalid endpoint")
	}
	if u.IsAbs() {
		if u.Scheme+"://"+u.Host != Origin || u.User != nil || u.Fragment != "" {
			return core.Invalid("oauth", "unexpected authority")
		}
	} else {
		path = Origin + path
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, path, strings.NewReader(form.Encode()))
	if err != nil {
		return core.Invalid("oauth", "invalid request")
	}
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return core.NewError(core.Unavailable, "ChatGPT authorization service unavailable")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return core.NewError(core.Unavailable, "invalid ChatGPT response")
	}
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if failure.Error == "invalid_grant" {
			return core.WithReason(core.NewError(core.FailedPrecondition, "ChatGPT requires reconnection"), core.ModelAuthenticationFailed)
		}
		return core.NewError(core.Unavailable, "ChatGPT authorization request failed")
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return core.NewError(core.Unavailable, "invalid ChatGPT response")
	}
	return nil
}

type discovery struct {
	Issuer     string `json:"issuer"`
	JWKS       string `json:"jwks_uri"`
	Revocation string `json:"revocation_endpoint"`
}

func (c *Client) discovery(ctx context.Context) (discovery, error) {
	var d discovery
	err := c.request(ctx, http.MethodGet, "/.well-known/openid-configuration", nil, &d)
	if err == nil && d.Issuer != Origin {
		err = core.Invalid("issuer", "unexpected OpenAI issuer")
	}
	return d, err
}
func (c *Client) identity(ctx context.Context, token, clientID, nonce string) (string, string, error) {
	bad := func() (string, string, error) {
		return "", "", core.NewError(core.Unauthenticated, "invalid ChatGPT identity")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32768 {
		return bad()
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return bad()
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || h.Kid == "" {
		return bad()
	}
	d, err := c.discovery(ctx)
	if err != nil {
		return "", "", err
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if d.JWKS == "" {
		return bad()
	}
	if err = c.request(ctx, http.MethodGet, d.JWKS, nil, &jwks); err != nil {
		return "", "", err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return bad()
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	verified := false
	for _, k := range jwks.Keys {
		if k.Kid != h.Kid || k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, e1 := base64.RawURLEncoding.DecodeString(k.N)
		e, e2 := base64.RawURLEncoding.DecodeString(k.E)
		if e1 != nil || e2 != nil || len(n) < 256 || len(n) > 1024 || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent*256 + int(b)
		}
		if exponent < 3 {
			continue
		}
		key := rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
		verified = rsa.VerifyPKCS1v15(&key, crypto.SHA256, sum[:], signature) == nil
		if verified {
			break
		}
	}
	if !verified {
		return bad()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return bad()
	}
	var claims struct {
		Issuer    string          `json:"iss"`
		Subject   string          `json:"sub"`
		Email     string          `json:"email"`
		Audience  json.RawMessage `json:"aud"`
		AZP       string          `json:"azp"`
		Expires   int64           `json:"exp"`
		NotBefore int64           `json:"nbf"`
		Nonce     string          `json:"nonce"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return bad()
	}
	aud := []string{}
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil {
		aud = []string{single}
	} else {
		_ = json.Unmarshal(claims.Audience, &aud)
	}
	if claims.Issuer != Origin || claims.Subject == "" || len(claims.Subject) > 512 || len(claims.Email) > 512 || !slices.Contains(aud, clientID) || (len(aud) > 1 && claims.AZP != clientID) || (claims.AZP != "" && claims.AZP != clientID) || claims.Expires <= c.Now().Unix() || claims.NotBefore > c.Now().Unix()+30 || (nonce != "" && claims.Nonce != nonce) {
		return bad()
	}
	return claims.Subject, claims.Email, nil
}
func (c *Client) tokens(ctx context.Context, form url.Values, old Session, nonce string) (Session, error) {
	var r struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		ID      string `json:"id_token"`
		Type    string `json:"token_type"`
		Scope   string `json:"scope"`
		Expires int64  `json:"expires_in"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/accounts/oauth/token", form, &r); err != nil {
		return Session{}, err
	}
	if r.Access == "" || r.Refresh == "" || !strings.EqualFold(r.Type, "Bearer") || r.Expires <= 0 || r.Expires > 86400 {
		return Session{}, core.NewError(core.Unavailable, "incomplete ChatGPT credentials")
	}
	s := old
	s.AccessToken = r.Access
	s.RefreshToken = r.Refresh
	s.ExpiresAt = c.Now().Add(time.Duration(r.Expires) * time.Second)
	if r.Scope != "" {
		s.Scope = r.Scope
	}
	if !slices.Contains(strings.Fields(s.Scope), "chatgpt.tokens.use.direct") || !slices.Contains(strings.Fields(s.Scope), "resource.invoke") || !slices.Contains(strings.Fields(s.Scope), "offline_access") {
		return Session{}, core.NewError(core.Forbidden, "ChatGPT plan permission not granted")
	}
	if r.ID != "" {
		sub, email, err := c.identity(ctx, r.ID, s.ClientID, nonce)
		if err != nil {
			return Session{}, err
		}
		if old.Subject != "" && old.Subject != sub {
			return Session{}, core.NewError(core.Forbidden, "ChatGPT account changed")
		}
		s.Subject, s.Email, s.IDToken = sub, email, r.ID
	} else if nonce != "" {
		return Session{}, core.NewError(core.Unauthenticated, "ChatGPT identity missing")
	}
	return s, nil
}
func (c *Client) Exchange(ctx context.Context, clientID, code, verifier, redirect, nonce, subject string) (Session, error) {
	if clientID == "" || clientID == "dynamic_agent_client" {
		return Session{}, core.Invalid("client_id", "issued client ID required")
	}
	return c.tokens(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "resource": {Resource}}, Session{ClientID: clientID, Subject: subject}, nonce)
}
func (c *Client) Resolve(ctx context.Context, secret core.Secret) (core.Secret, core.Secret, error) {
	var s Session
	if json.Unmarshal([]byte(secret.Reveal()), &s) != nil || s.ClientID == "" || s.Subject == "" || s.AccessToken == "" || s.RefreshToken == "" {
		return core.Secret{}, core.Secret{}, core.NewError(core.FailedPrecondition, "ChatGPT requires reconnection")
	}
	if c.Now().Add(time.Minute).Before(s.ExpiresAt) {
		return core.Secret{}, core.NewSecret(s.AccessToken), nil
	}
	updated, err := c.tokens(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {s.ClientID}, "refresh_token": {s.RefreshToken}, "resource": {Resource}}, s, "")
	if err != nil {
		return core.Secret{}, core.Secret{}, err
	}
	data, _ := json.Marshal(updated)
	return core.NewSecret(string(data)), core.NewSecret(updated.AccessToken), nil
}
func (c *Client) Revoke(ctx context.Context, secret core.Secret) error {
	var s Session
	if json.Unmarshal([]byte(secret.Reveal()), &s) != nil || s.RefreshToken == "" {
		return core.Invalid("session", "invalid ChatGPT session")
	}
	d, err := c.discovery(ctx)
	if err != nil {
		return err
	}
	if d.Revocation == "" {
		return core.NewError(core.Unavailable, "ChatGPT revocation unavailable")
	}
	return c.request(ctx, http.MethodPost, d.Revocation, url.Values{"token": {s.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {s.ClientID}}, nil)
}
