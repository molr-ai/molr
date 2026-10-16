package dingtalk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
	"molr.ai/runtime/internal/platform"
)

const apiOrigin = "https://api.dingtalk.com"
const messageTopic = "/v1.0/im/bot/messages/get"

type Incoming struct {
	ConversationType string `json:"conversationType"`
	ConversationID   string `json:"conversationId"`
	RobotCode        string `json:"robotCode"`
	SenderStaffID    string `json:"senderStaffId"`
	SenderID         string `json:"senderId"`
	SenderCorpID     string `json:"senderCorpId"`
	ChatbotCorpID    string `json:"chatbotCorpId"`
	ChatbotUserID    string `json:"chatbotUserId"`
	MsgID            string `json:"msgId"`
	MsgType          string `json:"msgtype"`
	Text             struct {
		Content string `json:"content"`
	} `json:"text"`
	Content json.RawMessage `json:"content"`
}
type Credentials struct{ ClientID, Secret string }
type Client interface {
	Stream(context.Context, Credentials, func() error, func(context.Context, Incoming) error) error
	Send(context.Context, Credentials, string, string, map[string]any) error
	Download(context.Context, Credentials, string, int64) ([]byte, string, error)
	Upload(context.Context, Credentials, string, []byte) (string, error)
}
type apiError struct {
	Reason     string
	RetryAfter time.Duration
}

func (e *apiError) Error() string { return "DingTalk: " + e.Reason }
func failure(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Reason
	}
	return "unknown"
}

type token struct {
	value string
	until time.Time
}
type HTTPClient struct {
	mu     sync.Mutex
	tokens map[string]token
	client func(string) *http.Client
	dial   func(context.Context, string) (*websocket.Conn, error)
}

func NewClient() *HTTPClient { return &HTTPClient{tokens: map[string]token{}} }
func (h *HTTPClient) http(origin string) *http.Client {
	if h.client != nil {
		return h.client(origin)
	}
	c := platform.PublicHTTPClient(origin)
	c.Timeout = 45 * time.Second
	return c
}
func (h *HTTPClient) request(ctx context.Context, endpoint, access string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiOrigin+endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if access != "" {
		req.Header.Set("x-acs-dingtalk-access-token", access)
	}
	c := h.http(apiOrigin)
	defer c.CloseIdleConnections()
	resp, err := c.Do(req)
	if err != nil {
		return &apiError{Reason: "unknown"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return &apiError{Reason: "unknown"}
	}
	var envelope struct {
		Code    string `json:"code"`
		Errcode int    `json:"errcode"`
	}
	_ = json.Unmarshal(data, &envelope)
	reason := ""
	switch {
	case resp.StatusCode == 429:
		reason = "rate_limited"
	case resp.StatusCode >= 500:
		reason = "unknown"
	case resp.StatusCode == 401:
		reason = "credential_required"
	case resp.StatusCode >= 400:
		reason = "rejected"
	}
	code := strings.ToLower(envelope.Code)
	if strings.Contains(code, "invalidclient") || strings.Contains(code, "invalid.client") || strings.Contains(code, "invalidauthentication") || strings.Contains(code, "invalidtoken") || strings.Contains(code, "invalidcredential") || strings.Contains(code, "accesstoken.expired") {
		reason = "credential_required"
	}
	if strings.Contains(code, "throttl") || strings.Contains(code, "limit") && strings.Contains(code, "rate") {
		reason = "rate_limited"
	}
	if reason == "" && (envelope.Code != "" || envelope.Errcode != 0) {
		reason = "rejected"
	}
	if reason != "" {
		delay := time.Minute
		if seconds, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && seconds > 0 && seconds < 86400 {
			delay = max(delay, time.Duration(seconds)*time.Second)
		} else if at, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			delay = max(delay, time.Until(at))
		}
		return &apiError{Reason: reason, RetryAfter: delay}
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) || json.Unmarshal(data, out) != nil {
		return &apiError{Reason: "unknown"}
	}
	return nil
}
func (h *HTTPClient) access(ctx context.Context, c Credentials) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := hash(c.ClientID + "\x00" + c.Secret)
	if t, ok := h.tokens[key]; ok && time.Now().Before(t.until) {
		return t.value, nil
	}
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := h.request(ctx, "/v1.0/oauth2/accessToken", "", map[string]string{"appKey": c.ClientID, "appSecret": c.Secret}, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" || out.ExpireIn <= 0 {
		return "", &apiError{Reason: "credential_required"}
	}
	if h.tokens == nil {
		h.tokens = map[string]token{}
	}
	for k, t := range h.tokens {
		if time.Now().After(t.until) {
			delete(h.tokens, k)
		}
	}
	if len(h.tokens) > 128 {
		clear(h.tokens)
	}
	h.tokens[key] = token{out.AccessToken, time.Now().Add(time.Duration(max(1, out.ExpireIn-60)) * time.Second)}
	return out.AccessToken, nil
}
func (h *HTTPClient) Send(ctx context.Context, c Credentials, user, key string, param map[string]any) error {
	access, err := h.access(ctx, c)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(param)
	if err != nil {
		return err
	}
	var out struct {
		ProcessQueryKey string   `json:"processQueryKey"`
		Invalid         []string `json:"invalidStaffIdList"`
		Limited         []string `json:"flowControlledStaffIdList"`
	}
	err = h.request(ctx, "/v1.0/robot/oToMessages/batchSend", access, map[string]any{"robotCode": c.ClientID, "userIds": []string{user}, "msgKey": key, "msgParam": string(raw)}, &out)
	if err != nil {
		return err
	}
	if len(out.Invalid) > 0 {
		return &apiError{Reason: "rejected"}
	}
	if len(out.Limited) > 0 {
		return &apiError{Reason: "rate_limited", RetryAfter: time.Minute}
	}
	if out.ProcessQueryKey == "" {
		return &apiError{Reason: "unknown"}
	}
	return nil
}

// The SDK's default dialer does not use Molr's public-IP guard. The small Stream
// transport below uses the official protocol with the existing WebSocket package,
// bounded frames, no credential-bearing logs and cancellation of every socket.
func streamURL(endpoint, ticket string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "wss" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".dingtalk.com") || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || ticket == "" || len(ticket) > 8192 {
		return "", errors.New("invalid stream endpoint")
	}
	q := u.Query()
	q.Set("ticket", ticket)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func dialStream(ctx context.Context, raw string) (*websocket.Conn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	origin := "https://" + u.Host
	c := platform.PublicHTTPClient(origin)
	transport := c.Transport.(*http.Transport)
	conn, err := transport.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), "443"))
	if err != nil {
		return nil, err
	}
	secured := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	stop := context.AfterFunc(ctx, func() { secured.Close() })
	defer stop()
	_ = secured.SetDeadline(time.Now().Add(15 * time.Second))
	if err = secured.HandshakeContext(ctx); err != nil {
		secured.Close()
		return nil, err
	}
	cfg, err := websocket.NewConfig(raw, apiOrigin)
	if err != nil {
		secured.Close()
		return nil, err
	}
	ws, err := websocket.NewClient(cfg, secured)
	if err != nil {
		secured.Close()
		return nil, err
	}
	_ = ws.SetDeadline(time.Time{})
	ws.MaxPayloadBytes = 1 << 20
	return ws, nil
}

type frame struct {
	Type    string `json:"type"`
	Headers struct {
		MessageID string `json:"messageId"`
		Topic     string `json:"topic"`
	} `json:"headers"`
	Data string `json:"data"`
}

func (h *HTTPClient) Stream(ctx context.Context, c Credentials, ready func() error, receive func(context.Context, Incoming) error) error {
	var endpoint struct {
		Endpoint string `json:"endpoint"`
		Ticket   string `json:"ticket"`
	}
	if err := h.request(ctx, "/v1.0/gateway/connections/open", "", map[string]any{"clientId": c.ClientID, "clientSecret": c.Secret, "ua": "molr", "subscriptions": []map[string]string{{"type": "CALLBACK", "topic": messageTopic}}}, &endpoint); err != nil {
		return err
	}
	raw, err := streamURL(endpoint.Endpoint, endpoint.Ticket)
	if err != nil {
		return err
	}
	dial := dialStream
	if h.dial != nil {
		dial = h.dial
	}
	ws, err := dial(ctx, raw)
	if err != nil {
		return err
	}
	defer ws.Close()
	stop := context.AfterFunc(ctx, func() { ws.Close() })
	defer stop()
	if err = ready(); err != nil {
		return err
	}
	// A separate bounded queue keeps heartbeat handling independent of media I/O.
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan frame, 32)
	done := make(chan struct{})
	var writeMu sync.Mutex
	ack := func(f frame, code int, data string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return websocket.JSON.Send(ws, map[string]any{"code": code, "message": http.StatusText(code), "headers": map[string]string{"messageId": f.Headers.MessageID, "contentType": "application/json"}, "data": data})
	}
	go func() {
		defer close(done)
		for {
			select {
			case <-workCtx.Done():
				return
			case f := <-queue:
				var m Incoming
				code := 200
				if json.Unmarshal([]byte(f.Data), &m) != nil {
					code = 400
				} else if err := receive(workCtx, m); err != nil {
					code = 500
				}
				if ack(f, code, `{"response":null}`) != nil {
					ws.Close()
					return
				}
			}
		}
	}()
	defer func() { cancel(); ws.Close(); <-done }()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(150 * time.Second))
		var f frame
		if err = websocket.JSON.Receive(ws, &f); err != nil {
			return err
		}
		switch {
		case f.Type == "SYSTEM" && f.Headers.Topic == "ping":
			if err = ack(f, 200, f.Data); err != nil {
				return err
			}
		case f.Type == "SYSTEM" && f.Headers.Topic == "disconnect":
			return errors.New("stream reconnect requested")
		case f.Type == "CALLBACK" && f.Headers.Topic == messageTopic:
			select {
			case queue <- f:
			case <-ctx.Done():
				return ctx.Err()
			default:
				return errors.New("stream input queue full")
			}
		default:
			if err = ack(f, 404, `{}`); err != nil {
				return err
			}
		}
	}
}
