// Package qq implements the official QQ bot C2C chat channel.
package qq

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/retry"
)

const apiOrigin = "https://api.bot.qq.com"
const registrationOrigin = "https://q.qq.com"
const tokenOrigin = "https://bots.qq.com"

type Credentials struct{ AppID, AppSecret, OpenID string }
type Registration struct {
	DeviceCode, URL     string
	ExpiresIn, Interval int
}
type RegistrationResult struct {
	Credentials
	Status string
}
type Incoming struct {
	AppID, OpenID, MessageID, Content string
	Attachments                       bool
}
type Client interface {
	Begin(context.Context) (Registration, error)
	Poll(context.Context, string) (RegistrationResult, error)
	Check(context.Context, Credentials) error
	Listen(context.Context, Credentials, func(Incoming) error, func(bool)) error
	Send(context.Context, Credentials, string, int, string) (string, error)
}
type apiError struct {
	reason     string
	retryAfter time.Duration
}

func (e *apiError) Error() string { return "qq: " + e.reason }
func failure(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.reason
	}
	return "unknown"
}

// Errors never contain remote response bodies, credentials, or signed URLs.
type HTTPClient struct {
	client func(string) *http.Client
	dial   func(context.Context, string) (*websocket.Conn, *http.Response, error)
}

func (h HTTPClient) http(origin string) *http.Client {
	if h.client != nil {
		return h.client(origin)
	}
	c := platform.PublicHTTPClient(origin)
	c.Timeout = 15 * time.Second
	return c
}
func (h HTTPClient) request(ctx context.Context, method, origin, path, token string, data any, out any) error {
	var body []byte
	var err error
	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(body))
	if err != nil {
		return &apiError{reason: "rejected"}
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "QQBot "+token)
	}
	resp, err := h.http(origin).Do(req)
	if err != nil {
		return &apiError{reason: "unknown"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return &apiError{reason: "unknown"}
	}
	if resp.StatusCode == 429 {
		return &apiError{reason: "rate_limited", retryAfter: responseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return &apiError{reason: "credentials_required"}
	}
	if resp.StatusCode >= 500 {
		return &apiError{reason: "unknown"}
	}
	var envelope struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if envelope.Code != 0 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reason := "rejected"
		switch envelope.Code {
		case 40034005, 304103:
			reason = "reply_expired"
		case 40034003:
			reason = "rate_limited"
		}
		return &apiError{reason: reason}
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, out) != nil {
		return &apiError{reason: "unknown"}
	}
	return nil
}

// QR protocol: @tencent-connect/qqbot-connector 1.2.0. Only the random task ID
// reaches the browser; its AES key and decrypted app secret stay in the Runtime.
func (h HTTPClient) Begin(ctx context.Context) (Registration, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return Registration{}, err
	}
	defer clear(key)
	var r struct {
		Retcode *int `json:"retcode"`
		Data    struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := h.request(ctx, http.MethodPost, registrationOrigin, "/lite/create_bind_task", "", map[string]string{"key": base64.StdEncoding.EncodeToString(key)}, &r); err != nil {
		return Registration{}, err
	}
	if r.Retcode == nil || *r.Retcode != 0 || r.Data.TaskID == "" || len(r.Data.TaskID) > 1024 {
		return Registration{}, &apiError{reason: "rejected"}
	}
	private, _ := json.Marshal(struct{ TaskID, Key string }{r.Data.TaskID, base64.StdEncoding.EncodeToString(key)})
	qr := registrationOrigin + "/qqbot/openclaw/connect.html?" + url.Values{"task_id": {r.Data.TaskID}, "source": {"molr"}, "_wv": {"2"}}.Encode()
	return Registration{string(private), qr, 300, 2}, nil
}
func (h HTTPClient) Poll(ctx context.Context, device string) (RegistrationResult, error) {
	var task struct{ TaskID, Key string }
	if json.Unmarshal([]byte(device), &task) != nil {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	var r struct {
		Retcode *int `json:"retcode"`
		Data    struct {
			Status int             `json:"status"`
			AppID  json.RawMessage `json:"bot_appid"`
			Secret string          `json:"bot_encrypt_secret"`
			OpenID string          `json:"user_openid"`
		} `json:"data"`
	}
	if err := h.request(ctx, http.MethodPost, registrationOrigin, "/lite/poll_bind_result", "", map[string]string{"task_id": task.TaskID}, &r); err != nil {
		return RegistrationResult{}, err
	}
	if r.Retcode == nil || *r.Retcode != 0 {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	switch r.Data.Status {
	case 0, 1:
		return RegistrationResult{Status: "waiting"}, nil
	case 3:
		return RegistrationResult{Status: "expired"}, nil
	case 2:
	default:
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	key, err := base64.StdEncoding.DecodeString(task.Key)
	if err != nil || len(key) != 32 {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	defer clear(key)
	raw, err := base64.StdEncoding.DecodeString(r.Data.Secret)
	if err != nil || len(raw) < 28 || len(raw) > 16384 {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return RegistrationResult{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return RegistrationResult{}, err
	}
	secret, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	defer clear(secret)
	appID := strings.Trim(string(r.Data.AppID), "\"")
	c := Credentials{appID, string(secret), r.Data.OpenID}
	if !validCredentials(c) {
		return RegistrationResult{}, &apiError{reason: "rejected"}
	}
	return RegistrationResult{c, "confirmed"}, nil
}
func (h HTTPClient) token(ctx context.Context, c Credentials) (string, error) {
	var r struct {
		Token   string          `json:"access_token"`
		Expires json.RawMessage `json:"expires_in"`
	}
	if err := h.request(ctx, http.MethodPost, tokenOrigin, "/app/getAppAccessToken", "", map[string]string{"appId": c.AppID, "clientSecret": c.AppSecret}, &r); err != nil {
		return "", err
	}
	if r.Token == "" || len(r.Token) > 16384 {
		return "", &apiError{reason: "credentials_required"}
	}
	return r.Token, nil
}
func (h HTTPClient) Check(ctx context.Context, c Credentials) error {
	_, err := h.token(ctx, c)
	return err
}
func (h HTTPClient) Send(ctx context.Context, c Credentials, replyID string, seq int, text string) (string, error) {
	token, err := h.token(ctx, c)
	if err != nil {
		return "", err
	}
	body := map[string]any{"msg_type": 0, "content": text}
	if replyID != "" {
		body["msg_id"] = replyID
		body["msg_seq"] = seq
	}
	var r struct {
		ID string `json:"id"`
	}
	if err = h.request(ctx, http.MethodPost, apiOrigin, "/v2/users/"+url.PathEscape(c.OpenID)+"/messages", token, body, &r); err != nil {
		return "", err
	}
	if r.ID == "" {
		return "", &apiError{reason: "unknown"}
	}
	return r.ID, nil
}
func responseRetryAfter(value string) time.Duration {
	if n, e := strconv.Atoi(value); e == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if at, e := http.ParseTime(value); e == nil && time.Until(at) > 0 {
		return time.Until(at)
	}
	return time.Minute
}

type gatewayEvent struct {
	Op   int             `json:"op"`
	Seq  *int64          `json:"s"`
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

func gatewayURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "wss" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && (u.Hostname() == "api.bot.qq.com" || u.Hostname() == "api.sgroup.qq.com")
}
func (h HTTPClient) Listen(ctx context.Context, c Credentials, receive func(Incoming) error, state func(bool)) error {
	var session string
	var seq int64
	failures := 0
	for ctx.Err() == nil {
		err := h.listenSession(ctx, c, &session, &seq, receive, state)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if failure(err) == "credentials_required" {
			return err
		}
		state(false)
		failures++
		if !sleep(ctx, (retry.Policy{}).Delay(failures)) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}
func (h HTTPClient) listenSession(ctx context.Context, c Credentials, session *string, seq *int64, receive func(Incoming) error, state func(bool)) error {
	token, err := h.token(ctx, c)
	if err != nil {
		return err
	}
	var gateway struct {
		URL string `json:"url"`
	}
	if err = h.request(ctx, http.MethodGet, apiOrigin, "/gateway", token, nil, &gateway); err != nil {
		return err
	}
	if !gatewayURL(gateway.URL) {
		return &apiError{reason: "rejected"}
	}
	u, _ := url.Parse(gateway.URL)
	transport := platform.PublicHTTPClient("https://" + u.Host).Transport.(*http.Transport)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: transport.DialContext}
	var conn *websocket.Conn
	var resp *http.Response
	if h.dial != nil {
		conn, resp, err = h.dial(ctx, gateway.URL)
	} else {
		conn, resp, err = dialer.DialContext(ctx, gateway.URL, nil)
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return &apiError{reason: "unknown"}
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	var mu sync.Mutex
	write := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	var hello gatewayEvent
	if err = conn.ReadJSON(&hello); err != nil || hello.Op != 10 {
		return &apiError{reason: "unknown"}
	}
	var heartbeat struct {
		Interval int `json:"heartbeat_interval"`
	}
	if json.Unmarshal(hello.Data, &heartbeat) != nil || heartbeat.Interval < 1000 || heartbeat.Interval > 300000 {
		return &apiError{reason: "rejected"}
	}
	interval := time.Duration(heartbeat.Interval) * time.Millisecond
	if *session != "" {
		err = write(map[string]any{"op": 6, "d": map[string]any{"token": "QQBot " + token, "session_id": *session, "seq": *seq}})
	} else {
		err = write(map[string]any{"op": 2, "d": map[string]any{"token": "QQBot " + token, "intents": 1 << 25, "shard": []int{0, 1}}})
	}
	if err != nil {
		return err
	}
	// Heartbeat sends are independent of durable inbound processing. Sequence is
	// advanced only after Receive commits, so RESUME can replay uncommitted events.
	var seqMu sync.Mutex
	beatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-ticker.C:
				seqMu.Lock()
				n := *seq
				seqMu.Unlock()
				if write(map[string]any{"op": 1, "d": n}) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(2*interval + 15*time.Second))
		var e gatewayEvent
		if err = conn.ReadJSON(&e); err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				switch closeErr.Code {
				case 4006, 4007, 4009:
					*session = ""
				case 4004, 4013, 4014, 4914, 4915:
					return &apiError{reason: "credentials_required"}
				}
			}
			return &apiError{reason: "unknown"}
		}
		switch e.Op {
		case 0:
			switch e.Type {
			case "READY":
				var ready struct {
					Session string `json:"session_id"`
				}
				if json.Unmarshal(e.Data, &ready) != nil || ready.Session == "" {
					return &apiError{reason: "unknown"}
				}
				*session = ready.Session
				state(true)
			case "RESUMED":
				state(true)
			case "C2C_MESSAGE_CREATE":
				var m struct {
					ID      string `json:"id"`
					Content string `json:"content"`
					Author  struct {
						OpenID string `json:"user_openid"`
					} `json:"author"`
					Attachments []json.RawMessage `json:"attachments"`
				}
				if json.Unmarshal(e.Data, &m) != nil {
					return &apiError{reason: "unknown"}
				}
				if err = receive(Incoming{c.AppID, m.Author.OpenID, m.ID, m.Content, len(m.Attachments) > 0}); err != nil {
					return err
				}
			}
			if e.Seq != nil {
				seqMu.Lock()
				*seq = *e.Seq
				seqMu.Unlock()
			}
		case 7:
			return &apiError{reason: "unknown"}
		case 9:
			*session = ""
			return &apiError{reason: "unknown"}
		case 1:
			seqMu.Lock()
			n := *seq
			seqMu.Unlock()
			if err = write(map[string]any{"op": 1, "d": n}); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
