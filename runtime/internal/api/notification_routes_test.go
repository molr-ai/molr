package api

import (
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/notification"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationRoutesRequireSessionAndCSRF(t *testing.T) {
	f := fixture(t)
	id := f.SeedNotification()
	handler := f.Server.Handler()
	read := f.ReadRequest()
	read.URL.Path = "/api/notifications"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, read)
	var page notification.Page
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.UnreadCount != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, valid := range []bool{false, true, true} {
		req := f.MutationRequest()
		req.URL.Path = "/api/notifications/update"
		body := `{"IDs":["` + string(id) + `"],"Action":"dismiss"}`
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
		if !valid {
			f.StripCSRF(req)
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		expected := 200
		if !valid {
			expected = 403
		}
		if w.Code != expected {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	read = f.ReadRequest()
	read.URL.Path = "/api/notifications/" + string(id)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, read)
	var n notification.Notice
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &n) != nil || n.DismissedAt == nil || n.ResolvedAt != nil || n.Version != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.RevokeSession()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, read)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
