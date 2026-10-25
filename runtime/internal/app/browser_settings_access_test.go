package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestBrowserProxySettingsRejectRegularMembers(t *testing.T) {
	f := newMultiUserFixture(t)
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{"Mode":"direct","PasswordAction":"clear","Version":0}`)} {
		if _, err := f.b.shellTasks.BrowserSettings(t.Context(), f.alice, raw); core.CodeOf(err) != core.Forbidden {
			t.Fatal("member accessed instance proxy settings", err)
		}
	}
}
