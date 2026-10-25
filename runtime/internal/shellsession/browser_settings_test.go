package shellsession

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"strings"
	"testing"
	"time"
)

func TestBrowserProxySettingsEncryptedVersionedAndAdministratorOnly(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	defaults, err := s.BrowserSettings(t.Context(), p, nil)
	need(t, err)
	if defaults.(browserSettingsView).Mode != "direct" {
		t.Fatal(defaults)
	}
	write := func(q browserSettingsRequest) (browserSettingsView, error) {
		raw, _ := json.Marshal(q)
		v, err := s.BrowserSettings(t.Context(), p, raw)
		if err != nil {
			return browserSettingsView{}, err
		}
		return v.(browserSettingsView), nil
	}
	q := browserSettingsRequest{Mode: "proxy", Server: "http://proxy.test:8080", Username: "fixture-proxy-user", Password: "fixture-proxy-password", PasswordAction: "replace"}
	view, err := write(q)
	need(t, err)
	if view.Version != 1 || !view.PasswordConfigured {
		t.Fatal(view)
	}
	visible, _ := json.Marshal(view)
	if strings.Contains(string(visible), q.Password) {
		t.Fatal("password returned")
	}
	need(t, b.store.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var record browserSettingsRecord
		if err := tx.Get(ctx, browserSettingsBucket, browserSettingsKey, &record); err != nil {
			return err
		}
		raw, _ := json.Marshal(record)
		if strings.Contains(string(raw), q.Password) || strings.Contains(string(raw), q.Username) {
			t.Fatal("plaintext stored")
		}
		return nil
	}))
	if _, err = write(q); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale save", err)
	}
	q.Version = 1
	q.Password = ""
	q.PasswordAction = "keep"
	q.Bypass = []string{"*.example.test"}
	view, err = write(q)
	need(t, err)
	next, err := s.loadBrowserProxy(t.Context())
	need(t, err)
	if next.Password != "fixture-proxy-password" {
		t.Fatal("keep lost secret")
	}
	q.Version = 2
	q.Server = "http://other.test:8080"
	if _, err = write(q); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("credential silently retargeted", err)
	}
	if _, err = s.BrowserSettings(t.Context(), e.Principal, nil); err == nil {
		t.Fatal("assistant read administrator credentials")
	}
	q = browserSettingsRequest{Mode: "direct", PasswordAction: "clear", Version: 2}
	view, err = write(q)
	need(t, err)
	if view.Authentication || view.Username != "" {
		t.Fatal("credentials not cleared")
	}
}

func TestBrowserProxyStatusUsesLaunchSnapshotAndUpdatesDiagnostics(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	r.task.Browser.Proxy = browser.ProxySummary{Mode: "direct"}
	raw, _ := json.Marshal(browserSettingsRequest{Mode: "proxy", Server: "http://proxy.test:8080", PasswordAction: "clear"})
	_, err := s.BrowserSettings(t.Context(), p, raw)
	need(t, err)
	status, err := s.browserStatus(t.Context(), p, e.AssistantID)
	need(t, err)
	var info struct {
		Proxy              browser.ProxySummary
		RestartRequired    bool
		ProxyAppliesTo     string
		LastNetworkFailure *browserNetworkFailure
	}
	need(t, json.Unmarshal(status, &info))
	if info.Proxy.Mode != "direct" || !info.RestartRequired || info.ProxyAppliesTo != "current_session" {
		t.Fatal(string(status))
	}
	s.browserRoundTrip = func(context.Context, *runningShell, []byte) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":false,"error":"net::ERR_PROXY_CONNECTION_FAILED at http://user:secret@site.test/path"}`), nil
	}
	r.browserNetwork = func() platform.NetworkDiagnostic {
		return platform.NetworkDiagnostic{At: time.Now(), Reason: "network_destination_denied"}
	}
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "navigate", URL: "https://example.test"}})
	if core.ReasonOf(err) != "proxy_connection_failed" || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	modelResult := tools.ErrorResult(tools.Call{Execution: e}, err)
	if !strings.Contains(string(modelResult.Content), "Proxy") || !strings.Contains(string(modelResult.Content), "proxy_connection_failed") {
		t.Fatal("model cannot see diagnostics", string(modelResult.Content))
	}
	status, err = s.browserStatus(t.Context(), p, e.AssistantID)
	need(t, err)
	need(t, json.Unmarshal(status, &info))
	if info.LastNetworkFailure == nil || info.LastNetworkFailure.Sandbox == nil || info.LastNetworkFailure.Proxy.Mode != "direct" {
		t.Fatal(string(status))
	}
	projected, err := prompt.Structured("browser_session", string(status))
	need(t, err)
	for _, key := range []string{"Proxy", "current_session", "RestartRequired", "proxy_connection_failed", "network_destination_denied"} {
		if !strings.Contains(projected, key) {
			t.Fatal("model projection lost", key, projected)
		}
	}

	s.browserRoundTrip = func(context.Context, *runningShell, []byte) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "reload"}})
	need(t, err)
	if r.task.Browser.LastNetworkFailure != nil {
		t.Fatal("successful navigation did not clear failure")
	}
}
