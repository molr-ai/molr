package telegram

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestHTTPProtocolAndSecretRedaction(t *testing.T) {
	for _, tc := range []struct {
		body   string
		code   int
		reason string
	}{{`{"ok":true,"result":{"message_id":7}}`, 200, ""}, {`{"ok":true,"result":null}`, 200, "unknown"}, {`null`, 200, "unknown"}, {`bad`, 502, "unknown"}, {`{"ok":false,"error_code":401,"description":"secret"}`, 401, "token_invalid"}, {`{"ok":false,"error_code":429,"parameters":{"retry_after":17}}`, 429, "rate_limited"}, {`{"ok":false,"error_code":409}`, 409, "poll_conflict"}} {
		t.Run(tc.reason+tc.body, func(t *testing.T) {
			client := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.telegram.org" || r.URL.Scheme != "https" || r.URL.Path != "/bot"+testToken+"/sendMessage" {
					t.Fatal(r.URL.Host)
				}
				var input map[string]any
				must(t, json.NewDecoder(r.Body).Decode(&input))
				if input["text"] != "hello" || input["parse_mode"] != nil {
					t.Fatal(input)
				}
				return response(tc.code, tc.body), nil
			})}}
			id, err := client.Send(t.Context(), testToken, 123, "hello")
			if tc.reason == "" {
				must(t, err)
				if id != 7 {
					t.Fatal(id)
				}
			} else {
				if failure(err) != tc.reason {
					t.Fatal(err)
				}
				if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), testToken) {
					t.Fatal("leaked token")
				}
				if tc.reason == "rate_limited" {
					var e *apiError
					if !errors.As(err, &e) || e.RetryAfter != 17 {
						t.Fatal(err)
					}
				}
			}
		})
	}
	h := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New(r.URL.String()) })}}
	_, err := h.Send(t.Context(), testToken, 1, "hello")
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("URL leaked")
	}
}
func TestPollingAndMediaWireContract(t *testing.T) {
	h := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			var in struct {
				Offset  int64    `json:"offset"`
				Timeout int      `json:"timeout"`
				Allowed []string `json:"allowed_updates"`
			}
			must(t, json.NewDecoder(r.Body).Decode(&in))
			if in.Offset != 101 || in.Timeout != 20 || len(in.Allowed) != 1 || in.Allowed[0] != "message" {
				t.Fatal(in)
			}
			return response(200, `{"ok":true,"result":[{"update_id":101,"message":{"message_id":1,"from":{"id":9007199254740000},"chat":{"id":9007199254740000,"type":"private"},"text":"hello"}}]}`), nil
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			return response(200, `{"ok":true,"result":{"file_path":"documents/file.txt","file_size":5}}`), nil
		case strings.Contains(r.URL.Path, "/file/bot"):
			if r.Header.Get("Authorization") != "" {
				t.Fatal("extra credential")
			}
			return response(200, "hello"), nil
		case strings.HasSuffix(r.URL.Path, "/sendPhoto"), strings.HasSuffix(r.URL.Path, "/sendDocument"):
			must(t, r.ParseMultipartForm(1<<20))
			field := "document"
			if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
				field = "photo"
			}
			f, _, err := r.FormFile(field)
			must(t, err)
			defer f.Close()
			data, err := io.ReadAll(f)
			must(t, err)
			if string(data) != "hello" || r.FormValue("chat_id") != "99" {
				t.Fatal("multipart")
			}
			return response(200, `{"ok":true,"result":{"message_id":8}}`), nil
		}
		t.Fatal(r.URL.Path)
		return nil, nil
	})}}
	updates, err := h.Poll(t.Context(), testToken, 101, 20)
	must(t, err)
	if updates[0].Message.From.ID != 9007199254740000 {
		t.Fatal("ID truncated")
	}
	file, err := h.Download(t.Context(), testToken, Media{ID: "file", Name: "test.txt"}, 10)
	must(t, err)
	if string(file.Data) != "hello" {
		t.Fatal(file)
	}
	_, err = h.SendMedia(t.Context(), testToken, 99, file)
	must(t, err)
	file.Type = "image/png"
	_, err = h.SendMedia(t.Context(), testToken, 99, file)
	must(t, err)
	if _, err = h.Download(t.Context(), testToken, Media{Size: 11}, 10); err == nil {
		t.Fatal("oversized media")
	}
}
func TestClientRejectsRedirectAndUnsafePath(t *testing.T) {
	for _, value := range []string{"../secret", "/absolute", "https://evil.example/file", "document?secret", "document#secret"} {
		t.Run(value, func(t *testing.T) {
			calls := 0
			h := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"file_path": value}})
				return response(200, string(raw)), nil
			})}}
			_, err := h.Download(t.Context(), testToken, Media{ID: "file"}, 100)
			if err == nil || calls != 1 {
				t.Fatal("unsafe path accepted", calls, err)
			}
		})
	}
	h, err := NewHTTPClient("")
	must(t, err)
	if h.client.Timeout < 25*time.Second {
		t.Fatal("poll timeout")
	}
	if h.client.CheckRedirect == nil {
		t.Fatal("redirect not blocked")
	}
	for _, proxy := range []string{"file:///tmp/proxy", "http://user:pass@proxy.example", "http://proxy.example/path", "https://proxy.example?token=x"} {
		if _, err := NewHTTPClient(proxy); err == nil {
			t.Fatal("invalid proxy", proxy)
		}
	}
	h, err = NewHTTPClient("http://127.0.0.1:8080")
	must(t, err)
	if h.client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("proxy redirect")
	}
}

func TestBotIdentityAndWebhookWireContract(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"id":42,"is_bot":true,"username":"fixture_bot"}`, true},
		{`{"id":42,"is_bot":false,"username":"fixture_user"}`, false},
		{`{"id":0,"is_bot":true,"username":"fixture_bot"}`, false},
		{`{"id":42,"is_bot":true}`, false},
	} {
		h := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/bot"+testToken+"/getMe" {
				t.Fatal("wrong identity endpoint")
			}
			return response(200, `{"ok":true,"result":`+tc.body+`}`), nil
		})}}
		user, err := h.Me(t.Context(), testToken)
		if tc.valid {
			must(t, err)
			if user.ID != 42 {
				t.Fatal(user)
			}
		} else if failure(err) != "token_invalid" {
			t.Fatal(err)
		}
	}
	for _, url := range []string{"", "https://hooks.example/telegram"} {
		h := &HTTPClient{client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/bot"+testToken+"/getWebhookInfo" {
				t.Fatal("wrong webhook endpoint")
			}
			return response(200, `{"ok":true,"result":{"url":"`+url+`"}}`), nil
		})}}
		set, err := h.Webhook(t.Context(), testToken)
		must(t, err)
		if set != (url != "") {
			t.Fatal("wrong webhook state")
		}
	}
}
