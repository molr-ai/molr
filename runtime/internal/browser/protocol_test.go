package browser

import (
	"math"
	"testing"
)

func TestWheelInputBounds(t *testing.T) {
	for _, a := range []Action{
		{Action: "wheel", X: 10, Y: 20, DeltaX: 0.5, DeltaY: -1.25, Modifiers: 8},
		{Action: "wheel", DeltaY: 100000},
	} {
		if _, err := a.Args(); err != nil || !a.Input() {
			t.Fatal(a, err)
		}
	}
	for _, a := range []Action{
		{Action: "wheel"}, {Action: "wheel", DeltaY: 100001},
		{Action: "wheel", DeltaY: 1, X: 1281}, {Action: "wheel", DeltaY: 1, Modifiers: 16},
		{Action: "wheel", DeltaY: math.Inf(1)}, {Action: "wheel", DeltaX: math.NaN()},
	} {
		if _, err := a.Args(); err == nil {
			t.Fatal("invalid wheel accepted", a)
		}
	}
}

func TestActionDoesNotExposeLaunchOrArbitraryExecution(t *testing.T) {
	for _, a := range []Action{{Action: "eval", Text: "process.exit()"}, {Action: "navigate", URL: "file:///etc/shadow"}, {Action: "navigate", URL: "javascript:alert(1)"}, {Action: "navigate", URL: "http://user:pass@example.com"}, {Action: "click", Selector: "--cdp=9222"}, {Action: "pointer", X: -1}, {Action: "press", Key: "--headed"}, {Action: "close_tab", Tab: "--cdp=9222"}} {
		if _, err := a.Args(); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	for _, a := range []Action{{Action: "navigate", URL: "https://example.com"}, {Action: "fill", Selector: "@e1", Text: "hello 世界"}, {Action: "snapshot"}, {Action: "tabs"}, {Action: "close_tab", Tab: "t2"}, {Action: "screenshot"}, {Action: "pointer", Event: "down", Button: "left", X: 10, Y: 20}} {
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
