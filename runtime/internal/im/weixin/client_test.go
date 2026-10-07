package weixin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPProtocolRequestsAndErrors(t *testing.T) {
	response := `{"qrcode":"code","qrcode_img_content":"qr-content"}`
	status := 200
	var last *http.Request
	var body map[string]any
	h := HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			last = r
			body = nil
			if r.Body != nil {
				raw, _ := io.ReadAll(r.Body)
				if len(raw) > 0 {
					must(t, json.Unmarshal(raw, &body))
				}
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
		})}
	}}
	q, err := h.QR(t.Context(), []string{})
	must(t, err)
	if q.Code != "code" || last.URL.Query().Get("bot_type") != "3" || last.Header.Get("Authorization") != "" || last.Header.Get("X-WECHAT-UIN") == "" {
		t.Fatal("QR request")
	}
	response = `{"status":"confirmed","bot_token":"secret","ilink_bot_id":"bot","ilink_user_id":"user","baseurl":"https://ilinkai.weixin.qq.com"}`
	l, err := h.Login(t.Context(), baseURL, "code", "123456")
	must(t, err)
	if l.Token != "secret" || last.Method != "GET" || last.URL.Query().Get("verify_code") != "123456" || last.Header.Get("AuthorizationType") != "" {
		t.Fatal("login request")
	}
	response = `{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":18446744073709551615,"message_type":1,"from_user_id":"user"}]}`
	updates, err := h.Poll(t.Context(), baseURL, "secret", "previous")
	must(t, err)
	if updates.Cursor != "next" || messageID(updates.Messages[0].ID) != "18446744073709551615" || body["get_updates_buf"] != "previous" || last.Header.Get("Authorization") != "Bearer secret" {
		t.Fatal("poll request")
	}
	response = `{"ret":0}`
	must(t, sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello")))
	message := body["msg"].(map[string]any)
	if message["context_token"] != "context" || message["client_id"] != "stable-id" || message["to_user_id"] != "user" {
		t.Fatal(message)
	}
	response = `{"ret":-2}`
	if failure(sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello"))) != "context_required" {
		t.Fatal("context failure")
	}
	for _, body := range []string{`{}`, `{"message_id":18446744073709551615}`, `{"ret":0,"errcode":0}`} {
		response = body
		must(t, sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello")))
	}
	for _, body := range []string{`null`, ``, `{`, `[]`, `{"ret":"0"}`} {
		response = body
		if failure(sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello"))) != "unknown" {
			t.Fatalf("invalid acknowledgement accepted: %s", body)
		}
	}
	response = `{"errcode":-14}`
	if failure(sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello"))) != "login_required" {
		t.Fatal("explicit error ignored")
	}
	response = `{"ret":-14}`
	_, err = h.Poll(t.Context(), baseURL, "secret", "")
	if failure(err) != "login_required" {
		t.Fatal(err)
	}
	status = 503
	if failure(sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello"))) != "unknown" {
		t.Fatal("5xx retried")
	}
	status = 429
	if failure(sendError(h.Send(t.Context(), baseURL, "secret", "user", "context", "stable-id", "hello"))) != "rate_limited" {
		t.Fatal("rate limit")
	}
	last = nil
	_, err = h.Login(context.Background(), "https://evil.example", "code", "")
	if err == nil || last != nil {
		t.Fatal("untrusted origin reached transport")
	}
}

func TestExistingCredentialCheckAndLosslessSendReceipt(t *testing.T) {
	response := `{"ret":0}`
	h := HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/ilink/bot/getupdates" {
				t.Fatal("credential check consumed messages")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
		})}
	}}
	must(t, h.Check(t.Context(), baseURL, "token", "user"))
	for _, body := range []string{`{}`, `{"ret":-14}`, `{"ret":0,"errcode":-14}`} {
		response = body
		if err := h.Check(t.Context(), baseURL, "token", "user"); err == nil {
			t.Fatal("unverified credential accepted")
		}
	}
	response = `{"message_id":18446744073709551615}`
	id, err := h.Send(t.Context(), baseURL, "token", "user", "context", "client-id", "hello")
	must(t, err)
	if id != "18446744073709551615" {
		t.Fatal("server receipt lost precision", id)
	}
}
