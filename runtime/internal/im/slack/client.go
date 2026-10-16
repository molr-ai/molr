// Package slack implements the private Slack chat entrance using Socket Mode.
package slack

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"golang.org/x/net/websocket"
	"io"
	"molr.ai/runtime/internal/platform"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Identity struct{ TeamID, AppID, BotID, UserID string }
type File struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MIME       string `json:"mimetype"`
	URL        string `json:"url_private_download"`
	PrivateURL string `json:"url_private"`
	Size       int64  `json:"size"`
}
type Message struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	User        string `json:"user"`
	BotID       string `json:"bot_id"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type"`
	Text        string `json:"text"`
	TS          string `json:"ts"`
	ThreadTS    string `json:"thread_ts"`
	Files       []File `json:"files"`
}
type Event struct {
	Type    string  `json:"type"`
	TeamID  string  `json:"team_id"`
	AppID   string  `json:"api_app_id"`
	ID      string  `json:"event_id"`
	Message Message `json:"event"`
}
type Envelope struct {
	Type       string `json:"type"`
	ID         string `json:"envelope_id"`
	Payload    Event  `json:"payload"`
	Connection struct {
		AppID string `json:"app_id"`
	} `json:"connection_info"`
}
type Socket interface {
	Read() (Envelope, error)
	Ack(string) error
	Close() error
}
type Media struct {
	Name, Type string
	Data       []byte
}
type Client interface {
	Identity(context.Context, string) (Identity, error)
	Connect(context.Context, string, string) (Socket, error)
	Send(context.Context, string, string, string) (string, error)
	Download(context.Context, string, File, int64) (Media, error)
	Upload(context.Context, string, string, Media) (string, error)
}
type apiError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *apiError) Error() string { return "Slack request failed: " + e.Code }
func failure(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		switch e.Code {
		case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "app_mismatch":
			return "login_required"
		case "ratelimited":
			return "rate_limited"
		case "unknown", "internal_error", "fatal_error", "service_unavailable":
			return "unknown"
		case "attachment_failed":
			return "attachment_failed"
		case "missing_scope":
			return "missing_scope"
		}
		return "rejected"
	}
	return "unknown"
}
func validID(id, prefix string) bool {
	if len(id) < 2 || len(id) > 64 || !strings.ContainsRune(prefix, rune(id[0])) {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func slackURL(raw, scheme string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != scheme || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, &apiError{Code: "invalid_url"}
	}
	h := strings.ToLower(u.Hostname())
	if h != "slack.com" && !strings.HasSuffix(h, ".slack.com") {
		return nil, &apiError{Code: "invalid_url"}
	}
	return u, nil
}

type HTTPClient struct{ client func(string) *http.Client }

func (h HTTPClient) http(origin string) *http.Client {
	if h.client != nil {
		return h.client(origin)
	}
	return platform.PublicHTTPClient(origin)
}
func (h HTTPClient) request(ctx context.Context, method, raw, token string, body io.Reader, contentType string, limit int64) ([]byte, error) {
	u, err := slackURL(raw, "https")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c := h.http("https://" + u.Host)
	defer c.CloseIdleConnections()
	resp, err := c.Do(req)
	if err != nil {
		return nil, &apiError{Code: "unknown"}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if n < 1 {
			n = 60
		}
		if n > 86400 {
			n = 86400
		}
		return nil, &apiError{Code: "ratelimited", RetryAfter: time.Duration(n) * time.Second}
	}
	if resp.StatusCode >= 500 {
		return nil, &apiError{Code: "unknown"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{Code: "http_error"}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, &apiError{Code: "unknown"}
	}
	return data, nil
}
func (h HTTPClient) call(ctx context.Context, method, token string, values url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := h.request(ctx, "POST", "https://slack.com/api/"+method, token, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", 2<<20)
	if err != nil {
		return err
	}
	var status struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &status) != nil || status.OK == nil {
		return &apiError{Code: "unknown"}
	}
	if !*status.OK {
		switch status.Error {
		case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "ratelimited", "internal_error", "fatal_error", "service_unavailable", "missing_scope":
			return &apiError{Code: status.Error}
		default:
			return &apiError{Code: "rejected"}
		}
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return &apiError{Code: "unknown"}
	}
	return nil
}
func (h HTTPClient) Identity(ctx context.Context, token string) (i Identity, err error) {
	var auth struct {
		Team string `json:"team_id"`
		Bot  string `json:"bot_id"`
		User string `json:"user_id"`
	}
	if err = h.call(ctx, "auth.test", token, nil, &auth); err != nil {
		return
	}
	var info struct {
		Bot struct {
			App     string `json:"app_id"`
			User    string `json:"user_id"`
			Deleted bool   `json:"deleted"`
		} `json:"bot"`
	}
	if err = h.call(ctx, "bots.info", token, url.Values{"bot": {auth.Bot}}, &info); err != nil {
		return
	}
	if !validID(auth.Team, "T") || !validID(auth.Bot, "B") || !validID(auth.User, "UW") || !validID(info.Bot.App, "A") || info.Bot.Deleted || info.Bot.User != auth.User {
		return i, &apiError{Code: "rejected"}
	}
	return Identity{auth.Team, info.Bot.App, auth.Bot, auth.User}, nil
}

type socket struct {
	conn *websocket.Conn
	stop func() bool
}

func (s *socket) Close() error { s.stop(); return s.conn.Close() }
func (s *socket) Read() (e Envelope, err error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	err = websocket.JSON.Receive(s.conn, &e)
	return
}
func (s *socket) Ack(id string) error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return websocket.JSON.Send(s.conn, map[string]string{"envelope_id": id})
}
func (h HTTPClient) Connect(ctx context.Context, token, app string) (Socket, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := h.call(ctx, "apps.connections.open", token, nil, &out); err != nil {
		return nil, err
	}
	u, err := slackURL(out.URL, "wss")
	if err != nil {
		return nil, err
	}
	transport := platform.PublicHTTPClient("https://" + u.Host).Transport.(*http.Transport)
	conn, err := transport.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), "443"))
	if err != nil {
		return nil, &apiError{Code: "unknown"}
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	tc := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	if err = tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, &apiError{Code: "unknown"}
	}
	config, err := websocket.NewConfig(out.URL, "https://slack.com")
	if err != nil {
		conn.Close()
		return nil, &apiError{Code: "unknown"}
	}
	ws, err := websocket.NewClient(config, tc)
	if err != nil {
		conn.Close()
		return nil, &apiError{Code: "unknown"}
	}
	ws.MaxPayloadBytes = 2 << 20
	s := &socket{conn: ws, stop: context.AfterFunc(ctx, func() { ws.Close() })}
	var hello Envelope
	if err = websocket.JSON.Receive(ws, &hello); err != nil || hello.Type != "hello" || hello.Connection.AppID != app {
		s.Close()
		return nil, &apiError{Code: "app_mismatch"}
	}
	_ = ws.SetDeadline(time.Time{})
	return s, nil
}
func (h HTTPClient) Send(ctx context.Context, token, channel, text string) (string, error) {
	var out struct {
		TS string `json:"ts"`
	}
	err := h.call(ctx, "chat.postMessage", token, url.Values{"channel": {channel}, "text": {text}, "mrkdwn": {"false"}, "parse": {"none"}, "unfurl_links": {"false"}, "unfurl_media": {"false"}}, &out)
	if err == nil && out.TS == "" {
		err = &apiError{Code: "unknown"}
	}
	return out.TS, err
}
func (h HTTPClient) Download(ctx context.Context, token string, f File, limit int64) (Media, error) {
	if f.Size < 0 || f.Size > limit {
		return Media{}, &apiError{Code: "attachment_failed"}
	}
	raw := f.URL
	if raw == "" {
		raw = f.PrivateURL
	}
	u, err := slackURL(raw, "https")
	if err != nil || u.Hostname() != "files.slack.com" {
		return Media{}, &apiError{Code: "attachment_failed"}
	}
	data, err := h.request(ctx, "GET", raw, token, nil, "", limit)
	if err != nil {
		return Media{}, err
	}
	return Media{Name: f.Name, Type: f.MIME, Data: data}, nil
}
func (h HTTPClient) Upload(ctx context.Context, token, channel string, f Media) (string, error) {
	var out struct {
		URL string `json:"upload_url"`
		ID  string `json:"file_id"`
	}
	if err := h.call(ctx, "files.getUploadURLExternal", token, url.Values{"filename": {f.Name}, "length": {strconv.Itoa(len(f.Data))}}, &out); err != nil {
		return "", &apiError{Code: "attachment_failed"}
	}
	if !validID(out.ID, "F") {
		return "", &apiError{Code: "attachment_failed"}
	}
	if _, err := h.request(ctx, "POST", out.URL, "", bytes.NewReader(f.Data), "application/octet-stream", 1<<20); err != nil {
		return "", &apiError{Code: "attachment_failed"}
	}
	list, _ := json.Marshal([]map[string]string{{"id": out.ID, "title": f.Name}})
	// Only completion shares the file; an uncertain completion must not be retried.
	err := h.call(ctx, "files.completeUploadExternal", token, url.Values{"files": {string(list)}, "channel_id": {channel}}, nil)
	return "", err
}
