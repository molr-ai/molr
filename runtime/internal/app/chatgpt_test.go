package app

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/chatgpt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/oauthbroker"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type chatgptTransport func(*http.Request) (*http.Response, error)

func (f chatgptTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestChatGPTAdminConnectionLifecycle(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	vault := f.b.Services.Credentials.(*credential.Vault)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	need(t, err)
	nonce, subject := "", "subscriber"
	exchanges := 0
	failExchange := false
	revocationFailed := false
	client := chatgpt.New()
	client.HTTP = &http.Client{Transport: chatgptTransport(func(r *http.Request) (*http.Response, error) {
		var value any
		status := 200
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			value = map[string]any{"issuer": chatgpt.Origin, "jwks_uri": chatgpt.Origin + "/keys", "revocation_endpoint": chatgpt.Origin + "/revoke"}
		case "/keys":
			value = map[string]any{"keys": []any{map[string]any{"kid": "test", "kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}}
		case "/api/accounts/oauth/token":
			exchanges++
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "issued" {
				t.Error("client registration lost")
			}
			h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
			claims, _ := json.Marshal(map[string]any{"iss": chatgpt.Origin, "aud": "issued", "sub": subject, "email": "fixture@example.invalid", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix()})
			payload := h + "." + base64.RawURLEncoding.EncodeToString(claims)
			hash := sha256.Sum256([]byte(payload))
			sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
			value = map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "id_token": payload + "." + base64.RawURLEncoding.EncodeToString(sig), "scope": chatgpt.Scopes, "token_type": "Bearer", "expires_in": 3600}
			if failExchange {
				status = 400
				value = map[string]any{"error": "invalid_grant"}
			}
		case "/revoke":
			if revocationFailed {
				status = 503
			}
			value = map[string]any{}
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		data, _ := json.Marshal(value)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})}
	s := &chatgptProvider{store: f.b.store, identity: f.b.Services.Auth.(*auth.Manager), vault: vault, client: client}
	broker := oauthbroker.New(map[string]oauthbroker.Provider{"chatgpt": s}, s.identity.ValidatePrincipal)
	start := func(p core.Principal, id core.ID, version core.Version) (oauthbroker.Started, error) {
		a, err := broker.Start(ctx, p, oauthbroker.StartRequest{Provider: "chatgpt", RedirectURI: "http://127.0.0.1:1234/auth/callback", ConnectionID: id, ExpectedVersion: version, Name: "Shared ChatGPT"})
		if err == nil {
			u, _ := url.Parse(a.AuthorizationURL)
			nonce = u.Query().Get("nonce")
		}
		return a, err
	}
	if _, err = start(f.alice, "", 0); core.CodeOf(err) != core.Forbidden {
		t.Fatal("member could add OAuth provider", err)
	}
	a, err := start(f.admin, "", 0)
	need(t, err)
	u, _ := url.Parse(a.AuthorizationURL)
	host := u.Query().Get("ext_agent_host_id")
	if u.Query().Get("client_id") != "dynamic_agent_client" || u.Query().Get("agent_name_hint") != "Molr" || host == "" {
		t.Fatal("invalid registration")
	}
	result, err := broker.Complete(ctx, f.admin, oauthbroker.CompleteRequest{ID: a.ID, State: a.State, Code: "code", ClientID: "issued"})
	need(t, err)
	if result.Status != "connected" {
		t.Fatal("connection failed", result.Status)
	}
	replay, err := broker.Complete(ctx, f.admin, oauthbroker.CompleteRequest{ID: a.ID, State: a.State, Code: "code", ClientID: "issued"})
	need(t, err)
	if replay.ConnectionID != result.ConnectionID || exchanges != 1 {
		t.Fatal("duplicate exchange")
	}
	connection, err := f.b.Services.Resources.ProviderConnection(ctx, f.admin, result.ConnectionID)
	need(t, err)
	if connection.ChatGPT.Subject != "subscriber" || connection.Resource.Authentication != "chatgpt" {
		t.Fatal("registration not bound")
	}
	// The generic API-key writer must not change an OAuth connection's endpoint or secret.
	_, err = f.b.Services.Resources.SaveProvider(ctx, f.admin, resource.SaveProviderRequest{ID: result.ConnectionID, Name: "Changed", PricingProvider: "openai", Endpoint: "https://evil.example/v1/chat/completions", Mutation: core.Mutation{IdempotencyKey: "alter", ExpectedVersion: connection.Resource.Version}})
	if err == nil {
		t.Fatal("OAuth binding edited through API-key route")
	}
	repo := resource.NewRepository(f.b.store)
	binding, err := repo.Binding(ctx, f.admin.OwnerID, result.ConnectionID)
	need(t, err)
	binding.Models = []resource.ModelConfig{{Name: "fixture-model", SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 1000000}}}
	need(t, repo.SaveBinding(ctx, binding))
	stored, err := repo.BindingForModel(ctx, f.admin.OwnerID, resource.ModelID(result.ConnectionID, "fixture-model"))
	need(t, err)
	if stored.ChatGPT == nil {
		t.Fatal("model membership lost")
	}
	catalog := adapters{store: f.b.store}.modelCatalog()
	p, err := catalog.Resolve(ctx, f.admin, resource.ModelID(result.ConnectionID, "fixture-model"))
	need(t, err)
	if p.Protocol != model.OpenAIResponses || p.Model.PricingInfo.Basis != "subscription-reference" || p.Pricing.InputMicrosPerMillion != 1000000 {
		t.Fatal("protocol or reference pricing lost")
	}
	_, err = catalog.Resolve(ctx, f.alice, p.Model.ID)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("unassigned member can use subscription", err)
	}
	a, err = start(f.admin, result.ConnectionID, connection.Resource.Version)
	need(t, err)
	u, _ = url.Parse(a.AuthorizationURL)
	if u.Query().Get("client_id") != "issued" || u.Query().Get("id_token_hint") == "" || u.Query().Get("ext_agent_host_id") != host || u.Query().Has("agent_name_hint") {
		t.Fatal("reauthorization changed registration")
	}
	subject = "another-account"
	failed, err := broker.Complete(ctx, f.admin, oauthbroker.CompleteRequest{ID: a.ID, State: a.State, Code: "other"})
	need(t, err)
	if failed.Status != "failed" {
		t.Fatal("different account replaced connection")
	}
	revocationFailed = true
	revoked, err := s.disconnect(ctx, f.admin, resource.DeleteProviderRequest{ID: result.ConnectionID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: connection.Resource.Version}})
	need(t, err)
	if revoked {
		t.Fatal("claimed remote revocation")
	}
	replayRevoked, replayErr := s.disconnect(ctx, f.admin, resource.DeleteProviderRequest{ID: result.ConnectionID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: connection.Resource.Version}})
	need(t, replayErr)
	if replayRevoked {
		t.Fatal("disconnect replay changed outcome")
	}

	connection, err = f.b.Services.Resources.ProviderConnection(ctx, f.admin, result.ConnectionID)
	need(t, err)
	if connection.CredentialID != "" || connection.Resource.State != resource.Disconnected || connection.ChatGPT.ClientID != "issued" {
		t.Fatal("logout lost registration or retained credentials")
	}
	a, err = start(f.admin, result.ConnectionID, connection.Resource.Version)
	need(t, err)
	u, _ = url.Parse(a.AuthorizationURL)
	if u.Query().Has("id_token_hint") {
		t.Fatal("logged out ID token retained")
	}
	_, err = broker.Cancel(ctx, f.admin, a.ID)
	need(t, err)
	failExchange = true
	a, err = start(f.admin, "", 0)
	need(t, err)
	failed, err = broker.Complete(ctx, f.admin, oauthbroker.CompleteRequest{ID: a.ID, State: a.State, Code: "expired", ClientID: "issued"})
	need(t, err)
	if failed.Status != "failed" || failed.ConnectionID == "" {
		t.Fatal("issued client not retained on invalid grant")
	}
}
func TestChatGPTModelCatalogIsAccountSpecific(t *testing.T) {
	client := &http.Client{Transport: chatgptTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != chatgpt.Resource+"/models" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Fatal("incorrect catalog request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"listed","visibility":"list","input_modalities":["text","image"],"context_window":100000},{"slug":"hidden","visibility":"hidden"}]}`))}, nil
	})}
	rows, err := fetchChatGPTModels(context.Background(), client, core.NewSecret("fixture"))
	need(t, err)
	if len(rows) != 1 || rows[0].Name != "listed" || !rows[0].SupportsImages || rows[0].SupportsAudio || rows[0].ProviderContextLimit != 100000 {
		t.Fatal("catalog fields incorrect")
	}
}
