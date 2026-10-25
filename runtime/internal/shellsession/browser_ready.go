package shellsession

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"net/http"
)

// BrowserReady inspects the fixed browser startup diagnostic endpoint. It does
// not accept an arbitrary path or issue a browser action.
func (s *Manager) BrowserReady(ctx context.Context, p core.Principal, assistant, task core.ID) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if err := s.host.CheckAssistant(ctx, p, assistant); err != nil {
		return nil, err
	}
	r := s.activeBrowser(p, assistant, task)
	if r == nil {
		return nil, core.NewError(core.NotFound, "browser task not running")
	}
	return s.browserCall(ctx, r, http.MethodGet, "/ready", nil)
}
