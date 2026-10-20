package browser

import "path/filepath"

// NativeCommand maps the validated public action to the pinned daemon protocol.
// Internal launch, CDP and filesystem options are never accepted from clients.
func NativeCommand(a Action, workspace string) (map[string]any, error) {
	if _, err := a.Args(); err != nil {
		return nil, err
	}
	c := map[string]any{"action": a.Action}
	switch a.Action {
	case "viewport":
		c["width"], c["height"] = ViewportWidth, a.Height
	case "screenshot":
		c["path"] = filepath.Join(workspace, "outputs", "browser-screenshot.png")
	case "tabs":
		c["action"] = "tab_list"
	case "get_url":
		c["action"] = "url"
	case "navigate":
		c["action"], c["url"] = "navigate", a.URL
	case "new_tab":
		c["action"], c["url"] = "tab_new", a.URL
	case "click":
		c["selector"] = a.Selector
	case "fill":
		c["selector"], c["value"] = a.Selector, a.Text
	case "press":
		c["key"] = a.Key
	case "insert_text":
		c["action"], c["text"] = "inserttext", a.Text
	case "scroll":
		c["direction"], c["amount"] = a.Direction, 400
	case "select_tab":
		c["action"], c["tabId"] = "tab_switch", a.Tab
	case "close_tab":
		c["action"] = "tab_close"
		if a.Tab != "" {
			c["tabId"] = a.Tab
		}
	}
	return c, nil
}
