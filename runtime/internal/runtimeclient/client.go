// Package runtimeclient provides the authenticated HTTP transport for native clients.
package runtimeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Error is a safe Runtime response; transport details and credentials are not displayed.
type Error struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("Runtime 请求失败（HTTP %d，%s）", e.Status, e.Code)
}

// Client owns one Runtime origin and its session. It never follows redirects.
type Client struct {
	base         *url.URL
	http         *http.Client
	mu           sync.Mutex
	csrf, device string
}

// New validates an origin and optionally trusts an additional PEM certificate.
func New(server, caFile string) (*Client, error) {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("服务地址必须是 http(s)://主机[:端口]，不含路径、凭据或查询参数")
	}
	u.Path = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 20 * time.Second
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA 文件不包含有效证书")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	jar, _ := cookiejar.New(nil)
	return &Client{base: u, device: NewKey(), http: &http.Client{Jar: jar, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// NewKey allocates an idempotency key for one logical mutation.
func NewKey() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if !strings.HasPrefix(path, "/api/") || strings.ContainsAny(path, "\r\n") {
		return nil, errors.New("invalid API path")
	}
	var data io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		data = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, data)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Molr CLI")
	req.Header.Set("Origin", c.base.String())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.mu.Lock()
	csrf := c.csrf
	c.mu.Unlock()
	if method != "GET" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	return req, nil
}

// Do performs a bounded JSON request. Callers own mutation keys and retry decisions.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("无法连接 Runtime，请检查地址、证书和网络")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	if name := resp.Header.Get("X-Molr-CSRF-Cookie"); name != "" {
		for _, cookie := range c.http.Jar.Cookies(c.base) {
			if cookie.Name == name {
				c.mu.Lock()
				c.csrf = cookie.Value
				c.mu.Unlock()
			}
		}
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20))
		return err
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return errors.New("Runtime 返回了无效数据")
	}
	return nil
}

func responseError(resp *http.Response) error {
	var body struct{ Code string }
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	// Only known protocol codes reach the terminal.
	switch body.Code {
	case "unauthenticated", "forbidden", "invalid_argument", "not_found", "conflict", "failed_precondition", "rate_limited", "unavailable", "internal":
	default:
		body.Code = "request_failed"
	}
	delay := time.Duration(0)
	if seconds, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err == nil {
		delay = seconds
	}
	return &Error{Status: resp.StatusCode, Code: body.Code, RetryAfter: delay}
}

// Login starts password login, returning an MFA challenge when required.
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	var pre struct {
		CSRF string `json:"csrf_token"`
	}
	if err := c.Do(ctx, "GET", "/api/auth/csrf", nil, &pre); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.csrf = pre.CSRF
	c.mu.Unlock()
	return c.authenticate(ctx, "/api/auth/login", map[string]any{"username": username, "password": password, "remember_me": true, "device_id": c.device, "client_type": "cli", "device_name": "Molr CLI"})
}

// CompleteMFA completes a pending login without storing the password or MFA code.
func (c *Client) CompleteMFA(ctx context.Context, challenge, code string) error {
	_, err := c.authenticate(ctx, "/api/auth/mfa/complete", map[string]any{"challenge": challenge, "code": code, "device_id": c.device, "client_type": "cli", "device_name": "Molr CLI"})
	return err
}
func (c *Client) authenticate(ctx context.Context, path string, body any) (string, error) {
	var result struct {
		Challenge string `json:"challenge"`
		CSRF      string `json:"csrf_token"`
	}
	if err := c.Do(ctx, "POST", path, body, &result); err != nil {
		return "", err
	}
	if result.Challenge == "" {
		if result.CSRF == "" {
			return "", errors.New("登录响应缺少会话证明")
		}
		c.mu.Lock()
		c.csrf = result.CSRF
		c.mu.Unlock()
	}
	return result.Challenge, nil
}

type savedSession struct {
	Server, CSRF, Device string
	Cookies              []*http.Cookie
}

// Restore loads credentials only for the exact configured origin.
func (c *Client) Restore(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false, errors.New("会话文件必须是权限 0600 的普通文件")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var saved savedSession
	if err = json.Unmarshal(raw, &saved); err != nil {
		return false, errors.New("会话文件无效")
	}
	if saved.Server != c.base.String() {
		return false, nil
	}
	c.mu.Lock()
	c.csrf, c.device = saved.CSRF, saved.Device
	c.mu.Unlock()
	c.http.Jar.SetCookies(c.base, saved.Cookies)
	return len(saved.Cookies) > 0, nil
}

// Save atomically persists the current session in an owner-only file.
func (c *Client) Save(path string) error {
	c.mu.Lock()
	saved := savedSession{Server: c.base.String(), CSRF: c.csrf, Device: c.device, Cookies: c.http.Jar.Cookies(c.base)}
	c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
