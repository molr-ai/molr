package shellsession

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"regexp"
	"time"
)

type browserNetworkFailure struct {
	TaskID  core.ID
	At      time.Time
	Reason  string
	Message string
	Proxy   browser.ProxySummary
	// A contemporaneous sandbox event, not necessarily the cause of the navigation failure.
	Sandbox *platform.NetworkDiagnostic `json:",omitempty"`
}

func browserNavigation(a browser.Action) bool {
	switch a.Action {
	case "navigate", "new_tab", "back", "forward", "reload":
		return true
	}
	return false
}
func (s *Manager) browserNetworkError(ctx context.Context, r *runningShell, a browser.Action, started time.Time, message string) error {
	if !browserNavigation(a) {
		return core.NewError(core.FailedPrecondition, message)
	}
	s.mu.Lock()
	failure := &browserNetworkFailure{TaskID: r.task.ID, At: time.Now().UTC(), Reason: browser.ProxyErrorReason(message), Message: safeBrowserMessage(message), Proxy: r.task.Browser.Proxy}
	if len(failure.Message) > 2048 {
		failure.Message = failure.Message[:2048]
	}
	if r.browserNetwork != nil {
		diagnostic := r.browserNetwork()
		if !diagnostic.At.Before(started) {
			failure.Sandbox = &diagnostic
		}
	}
	// Keep last failure scoped to this task, including when it subsequently exits.
	r.task.Browser.LastNetworkFailure = failure
	_ = s.save(context.WithoutCancel(ctx), r.task)
	s.mu.Unlock()
	raw, _ := json.Marshal(failure)
	return &core.Error{Code: core.FailedPrecondition, Reason: core.Reason(failure.Reason), Message: "Browser navigation failed. Inspect browser_session status and the proxy endpoint's sandbox network grants. Do not change proxy settings, grants or switch to direct access automatically. Details: " + string(raw)}
}
func (s *Manager) clearBrowserNetworkFailure(ctx context.Context, r *runningShell) {
	s.mu.Lock()
	changed := r.task.Browser.LastNetworkFailure != nil
	r.task.Browser.LastNetworkFailure = nil
	if changed {
		_ = s.save(context.WithoutCancel(ctx), r.task)
	}
	s.mu.Unlock()
}

// Remove credential-like URL userinfo from errors outside the browser helper too.
var browserNetworkCode = regexp.MustCompile(`net::ERR_[A-Z0-9_]+`)

func safeBrowserMessage(message string) string {
	if code := browserNetworkCode.FindString(message); code != "" {
		return code
	}
	return "Browser operation failed; cause undetermined."
}
