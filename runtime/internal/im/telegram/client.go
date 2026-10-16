// Package telegram implements a fixed, private-chat Bot API channel.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/platform"
)

const origin = "https://api.telegram.org"
const downloadLimit int64 = 20 * 1000 * 1000
const uploadLimit int64 = 50 * 1000 * 1000

var tokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{20,200}$`)

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type Media struct {
	ID   string `json:"file_id"`
	Size int64  `json:"file_size"`
	Name string `json:"file_name"`
	Type string `json:"mime_type"`
}
type Message struct {
	ID       int64    `json:"message_id"`
	From     *User    `json:"from"`
	Chat     Chat     `json:"chat"`
	ThreadID int64    `json:"message_thread_id"`
	Text     string   `json:"text"`
	Caption  string   `json:"caption"`
	Photo    []Media  `json:"photo"`
	Document *Media   `json:"document"`
	Voice    *Media   `json:"voice"`
	Audio    *Media   `json:"audio"`
	Video    *Media   `json:"video"`
	Reply    *Message `json:"reply_to_message"`
}
type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message"`
}
type MediaFile struct {
	Name, Type string
	Data       []byte
}
type Client interface {
	Me(context.Context, string) (User, error)
	Webhook(context.Context, string) (bool, error)
	Poll(context.Context, string, int64, int) ([]Update, error)
	Send(context.Context, string, int64, string) (int64, error)
	Download(context.Context, string, Media, int64) (MediaFile, error)
	SendMedia(context.Context, string, int64, MediaFile) (int64, error)
}

// Errors never include Telegram descriptions or request URLs (both can contain secrets).
type apiError struct {
	Code       int
	RetryAfter int
	Reason     string
}

func (e *apiError) Error() string { return "telegram request failed: " + e.Reason }
func failure(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Reason
	}
	return "unknown"
}

type HTTPClient struct{ client *http.Client }

func NewHTTPClient(proxy string) (*HTTPClient, error) {
	c := platform.PublicHTTPClient(origin)
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("invalid Telegram proxy URL")
		}
		// Only a deployment-selected proxy may replace direct dialing. TLS still authenticates Telegram.
		c = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	c.Timeout = 90 * time.Second
	return &HTTPClient{client: c}, nil
}
func (h *HTTPClient) request(ctx context.Context, token, method, contentType string, body io.Reader, result any) error {
	if !tokenPattern.MatchString(token) {
		return &apiError{Reason: "token_invalid"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/bot"+token+"/"+method, body)
	if err != nil {
		return &apiError{Reason: "rejected"}
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := h.client.Do(req)
	if err != nil {
		return &apiError{Reason: "unknown"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return &apiError{Reason: "unknown"}
	}
	var envelope struct {
		OK         *bool           `json:"ok"`
		Result     json.RawMessage `json:"result"`
		Code       int             `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return &apiError{Reason: "unknown"}
	}
	if envelope.OK == nil {
		return &apiError{Reason: "unknown"}
	}
	if resp.StatusCode != 200 || !*envelope.OK {
		code := envelope.Code
		if code == 0 {
			code = resp.StatusCode
		}
		reason := "rejected"
		switch {
		case code == 401:
			reason = "token_invalid"
		case code == 403:
			reason = "blocked"
		case code == 409:
			reason = "poll_conflict"
		case code == 429:
			reason = "rate_limited"
		case code >= 500:
			reason = "unknown"
		}
		return &apiError{Code: code, Reason: reason, RetryAfter: envelope.Parameters.RetryAfter}
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" || json.Unmarshal(envelope.Result, result) != nil {
		return &apiError{Reason: "unknown"}
	}
	return nil
}
func (h *HTTPClient) call(ctx context.Context, token, method string, input, result any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return &apiError{Reason: "rejected"}
	}
	return h.request(ctx, token, method, "application/json", bytes.NewReader(body), result)
}
func (h *HTTPClient) Me(ctx context.Context, token string) (u User, err error) {
	err = h.call(ctx, token, "getMe", struct{}{}, &u)
	if err == nil && (!u.IsBot || u.ID <= 0 || u.Username == "") {
		err = &apiError{Reason: "token_invalid"}
	}
	return
}
func (h *HTTPClient) Webhook(ctx context.Context, token string) (bool, error) {
	var r struct {
		URL string `json:"url"`
	}
	err := h.call(ctx, token, "getWebhookInfo", struct{}{}, &r)
	return r.URL != "", err
}
func (h *HTTPClient) Poll(ctx context.Context, token string, offset int64, timeout int) (r []Update, err error) {
	err = h.call(ctx, token, "getUpdates", map[string]any{"offset": offset, "timeout": timeout, "limit": 100, "allowed_updates": []string{"message"}}, &r)
	return
}
func (h *HTTPClient) Send(ctx context.Context, token string, chat int64, text string) (int64, error) {
	var m Message
	err := h.call(ctx, token, "sendMessage", map[string]any{"chat_id": chat, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}, &m)
	if err == nil && m.ID <= 0 {
		err = &apiError{Reason: "unknown"}
	}
	return m.ID, err
}
func (h *HTTPClient) Download(ctx context.Context, token string, m Media, limit int64) (MediaFile, error) {
	if limit > downloadLimit {
		limit = downloadLimit
	}
	if m.Size > limit || m.Size < 0 {
		return MediaFile{}, errors.New("attachment too large")
	}
	var f struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := h.call(ctx, token, "getFile", map[string]string{"file_id": m.ID}, &f); err != nil {
		return MediaFile{}, err
	}
	if f.Path == "" || f.Size > limit || strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") || strings.ContainsAny(f.Path, ":%?#\\") {
		return MediaFile{}, errors.New("invalid attachment path")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/file/bot"+token+"/"+f.Path, nil)
	if err != nil {
		return MediaFile{}, errors.New("invalid attachment")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return MediaFile{}, errors.New("attachment download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return MediaFile{}, errors.New("attachment download failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return MediaFile{}, errors.New("attachment download failed")
	}
	name := path.Base(m.Name)
	if name == "." || name == "/" || name == "" {
		name = path.Base(f.Path)
	}
	typ := m.Type
	if typ == "" {
		typ = mime.TypeByExtension(path.Ext(name))
	}
	if typ == "" {
		typ = http.DetectContentType(data)
	}
	return MediaFile{Name: name, Type: typ, Data: data}, nil
}
func (h *HTTPClient) SendMedia(ctx context.Context, token string, chat int64, file MediaFile) (int64, error) {
	if int64(len(file.Data)) > uploadLimit {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	method, field := "sendDocument", "document"
	if (file.Type == "image/jpeg" || file.Type == "image/png") && len(file.Data) <= 10*1000*1000 {
		method, field = "sendPhoto", "photo"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chat, 10)); err != nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	part, err := writer.CreateFormFile(field, path.Base(file.Name))
	if err != nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	if _, err = part.Write(file.Data); err != nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	if err = writer.Close(); err != nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	var m Message
	err = h.request(ctx, token, method, writer.FormDataContentType(), &body, &m)
	if err == nil && m.ID <= 0 {
		err = &apiError{Reason: "unknown"}
	}
	return m.ID, err
}
