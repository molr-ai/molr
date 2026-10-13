package browser

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
)

const CommandPort = 9224
const StreamPort = 9223

// Actions are explicit: clients cannot select executables, launch flags, CDP
// endpoints, shell scripts, session names or host filesystem paths.
type Action struct {
	Action    string  `json:"action"`
	URL       string  `json:"url,omitempty"`
	Selector  string  `json:"selector,omitempty"`
	Text      string  `json:"text,omitempty"`
	Key       string  `json:"key,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Tab       string  `json:"tab,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Button    string  `json:"button,omitempty"`
	Event     string  `json:"event,omitempty"`
	DeltaX    float64 `json:"delta_x,omitempty"`
	DeltaY    float64 `json:"delta_y,omitempty"`
	Modifiers int     `json:"modifiers,omitempty"`
}

// Input actions are bounded, low-level operations suitable for a short batch.
func (a Action) Input() bool {
	return a.Action == "pointer" || a.Action == "wheel" || a.Action == "insert_text" || a.Action == "press"
}

func (a Action) ReadOnly() bool {
	return a.Action == "snapshot" || a.Action == "screenshot" || a.Action == "tabs" || a.Action == "get_url"
}
func (a Action) Args() ([]string, error) {
	bad := func() ([]string, error) { return nil, fmt.Errorf("invalid browser action") }
	if a.Modifiers < 0 || a.Modifiers > 15 {
		return bad()
	}
	if len(a.Text) > 8192 || len(a.Selector) > 2048 || strings.ContainsRune(a.Selector, 0) || len(a.URL) > 8192 {
		return bad()
	}
	selector := func() bool { return a.Selector != "" && !strings.HasPrefix(a.Selector, "-") }
	tabValid := func() bool {
		return len(a.Tab) > 1 && len(a.Tab) < 16 && a.Tab[0] == 't' && strings.Trim(a.Tab[1:], "0123456789") == ""
	}
	switch a.Action {
	case "snapshot":
		return []string{"snapshot"}, nil
	case "screenshot":
		return []string{"screenshot", "/work/outputs/browser-screenshot.png"}, nil
	case "tabs":
		return []string{"tab"}, nil
	case "get_url":
		return []string{"get", "url"}, nil
	case "navigate", "new_tab":
		u, e := url.Parse(a.URL)
		if e != nil || (a.URL != "about:blank" && (u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil)) {
			return bad()
		}
		if a.Action == "new_tab" {
			return []string{"tab", "new", a.URL}, nil
		}
		return []string{"open", a.URL}, nil
	case "click":
		if selector() {
			return []string{"click", a.Selector}, nil
		}
	case "fill":
		if selector() {
			return []string{"fill", a.Selector, a.Text}, nil
		}
	case "press":
		if a.Key != "" && len(a.Key) < 100 && !strings.HasPrefix(a.Key, "-") {
			return []string{a.Action, a.Key}, nil
		}
	case "insert_text":
		return []string{"keyboard", "inserttext", a.Text}, nil
	case "scroll":
		if a.Direction == "up" || a.Direction == "down" || a.Direction == "left" || a.Direction == "right" {
			return []string{"scroll", a.Direction, "400"}, nil
		}
	case "back", "forward", "reload", "close":
		return []string{a.Action}, nil
	case "select_tab":
		if tabValid() {
			return []string{"tab", a.Tab}, nil
		}
	case "close_tab":
		if a.Tab != "" {
			if !tabValid() {
				return bad()
			}
			return []string{"tab", "close", a.Tab}, nil
		}
		return []string{"tab", "close"}, nil
	case "pointer", "wheel":
		if math.IsNaN(a.X) || math.IsNaN(a.Y) || a.X < 0 || a.X > 1280 || a.Y < 0 || a.Y > 800 {
			return bad()
		}
		if a.Action == "wheel" {
			if math.IsNaN(a.DeltaX) || math.IsNaN(a.DeltaY) || math.Abs(a.DeltaX) > 100000 || math.Abs(a.DeltaY) > 100000 || a.DeltaX == 0 && a.DeltaY == 0 {
				return bad()
			}
			return []string{"wheel"}, nil // Dispatched through the private structured protocol.
		}
		if a.Event == "move" {
			return []string{"mouse", "move", fmt.Sprint(a.X), fmt.Sprint(a.Y)}, nil
		}
		if (a.Event == "down" || a.Event == "up") && (a.Button == "left" || a.Button == "middle" || a.Button == "right") {
			return []string{"mouse", a.Event, a.Button}, nil
		}
	}
	return bad()
}

type Result struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}
