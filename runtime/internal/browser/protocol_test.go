package browser

import "testing"

func TestActionDoesNotExposeLaunchOrArbitraryExecution(t *testing.T) {
	for _, a := range []Action{{Action: "eval", Text: "process.exit()"}, {Action: "navigate", URL: "file:///etc/shadow"}, {Action: "navigate", URL: "javascript:alert(1)"}, {Action: "navigate", URL: "http://user:pass@example.com"}, {Action: "click", Selector: "--cdp=9222"}, {Action: "pointer", X: -1}, {Action: "press", Key: "--headed"}} {
		if _, err := a.Args(); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	for _, a := range []Action{{Action: "navigate", URL: "https://example.com"}, {Action: "fill", Selector: "@e1", Text: "hello 世界"}, {Action: "snapshot"}, {Action: "tabs"}, {Action: "screenshot"}, {Action: "pointer", Event: "down", Button: "left", X: 10, Y: 20}} {
		if _, err := a.Args(); err != nil {
			t.Fatalf("rejected %+v: %v", a, err)
		}
	}
	for _, name := range []string{"snapshot", "screenshot", "tabs", "get_url"} {
		if !(Action{Action: name}).ReadOnly() {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"navigate", "fill", "close", "select_tab", "press"} {
		if (Action{Action: name}).ReadOnly() {
			t.Fatal(name)
		}
	}
}
