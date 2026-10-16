package feishu

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func transportClient(fn func(*http.Request) (int, string)) HTTPClient {
	return HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			status, body := fn(r)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
	}}
}
func TestRegistrationRequestsOnlyPrivateChatScopes(t *testing.T) {
	h := transportClient(func(r *http.Request) (int, string) {
		if r.URL.Host != "accounts.feishu.cn" {
			t.Fatal(r.URL)
		}
		must(t, r.ParseForm())
		if r.Form.Get("action") != "begin" || r.Form.Get("request_user_info") != "open_id" {
			t.Fatal(r.Form)
		}
		return 200, `{"device_code":"fixture-device","verification_uri_complete":"https://open.feishu.cn/authorize?code=fixture","expires_in":240,"interval":5}`
	})
	reg, err := h.Begin(t.Context())
	must(t, err)
	if reg.ExpiresIn != 240 {
		t.Fatal("platform expiry not respected", reg.ExpiresIn)
	}
	u, err := url.Parse(reg.URL)
	must(t, err)
	compressed, err := base64.RawURLEncoding.DecodeString(u.Query().Get("addons"))
	must(t, err)
	z, err := gzip.NewReader(bytes.NewReader(compressed))
	must(t, err)
	defer z.Close()
	data, err := io.ReadAll(z)
	must(t, err)
	var addons struct {
		Preset bool
		Scopes struct{ Tenant []string }
		Events struct{ Items struct{ Tenant []string } }
	}
	must(t, json.Unmarshal(data, &addons))
	if addons.Preset || len(addons.Scopes.Tenant) != 2 || len(addons.Events.Items.Tenant) != 1 || addons.Events.Items.Tenant[0] != "im.message.receive_v1" || strings.Contains(string(data), "group") || u.Query().Get("createOnly") != "true" {
		t.Fatal(string(data))
	}
}
func TestHTTPMessageEnvelopeUUIDAndErrors(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
		reason string
	}{{`{"code":0,"data":{"message_id":"om_result"}}`, 200, ""}, {`{"code":230020}`, 200, "rate_limited"}, {`{"code":230020}`, 400, "rate_limited"}, {`{"code":230027}`, 400, "rejected"}, {`null`, 200, "unknown"}, {`{}`, 200, "unknown"}, {`{"code":0}`, 200, "unknown"}, {`{}`, 503, "unknown"}, {`{}`, 429, "rate_limited"}} {
		t.Run(test.reason+test.body, func(t *testing.T) {
			h := transportClient(func(r *http.Request) (int, string) {
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					return 200, `{"code":0,"tenant_access_token":"fixture-token"}`
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.Query().Get("receive_id_type") != "open_id" {
					t.Fatal(r.Header)
				}
				var body map[string]string
				must(t, json.NewDecoder(r.Body).Decode(&body))
				if body["uuid"] != "stable-id" || body["receive_id"] != "ou_user" || body["msg_type"] != "text" || body["content"] != `{"text":"hello\nworld"}` {
					t.Fatal(body)
				}
				return test.status, test.body
			})
			id, err := h.Send(context.Background(), Credentials{"cli_test", "secret", "ou_user"}, "stable-id", "hello\nworld")
			if test.reason == "" {
				must(t, err)
				if id != "om_result" {
					t.Fatal(id)
				}
			} else if err == nil || failure(err) != test.reason {
				t.Fatal(err)
			}
		})
	}
}
func TestRegistrationRejectsForeignURLAndLarkTenant(t *testing.T) {
	h := transportClient(func(*http.Request) (int, string) {
		return 200, `{"device_code":"fixture","verification_uri_complete":"https://example.com/authorize"}`
	})
	if _, err := h.Begin(t.Context()); err == nil {
		t.Fatal("foreign authorization URL")
	}
	h = transportClient(func(*http.Request) (int, string) {
		return 200, `{"client_id":"cli_test","client_secret":"secret","user_info":{"open_id":"ou_test","tenant_brand":"lark"}}`
	})
	r, err := h.Poll(t.Context(), "device")
	must(t, err)
	if r.Status != "unsupported_tenant" || r.AppSecret != "" {
		t.Fatal(r.Status)
	}
}
