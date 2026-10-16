package slack

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeHTTP(fn roundTrip) HTTPClient {
	return HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: fn, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}}
}
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestWebAPIOutcomesAndPlainText(t *testing.T) {
	for _, tc := range []struct {
		body, want string
		status     int
	}{{`{"ok":true,"ts":"123.456"}`, "", 200}, {`{"ok":false,"error":"invalid_auth"}`, "login_required", 200}, {`{"ok":false,"error":"missing_scope"}`, "missing_scope", 200}, {`null`, "unknown", 200}, {`{}`, "unknown", 200}, {`{"ok":true}`, "unknown", 200}, {`not-json`, "unknown", 200}, {`unavailable`, "unknown", 503}, {`limited`, "rate_limited", 429}} {
		t.Run(tc.body, func(t *testing.T) {
			h := fakeHTTP(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://slack.com/api/chat.postMessage" || r.Header.Get("Authorization") != "Bearer private-token" {
					t.Fatal("wrong target or token")
				}
				_ = r.ParseForm()
				if r.Form.Get("mrkdwn") != "false" || r.Form.Get("parse") != "none" || r.Form.Get("channel") != "DDM" {
					t.Fatal(r.Form)
				}
				out := response(tc.status, tc.body)
				out.Header.Set("Retry-After", "125")
				return out, nil
			})
			_, err := h.Send(t.Context(), "private-token", "DDM", "<@UUSER> & *text*")
			if tc.want == "" {
				must(t, err)
			} else if err == nil || failure(err) != tc.want {
				t.Fatal(err)
			}
			if tc.status == 429 && err.(*apiError).RetryAfter != 125*time.Second {
				t.Fatal(err)
			}
		})
	}
}
func TestMediaOriginAndUploadSequence(t *testing.T) {
	calls := []string{}
	h := fakeHTTP(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/files.getUploadURLExternal":
			return response(200, `{"ok":true,"file_id":"FFILE","upload_url":"https://files.slack.com/upload/test"}`), nil
		case "/upload/test":
			if r.Header.Get("Authorization") != "" {
				t.Fatal("bot token leaked to upload URL")
			}
			return response(200, "OK"), nil
		case "/api/files.completeUploadExternal":
			_ = r.ParseForm()
			if r.Form.Get("channel_id") != "DDM" || !strings.Contains(r.Form.Get("files"), "FFILE") {
				t.Fatal(r.Form)
			}
			return response(200, `{"ok":true}`), nil
		default:
			t.Fatal(r.URL)
			return nil, nil
		}
	})
	_, err := h.Upload(t.Context(), "private-token", "DDM", Media{Name: "photo.png", Data: []byte("photo")})
	must(t, err)
	if len(calls) != 3 {
		t.Fatal(calls)
	}
	for _, raw := range []string{"https://evil.example/file", "https://files.slack.com.evil.example/file", "http://files.slack.com/file", "https://files.slack.com:444/file", "https://user@files.slack.com/file", "https://slack.com/file"} {
		_, err = h.Download(t.Context(), "private-token", File{URL: raw}, 100)
		if err == nil {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	if len(calls) != 3 {
		t.Fatal("unsafe download performed network request")
	}
}
func TestIdentityChecksAndDownloadLimit(t *testing.T) {
	h := fakeHTTP(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "auth.test") {
			return response(200, `{"ok":true,"team_id":"TTEAM","bot_id":"BBOT","user_id":"UBOT"}`), nil
		}
		return response(200, `{"ok":true,"bot":{"app_id":"AAPP","user_id":"UOTHER"}}`), nil
	})
	if _, err := h.Identity(context.Background(), "token"); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	h = fakeHTTP(func(*http.Request) (*http.Response, error) { return response(200, "too many bytes"), nil })
	if _, err := h.Download(t.Context(), "token", File{URL: "https://files.slack.com/file"}, 2); err == nil {
		t.Fatal("download size ignored")
	}
}
