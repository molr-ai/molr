// Package feishu implements the private-chat channel using Feishu's application API.
package feishu

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"molr.ai/runtime/internal/platform"
)

const apiOrigin = "https://open.feishu.cn"
const registrationOrigin = "https://accounts.feishu.cn"

type Credentials struct{ AppID, AppSecret, OpenID string }
type Registration struct {
	DeviceCode, URL     string
	ExpiresIn, Interval int
}
type RegistrationResult struct {
	Credentials
	Status string
}
type Incoming struct{ AppID, Tenant, OpenID, SenderType, ChatType, ChatID, MessageID, Type, Content string }
type Client interface {
	Begin(context.Context) (Registration, error)
	Poll(context.Context, string) (RegistrationResult, error)
	Check(context.Context, Credentials) error
	Listen(context.Context, Credentials, func(Incoming) error, func(bool)) error
	Send(context.Context, Credentials, string, string) (string, error)
}
type apiError struct {
	reason     string
	retryAfter time.Duration
}

func (e *apiError) Error() string { return "feishu: " + e.reason }
func failure(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.reason
	}
	return "unknown"
}

// All HTTP responses are bounded and no credentials follow redirects.
type HTTPClient struct{ client func(string) *http.Client }

func (h HTTPClient) http(origin string) *http.Client {
	if h.client != nil {
		return h.client(origin)
	}
	c := platform.PublicHTTPClient(origin)
	c.Timeout = 15 * time.Second
	return c
}
func (h HTTPClient) request(ctx context.Context, origin, path, token, contentType string, data []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.http(origin).Do(req)
	if err != nil {
		return &apiError{reason: "unknown"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.Code != 0 {
			return apiFailure(envelope.Code)
		}
		return &apiError{reason: "rejected"}
	}
	if len(body) == 0 || bytes.Equal(bytes.TrimSpace(body), []byte("null")) || json.Unmarshal(body, out) != nil {
		return &apiError{reason: "unknown"}
	}
	return nil
}
func (h HTTPClient) registration(ctx context.Context, values url.Values, out any) error {
	return h.request(ctx, registrationOrigin, "/oauth/v1/app/registration", "", "application/x-www-form-urlencoded", []byte(values.Encode()), out)
}
func (h HTTPClient) Begin(ctx context.Context) (Registration, error) {
	var r struct {
		DeviceCode string `json:"device_code"`
		URL        string `json:"verification_uri_complete"`
		Expire     int    `json:"expire_in"`
		Expires    int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	err := h.registration(ctx, url.Values{"action": {"begin"}, "archetype": {"PersonalAgent"}, "auth_method": {"client_secret"}, "request_user_info": {"open_id"}}, &r)
	if err != nil {
		return Registration{}, err
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "https" || (u.Host != "accounts.feishu.cn" && u.Host != "open.feishu.cn") || u.User != nil || r.DeviceCode == "" || len(r.DeviceCode) > 8192 {
		return Registration{}, &apiError{reason: "rejected"}
	}
	// Protocol defined by the official SDK's scene/registration package (v3.12.2).
	addons := []byte(`{"preset":false,"scopes":{"tenant":["im:message.p2p_msg:readonly","im:message:send_as_bot"]},"events":{"items":{"tenant":["im.message.receive_v1"]}}}`)
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	if _, err = z.Write(addons); err != nil {
		return Registration{}, err
	}
	if err = z.Close(); err != nil {
		return Registration{}, err
	}
	q := u.Query()
	q.Set("addons", base64.RawURLEncoding.EncodeToString(buf.Bytes()))
	q.Set("createOnly", "true")
	q.Set("from", "sdk")
	q.Set("tp", "sdk")
	q.Set("source", "molr")
	q.Set("name", "Molr Assistant")
	u.RawQuery = q.Encode()
	if r.Expires > 0 {
		r.Expire = r.Expires
	}
	if r.Expire <= 0 || r.Expire > 600 {
		r.Expire = 600
	}
	if r.Interval < 5 {
		r.Interval = 5
	}
	return Registration{r.DeviceCode, u.String(), r.Expire, r.Interval}, nil
}
func (h HTTPClient) Poll(ctx context.Context, device string) (RegistrationResult, error) {
	var r struct {
		AppID  string `json:"client_id"`
		Secret string `json:"client_secret"`
		Error  string `json:"error"`
		User   struct {
			OpenID string `json:"open_id"`
			Brand  string `json:"tenant_brand"`
		} `json:"user_info"`
	}
	if err := h.registration(ctx, url.Values{"action": {"poll"}, "device_code": {device}}, &r); err != nil {
		return RegistrationResult{}, err
	}
	if r.User.Brand == "lark" {
		return RegistrationResult{Status: "unsupported_tenant"}, nil
	}
	if r.AppID != "" && r.Secret != "" && r.User.OpenID != "" {
		return RegistrationResult{Credentials{r.AppID, r.Secret, r.User.OpenID}, "confirmed"}, nil
	}
	switch r.Error {
	case "", "authorization_pending":
		return RegistrationResult{Status: "waiting"}, nil
	case "slow_down":
		return RegistrationResult{Status: "slow_down"}, nil
	case "access_denied":
		return RegistrationResult{Status: "denied"}, nil
	case "expired_token":
		return RegistrationResult{Status: "expired"}, nil
	}
	return RegistrationResult{}, &apiError{reason: "rejected"}
}
func apiFailure(code int) error {
	if code == 0 {
		return nil
	}
	if code == 99991661 || code == 99991663 || code == 99991664 || code == 99991665 || code == 10003 || code == 10014 {
		return &apiError{reason: "credentials_required"}
	}
	if code == 99991400 || code == 230020 {
		return &apiError{reason: "rate_limited"}
	}
	return &apiError{reason: "rejected"}
}
func (h HTTPClient) token(ctx context.Context, c Credentials) (string, error) {
	body, _ := json.Marshal(map[string]string{"app_id": c.AppID, "app_secret": c.AppSecret})
	var r struct {
		Code  *int   `json:"code"`
		Token string `json:"tenant_access_token"`
	}
	if err := h.request(ctx, apiOrigin, "/open-apis/auth/v3/tenant_access_token/internal", "", "application/json", body, &r); err != nil {
		return "", err
	}
	if r.Code == nil {
		return "", &apiError{reason: "unknown"}
	}
	if err := apiFailure(*r.Code); err != nil {
		return "", err
	}
	if r.Token == "" {
		return "", &apiError{reason: "unknown"}
	}
	return r.Token, nil
}
func (h HTTPClient) Check(ctx context.Context, c Credentials) error {
	_, err := h.token(ctx, c)
	return err
}
func (h HTTPClient) Send(ctx context.Context, c Credentials, id, text string) (string, error) {
	token, err := h.token(ctx, c)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	body, _ := json.Marshal(map[string]string{"receive_id": c.OpenID, "msg_type": "text", "content": string(content), "uuid": id})
	var r struct {
		Code *int `json:"code"`
		Data struct {
			ID string `json:"message_id"`
		} `json:"data"`
	}
	if err = h.request(ctx, apiOrigin, "/open-apis/im/v1/messages?receive_id_type=open_id", token, "application/json", body, &r); err != nil {
		return "", err
	}
	if r.Code == nil {
		return "", &apiError{reason: "unknown"}
	}
	if err = apiFailure(*r.Code); err != nil {
		return "", err
	}
	if r.Data.ID == "" {
		return "", &apiError{reason: "unknown"}
	}
	return r.Data.ID, nil
}

type silentLogger struct{}

func (silentLogger) Debug(context.Context, ...interface{}) {}
func (silentLogger) Info(context.Context, ...interface{})  {}
func (silentLogger) Warn(context.Context, ...interface{})  {}
func (silentLogger) Error(context.Context, ...interface{}) {}
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func (h HTTPClient) Listen(ctx context.Context, c Credentials, receive func(Incoming) error, state func(bool)) error {
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(_ context.Context, e *larkim.P2MessageReceiveV1) error {
		if e == nil || e.EventV2Base == nil || e.EventV2Base.Header == nil || e.Event == nil || e.Event.Message == nil || e.Event.Sender == nil || e.Event.Sender.SenderId == nil {
			return nil
		}
		m, s := e.Event.Message, e.Event.Sender
		return receive(Incoming{AppID: e.EventV2Base.Header.AppID, Tenant: e.TenantKey(), OpenID: str(s.SenderId.OpenId), SenderType: str(s.SenderType), ChatType: str(m.ChatType), ChatID: str(m.ChatId), MessageID: str(m.MessageId), Type: str(m.MessageType), Content: str(m.Content)})
	})
	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !(strings.HasSuffix(host, ".feishu.cn") || strings.HasSuffix(host, ".larksuite.com")) {
			return nil, errors.New("unapproved feishu websocket host")
		}
		transport := platform.PublicHTTPClient("https://" + address).Transport.(*http.Transport)
		return transport.DialContext(ctx, network, address)
	}}
	ws := larkws.NewClient(c.AppID, c.AppSecret, larkws.WithEventHandler(handler), larkws.WithHttpClient(h.http(apiOrigin)), larkws.WithWebSocketDialer(dialer), larkws.WithLogger(silentLogger{}), larkws.WithOnReady(func() { state(true) }), larkws.WithOnReconnected(func() { state(true) }), larkws.WithOnReconnecting(func() { state(false) }), larkws.WithOnDisconnected(func() { state(false) }))
	err := ws.Start(ctx)
	var clientErr *larkws.ClientError
	if errors.As(err, &clientErr) {
		return &apiError{reason: "credentials_required"}
	}
	return err
}

func responseRetryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && time.Until(at) > 0 {
		return time.Until(at)
	}
	return 0
}
