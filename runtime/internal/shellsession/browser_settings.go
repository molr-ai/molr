package shellsession

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strings"
)

const browserSettingsBucket = "system.browser_settings"
const browserSettingsKey = "proxy"

var browserSettingsAAD = []byte("molr/browser-proxy/v1")

type browserSettingsRecord struct {
	Proxy       browser.ProxySummary
	Credentials []byte
}
type BrowserSettingsView struct {
	browser.ProxySummary
	Username           string
	PasswordConfigured bool
}
type BrowserSettingsRequest struct {
	Mode           string
	Server         string
	Bypass         []string
	Username       string
	Password       string
	PasswordAction string // keep, replace, clear
	Version        uint64
	Verification   string
}

func (s *Manager) loadBrowserProxy(ctx context.Context) (browser.Proxy, error) {
	var row browserSettingsRecord
	err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, browserSettingsBucket, browserSettingsKey, &row)
	})
	if persistence.Missing(err) {
		return browser.Proxy{ProxySummary: browser.ProxySummary{Mode: "direct", Bypass: []string{}}}, nil
	}
	if err != nil {
		return browser.Proxy{}, err
	}
	p := browser.Proxy{ProxySummary: row.Proxy}
	if len(row.Credentials) > 0 {
		box := s.host.ProxyCipher()
		if len(row.Credentials) < box.NonceSize() {
			return p, core.NewError(core.Internal, "browser proxy credential unavailable")
		}
		data, err := box.Open(nil, row.Credentials[:box.NonceSize()], row.Credentials[box.NonceSize():], browserSettingsAAD)
		if err != nil {
			return p, core.NewError(core.Internal, "browser proxy credential unavailable")
		}
		defer clear(data)
		var secret struct{ Username, Password string }
		if json.Unmarshal(data, &secret) != nil {
			return p, core.NewError(core.Internal, "browser proxy credential unavailable")
		}
		p.Username, p.Password = secret.Username, secret.Password
	}
	if err := p.Validate(); err != nil {
		return browser.Proxy{}, core.NewError(core.Internal, "invalid stored browser proxy settings")
	}
	return p, nil
}
func proxySettingsView(p browser.Proxy) BrowserSettingsView {
	return BrowserSettingsView{ProxySummary: p.ProxySummary, Username: p.Username, PasswordConfigured: p.Authentication}
}
func (s *Manager) BrowserSettings(ctx context.Context, p core.Principal, raw json.RawMessage) (BrowserSettingsView, error) {
	manager, ok := s.identity.(interface {
		RequireAdministrator(context.Context, core.Principal) error
	})
	if !ok {
		return BrowserSettingsView{}, core.NewError(core.Unavailable, "browser settings unavailable")
	}
	if err := manager.RequireAdministrator(ctx, p); err != nil {
		return BrowserSettingsView{}, err
	}
	current, err := s.loadBrowserProxy(ctx)
	if err != nil {
		return BrowserSettingsView{}, err
	}
	if len(raw) == 0 {
		return proxySettingsView(current), nil
	}
	var q BrowserSettingsRequest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 16<<10 || d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		return BrowserSettingsView{}, core.Invalid("settings", "invalid browser settings")
	}
	if q.Version != current.Version {
		return BrowserSettingsView{}, core.NewError(core.Conflict, "browser settings changed; reload before saving")
	}
	next := browser.Proxy{ProxySummary: browser.ProxySummary{Mode: q.Mode, Server: strings.TrimSpace(q.Server), Bypass: q.Bypass, Version: q.Version + 1}, Username: q.Username}
	switch q.PasswordAction {
	case "keep":
		if q.Password != "" || q.Username != current.Username || q.Mode == "proxy" && current.Authentication && next.Server != current.Server {
			return BrowserSettingsView{}, core.Invalid("password", "re-enter credentials when changing the proxy server or username")
		}
		next.Password = current.Password
	case "replace":
		next.Password = q.Password
	case "clear":
		if q.Password != "" || q.Username != "" {
			return BrowserSettingsView{}, core.Invalid("password", "clear both username and password")
		}
	default:
		return BrowserSettingsView{}, core.Invalid("password_action", "keep, replace or clear required")
	}
	if next.Bypass == nil {
		next.Bypass = []string{}
	}
	if err := next.Validate(); err != nil {
		return BrowserSettingsView{}, core.Invalid("proxy", err.Error())
	}
	if err := s.host.VerifyProxy(ctx, p, auth.Verification{Token: core.NewSecret(q.Verification)}); err != nil {
		return BrowserSettingsView{}, err
	}
	row := browserSettingsRecord{Proxy: next.ProxySummary}
	if next.Authentication {
		secret, _ := json.Marshal(struct{ Username, Password string }{next.Username, next.Password})
		defer clear(secret)
		box := s.host.ProxyCipher()
		nonce := make([]byte, box.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return BrowserSettingsView{}, err
		}
		row.Credentials = box.Seal(nonce, nonce, secret, browserSettingsAAD)
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old browserSettingsRecord
		err := tx.Get(ctx, browserSettingsBucket, browserSettingsKey, &old)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if old.Proxy.Version != q.Version {
			return core.NewError(core.Conflict, "browser settings changed; reload before saving")
		}
		return tx.Put(ctx, browserSettingsBucket, browserSettingsKey, row)
	})
	if err != nil {
		return BrowserSettingsView{}, err
	}
	return proxySettingsView(next), nil
}
