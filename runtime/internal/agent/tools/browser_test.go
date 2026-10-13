package tools

import (
	"encoding/json"
	"testing"
)

func TestBrowserArgumentsRejectControlAndLaunchOptions(t *testing.T) {
	for _, raw := range []string{`{"action":"navigate","url":"file:///etc/passwd"}`, `{"action":"eval","text":"alert(1)"}`, `{"action":"start","executable":"/bin/sh"}`, `{"action":"status"} {}`} {
		if _, err := ParseBrowser(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	q, err := ParseBrowser(json.RawMessage(`{"action":"fill","selector":"@e1","text":"--cdp=9222 中文"}`))
	if err != nil || q.Text != "--cdp=9222 中文" {
		t.Fatal(q, err)
	}
	for _, action := range []string{"viewport", "takeover", "release", "pointer", "insert_text", "wheel", "input_batch", "keydown", "reset_input"} {
		if _, err := parse(Call{Name: "browser_action", Arguments: json.RawMessage(`{"action":"` + action + `"}`)}); err == nil {
			t.Fatal("model control bypass", action)
		}
	}
}
