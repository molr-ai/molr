package qq

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(raw string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}
}
func TestQRProtocolDecryptsAndKeepsKeyPrivate(t *testing.T) {
	var key []byte
	var tamper bool
	h := HTTPClient{client: func(origin string) *http.Client {
		return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			if origin != registrationOrigin || req.Header.Get("Authorization") != "" {
				t.Fatal("credential crossed origin")
			}
			var body map[string]string
			must(t, json.NewDecoder(req.Body).Decode(&body))
			switch req.URL.Path {
			case "/lite/create_bind_task":
				var err error
				key, err = base64.StdEncoding.DecodeString(body["key"])
				must(t, err)
				if len(key) != 32 {
					t.Fatal("weak key")
				}
				return response(`{"retcode":0,"data":{"task_id":"test-task"}}`, 200), nil
			case "/lite/poll_bind_result":
				if body["task_id"] != "test-task" || len(body) != 1 {
					t.Fatal(body)
				}
				block, err := aes.NewCipher(key)
				must(t, err)
				gcm, err := cipher.NewGCM(block)
				must(t, err)
				nonce := bytes.Repeat([]byte{1}, 12)
				sealed := gcm.Seal(nonce, nonce, []byte("fixture-secret"), nil)
				if tamper {
					sealed[len(sealed)-1] ^= 1
				}
				raw, _ := json.Marshal(map[string]any{"retcode": 0, "data": map[string]any{"status": 2, "bot_appid": 123456, "user_openid": "user", "bot_encrypt_secret": base64.StdEncoding.EncodeToString(sealed)}})
				return response(string(raw), 200), nil
			default:
				t.Fatal(req.URL.Path)
				return nil, nil
			}
		})}
	}}
	r, err := h.Begin(t.Context())
	must(t, err)
	if strings.Contains(r.URL, base64.StdEncoding.EncodeToString(key)) || !strings.Contains(r.URL, "task_id=test-task") || !strings.Contains(r.URL, "source=molr") {
		t.Fatal("invalid public QR URL")
	}
	got, err := h.Poll(t.Context(), r.DeviceCode)
	must(t, err)
	if got.AppID != "123456" || got.AppSecret != "fixture-secret" || got.OpenID != "user" || got.Status != "confirmed" {
		t.Fatal("credential decoding failed")
	}
	tamper = true
	if _, err = h.Poll(t.Context(), r.DeviceCode); failure(err) != "rejected" {
		t.Fatal("accepted tampered credential")
	}
}
func TestSendProtocolAndErrors(t *testing.T) {
	for _, mode := range []string{"passive", "proactive", "unknown", "limit", "expired"} {
		t.Run(mode, func(t *testing.T) {
			h := HTTPClient{client: func(origin string) *http.Client {
				return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
					if origin == tokenOrigin {
						if req.Header.Get("Authorization") != "" {
							t.Fatal("unexpected token header")
						}
						return response(`{"access_token":"test-token","expires_in":7200}`, 200), nil
					}
					if origin != apiOrigin || req.Header.Get("Authorization") != "QQBot test-token" || req.URL.Path != "/v2/users/user/messages" {
						t.Fatal("wrong send destination")
					}
					var body map[string]any
					must(t, json.NewDecoder(req.Body).Decode(&body))
					if body["content"] != "hello" {
						t.Fatal(body)
					}
					if mode == "proactive" {
						if _, ok := body["msg_id"]; ok {
							t.Fatal("proactive contains reply ID")
						}
					} else if body["msg_id"] != "inbound" || body["msg_seq"] != float64(2) {
						t.Fatal(body)
					}
					switch mode {
					case "unknown":
						return response(`private-server-error test-token`, 502), nil
					case "limit":
						r := response(`{}`, 429)
						r.Header.Set("Retry-After", "180")
						return r, nil
					case "expired":
						return response(`{"code":40034005,"message":"private"}`, 400), nil
					}
					return response(`{"id":"sent"}`, 200), nil
				})}
			}}
			id := "inbound"
			if mode == "proactive" {
				id = ""
			}
			_, err := h.Send(context.Background(), Credentials{"123456", "secret", "user"}, id, 2, "hello")
			switch mode {
			case "unknown":
				if failure(err) != "unknown" {
					t.Fatal(err)
				}
			case "limit":
				if failure(err) != "rate_limited" {
					t.Fatal(err)
				}
			case "expired":
				if failure(err) != "reply_expired" {
					t.Fatal(err)
				}
			default:
				must(t, err)
			}
			if err != nil && (strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "test-token")) {
				t.Fatal("sensitive error leaked")
			}
		})
	}
}
func TestGatewayAllowlist(t *testing.T) {
	for _, raw := range []string{"wss://api.bot.qq.com/websocket", "wss://api.sgroup.qq.com/websocket"} {
		if !gatewayURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"ws://api.bot.qq.com/", "wss://api.bot.qq.com.evil.example/", "wss://user@api.bot.qq.com/", "wss://127.0.0.1/", "wss://api.bot.qq.com:8443/"} {
		if gatewayURL(raw) {
			t.Fatal(raw)
		}
	}
}

func TestGatewayResumeAdvancesOnlyCommittedMessages(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
		var auth struct {
			Op   int            `json:"op"`
			Data map[string]any `json:"d"`
		}
		if conn.ReadJSON(&auth) != nil {
			return
		}
		attempts++
		if attempts == 1 {
			if auth.Op != 2 || auth.Data["token"] != "QQBot test-token" || auth.Data["intents"] != float64(1<<25) {
				t.Error("bad identify")
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "session"}})
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "GROUP_AT_MESSAGE_CREATE", "s": 2, "d": map[string]string{"content": "ignore group"}})
		} else {
			if auth.Op != 6 || auth.Data["session_id"] != "session" || auth.Data["seq"] != float64(2) {
				t.Error("resume advanced past failed receive")
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "RESUMED", "s": 2, "d": map[string]any{}})
		}
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 3, "d": map[string]any{"id": "message", "content": "hello", "author": map[string]string{"user_openid": "user"}}})
		_ = conn.WriteJSON(map[string]any{"op": 7, "d": nil})
	}))
	defer server.Close()
	h := HTTPClient{client: func(origin string) *http.Client {
		return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			if origin == tokenOrigin {
				return response(`{"access_token":"test-token"}`, 200), nil
			}
			if req.Header.Get("Authorization") != "QQBot test-token" {
				t.Error("gateway lacks auth")
			}
			return response(`{"url":"wss://api.bot.qq.com/websocket"}`, 200), nil
		})}
	}, dial: func(ctx context.Context, _ string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	}}
	var session string
	var seq int64
	received := 0
	recv := func(m Incoming) error {
		received++
		if m.AppID != "123456" || m.OpenID != "user" || m.Content != "hello" {
			t.Fatal(m)
		}
		if received == 1 {
			return errors.New("transaction rolled back")
		}
		return nil
	}
	c := Credentials{"123456", "secret", "user"}
	_ = h.listenSession(t.Context(), c, &session, &seq, recv, func(bool) {})
	if seq != 2 || received != 1 {
		t.Fatal(seq, received)
	}
	_ = h.listenSession(t.Context(), c, &session, &seq, recv, func(bool) {})
	if seq != 3 || received != 2 {
		t.Fatal(seq, received)
	}
}
