package dingtalk

import (
	"context"
	"encoding/json"
	"golang.org/x/net/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestSendUsesApplicationTokenAndRecipientResult(t *testing.T) {
	h := NewClient()
	var tokenCalls int
	mode := "ok"
	h.client = func(string) *http.Client {
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			var body map[string]any
			must(t, json.NewDecoder(r.Body).Decode(&body))
			if r.URL.Path == "/v1.0/oauth2/accessToken" {
				tokenCalls++
				if body["appKey"] != "app" || body["appSecret"] != "secret" {
					t.Fatal(body)
				}
				return response(200, `{"accessToken":"access","expireIn":7200}`), nil
			}
			if r.Header.Get("x-acs-dingtalk-access-token") != "access" || r.URL.Path != "/v1.0/robot/oToMessages/batchSend" || body["robotCode"] != "app" || body["msgKey"] != "sampleMarkdown" || body["msgParam"] != `{"text":"hello","title":"Molr"}` {
				t.Fatal("incorrect outbound contract")
			}
			switch mode {
			case "limited":
				return response(200, `{"processQueryKey":"id","flowControlledStaffIdList":["staff"]}`), nil
			case "invalid":
				return response(200, `{"processQueryKey":"id","invalidStaffIdList":["staff"]}`), nil
			case "empty":
				return response(200, `{}`), nil
			case "null":
				return response(200, `null`), nil
			case "error":
				return response(500, `{"code":"InternalError"}`), nil
			}
			return response(200, `{"processQueryKey":"id"}`), nil
		})}
	}
	for _, tc := range []struct{ mode, want string }{{"ok", ""}, {"limited", "rate_limited"}, {"invalid", "rejected"}, {"empty", "unknown"}, {"null", "unknown"}, {"error", "unknown"}} {
		mode = tc.mode
		err := h.Send(t.Context(), Credentials{"app", "secret"}, "staff", "sampleMarkdown", map[string]any{"title": "Molr", "text": "hello"})
		got := ""
		if err != nil {
			got = failure(err)
		}
		if got != tc.want {
			t.Fatalf("%s: %s", mode, got)
		}
	}
	if tokenCalls != 1 {
		t.Fatal("token was not cached")
	}
}
func TestUntrustedURLsNeverReceiveCredentials(t *testing.T) {
	for _, endpoint := range []string{"ws://api.dingtalk.com/connect", "wss://evil.example/connect", "wss://dingtalk.com.evil.example/connect", "wss://user@api.dingtalk.com/connect", "wss://api.dingtalk.com:444/connect"} {
		if _, err := streamURL(endpoint, "ticket"); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, target := range []string{"http://down.dingtalk.com/file", "https://evil.example/file", "https://user@down.dingtalk.com/file", "https://down.dingtalk.com:444/file"} {
		h := NewClient()
		var calls int
		h.client = func(origin string) *http.Client {
			if origin != apiOrigin {
				t.Fatal("attempted unapproved download")
			}
			return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.Contains(r.URL.Path, "accessToken") {
					return response(200, `{"accessToken":"access","expireIn":7200}`), nil
				}
				raw, _ := json.Marshal(map[string]string{"downloadUrl": target})
				return response(200, string(raw)), nil
			})}
		}
		if _, _, err := h.Download(t.Context(), Credentials{"app", "secret"}, "code", 1024); err == nil {
			t.Fatal("untrusted download accepted")
		}
		if calls != 2 {
			t.Fatal(calls)
		}
	}
}
func TestStreamHeartbeatDoesNotWaitForMessageProcessing(t *testing.T) {
	received := make(chan struct{})
	release := make(chan struct{})
	pingACK := make(chan struct{})
	var callbacks atomic.Int32
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		_ = websocket.Message.Send(ws, `{"type":"CALLBACK","headers":{"topic":"/v1.0/im/bot/messages/get","messageId":"callback","time":123},"data":"{\"msgId\":\"message\"}"}`)
		<-received
		_ = websocket.Message.Send(ws, `{"type":"SYSTEM","headers":{"topic":"ping","messageId":"heartbeat"},"data":"{\"opaque\":\"ping-token\"}"}`)
		var ack struct {
			Code    int               `json:"code"`
			Headers map[string]string `json:"headers"`
			Data    string            `json:"data"`
		}
		if websocket.JSON.Receive(ws, &ack) == nil && ack.Code == 200 && ack.Headers["messageId"] == "heartbeat" && strings.Contains(ack.Data, "ping-token") {
			close(pingACK)
		}
		<-release
	}))
	defer server.Close()
	h := NewClient()
	h.client = func(string) *http.Client {
		return &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return response(200, `{"endpoint":"wss://test.dingtalk.com/connect","ticket":"test-ticket"}`), nil
		})}
	}
	h.dial = func(ctx context.Context, _ string) (*websocket.Conn, error) {
		return websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- h.Stream(ctx, Credentials{"app", "secret"}, func() error { return nil }, func(ctx context.Context, m Incoming) error {
			callbacks.Add(1)
			close(received)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case <-pingACK:
	case <-time.After(3 * time.Second):
		t.Error("heartbeat blocked by message callback")
	}
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not stop")
	}
	if callbacks.Load() != 1 {
		t.Fatal("callback not decoded")
	}
}
func TestMediaParsing(t *testing.T) {
	m := incoming("rich", "")
	m.MsgType = "richText"
	m.Content = json.RawMessage(`{"richText":[{"text":"look"},{"type":"picture","downloadCode":"image-code"}]}`)
	text, refs := parseIncoming(m)
	if text != "look" || len(refs) != 1 || refs[0].Code != "image-code" {
		t.Fatal(text, refs)
	}
	m.MsgType = "audio"
	m.Content = json.RawMessage(`{"recognition":"transcript","downloadCode":"voice"}`)
	text, refs = parseIncoming(m)
	if text != "transcript" || len(refs) != 0 {
		t.Fatal("transcription not preferred")
	}
}

func TestUploadMultipartAndRejectedReceipt(t *testing.T) {
	for _, receipt := range []string{`{"errcode":0,"media_id":"@fixture"}`, `{"errcode":400,"errmsg":"rejected"}`, `{}`, `null`} {
		h := NewClient()
		h.client = func(origin string) *http.Client {
			return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if origin == apiOrigin {
					return response(200, `{"accessToken":"fixture-access","expireIn":7200}`), nil
				}
				if origin != "https://oapi.dingtalk.com" || r.URL.Path != "/media/upload" || r.URL.Query().Get("type") != "file" || r.URL.Query().Get("access_token") != "fixture-access" {
					t.Fatal("wrong upload request")
				}
				must(t, r.ParseMultipartForm(1024))
				defer r.MultipartForm.RemoveAll()
				file, header, err := r.FormFile("media")
				must(t, err)
				defer file.Close()
				data, err := io.ReadAll(file)
				must(t, err)
				if header.Filename != "fixture.txt" || string(data) != "contents" {
					t.Fatal("wrong attachment")
				}
				return response(200, receipt), nil
			})}
		}
		id, err := h.Upload(t.Context(), Credentials{"app", "secret"}, "fixture.txt", []byte("contents"))
		if strings.Contains(receipt, "@fixture") {
			must(t, err)
			if id != "@fixture" {
				t.Fatal(id)
			}
		} else if err == nil || id != "" {
			t.Fatal("invalid upload accepted")
		}
	}
}
