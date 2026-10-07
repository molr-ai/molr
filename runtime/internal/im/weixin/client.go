// Package weixin implements Molr's fixed iLink channel.
// Wire behavior was checked against @tencent-weixin/openclaw-weixin 2.4.9.
package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/platform"
)

const baseURL = "https://ilinkai.weixin.qq.com"

type QR struct {
	Code    string `json:"qrcode"`
	Content string `json:"qrcode_img_content"`
}
type LoginResult struct {
	Status   string `json:"status"`
	Token    string `json:"bot_token"`
	BotID    string `json:"ilink_bot_id"`
	UserID   string `json:"ilink_user_id"`
	BaseURL  string `json:"baseurl"`
	Redirect string `json:"redirect_host"`
}
type Item struct {
	Type  int             `json:"type"`
	ID    json.RawMessage `json:"msg_id,omitempty"`
	Ref   *Reference      `json:"ref_msg,omitempty"`
	Image MediaItem       `json:"image_item,omitempty"`
	File  MediaItem       `json:"file_item,omitempty"`
	Video MediaItem       `json:"video_item,omitempty"`
	Text  struct {
		Text string `json:"text"`
	} `json:"text_item"`
	Voice struct {
		Text       string   `json:"text"`
		Media      CDNMedia `json:"media"`
		Format     int      `json:"encode_type"`
		SampleRate int      `json:"sample_rate"`
	} `json:"voice_item"`
}
type Incoming struct {
	ID      json.RawMessage `json:"message_id"`
	From    string          `json:"from_user_id"`
	To      string          `json:"to_user_id"`
	Group   string          `json:"group_id"`
	Type    int             `json:"message_type"`
	Context string          `json:"context_token"`
	Items   []Item          `json:"item_list"`
}
type Updates struct {
	Ret      int        `json:"ret"`
	Errcode  int        `json:"errcode"`
	Cursor   string     `json:"get_updates_buf"`
	Messages []Incoming `json:"msgs"`
}
type Client interface {
	QR(context.Context, []string) (QR, error)
	Login(context.Context, string, string, string) (LoginResult, error)
	Poll(context.Context, string, string, string) (Updates, error)
	Check(context.Context, string, string, string) error
	Send(context.Context, string, string, string, string, string, string) (string, error)
}
type apiError struct {
	Code      int
	Uncertain bool
}

func (e *apiError) Error() string { return fmt.Sprintf("weixin response code %d", e.Code) }
func failure(err error) string {
	var media *mediaError
	if errors.As(err, &media) {
		return "attachment_failed"
	}
	var e *apiError
	if errors.As(err, &e) {
		if e.Uncertain {
			return "unknown"
		}
		if e.Code == -14 || e.Code == 401 {
			return "login_required"
		}
		if e.Code == -2 {
			return "context_required"
		}
		if e.Code == 429 {
			return "rate_limited"
		}
		return "rejected"
	}
	return "unknown"
}

// API hosts returned by login are accepted only within Tencent's Weixin domain.
// No credentials follow HTTP redirects; DNS destinations are checked at dial.
func validBase(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".weixin.qq.com") {
		return "", errors.New("invalid weixin API origin")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

type HTTPClient struct{ client func(string) *http.Client }

func (h HTTPClient) request(ctx context.Context, origin, endpoint, token string, body any, out any) error {
	origin, err := validBase(origin)
	if err != nil {
		return err
	}
	method := http.MethodGet
	var data []byte
	if body != nil {
		method = http.MethodPost
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+"/ilink/bot/"+endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", "132105") // protocol reference: 2.4.9
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(random[:])), 10))))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := platform.PublicHTTPClient(origin)
	client.Timeout = 40 * time.Second
	client.Transport.(*http.Transport).ResponseHeaderTimeout = 38 * time.Second
	if h.client != nil {
		client = h.client(origin)
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &apiError{Code: resp.StatusCode, Uncertain: resp.StatusCode >= 500}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("invalid weixin response")
	}
	return json.Unmarshal(data, out)
}
func metadata() map[string]string {
	return map[string]string{"channel_version": "2.4.9", "bot_agent": "Molr/0.1.0"}
}
func (h HTTPClient) QR(ctx context.Context, tokens []string) (q QR, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = h.request(ctx, baseURL, "get_bot_qrcode?bot_type=3", "", map[string]any{"local_token_list": tokens}, &q)
	if err == nil && (q.Code == "" || q.Content == "" || len(q.Code) > 8192 || len(q.Content) > 16384) {
		err = errors.New("invalid QR response")
	}
	return
}
func (h HTTPClient) Login(ctx context.Context, origin, code, verify string) (r LoginResult, err error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	q := url.Values{"qrcode": {code}}
	if verify != "" {
		q.Set("verify_code", verify)
	}
	err = h.request(ctx, origin, "get_qrcode_status?"+q.Encode(), "", nil, &r)
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		return LoginResult{Status: "wait"}, nil
	}
	return
}
func (h HTTPClient) Poll(ctx context.Context, origin, token, cursor string) (r Updates, err error) {
	err = h.request(ctx, origin, "getupdates", token, map[string]any{"get_updates_buf": cursor, "base_info": metadata()}, &r)
	if err == nil && (r.Ret != 0 || r.Errcode != 0) {
		code := r.Ret
		if r.Errcode != 0 {
			code = r.Errcode
		}
		err = &apiError{Code: code}
	}
	return
}
func (h HTTPClient) Send(ctx context.Context, origin, token, user, contextToken, clientID, text string) (string, error) {
	return h.sendItem(ctx, origin, token, user, contextToken, clientID, map[string]any{"type": 1, "text_item": map[string]string{"text": text}})
}
func (h HTTPClient) sendItem(ctx context.Context, origin, token, user, contextToken, clientID string, item any) (string, error) {
	var r *struct {
		ID      json.RawMessage `json:"message_id"`
		Ret     int             `json:"ret"`
		Errcode int             `json:"errcode"`
	}
	err := h.request(ctx, origin, "sendmessage", token, map[string]any{"base_info": metadata(), "msg": map[string]any{
		"from_user_id": "", "to_user_id": user, "client_id": clientID, "message_type": 2, "message_state": 2, "context_token": contextToken,
		"item_list": []any{item},
	}}, &r)
	if err != nil {
		return "", err
	}
	// iLink may omit zero-valued ret in successful JSON objects. Match the
	// reference plugin rather than turning those acknowledgements into unknown.
	// Missing/invalid JSON and explicit error codes still do not imply success.
	if r == nil {
		return "", errors.New("missing send response")
	}
	if r.Ret != 0 {
		return "", &apiError{Code: r.Ret}
	}
	if r.Errcode != 0 {
		return "", &apiError{Code: r.Errcode}
	}
	return messageID(r.ID), nil
}
func messageID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		if len(id) <= 128 {
			return id
		}
		return ""
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if _, err := strconv.ParseUint(n.String(), 10, 64); err == nil {
			return n.String()
		}
	}
	return ""
}

func (h HTTPClient) notify(ctx context.Context, origin, token string, start bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := "msg/notifystop"
	if start {
		endpoint = "msg/notifystart"
	}
	var response struct {
		Ret int `json:"ret"`
	}
	_ = h.request(ctx, origin, endpoint, token, map[string]any{"base_info": metadata()}, &response)
}

// Check validates an existing credential without consuming inbound updates.
func (h HTTPClient) Check(ctx context.Context, origin, token, user string) error {
	var result struct {
		Ret     *int `json:"ret"`
		Errcode int  `json:"errcode"`
	}
	if err := h.request(ctx, origin, "getconfig", token, map[string]any{"base_info": metadata(), "ilink_user_id": user}, &result); err != nil {
		return err
	}
	if result.Ret == nil {
		return errors.New("missing config result")
	}
	if result.Errcode != 0 {
		return &apiError{Code: result.Errcode}
	}
	if *result.Ret != 0 {
		return &apiError{Code: *result.Ret}
	}
	return nil
}
