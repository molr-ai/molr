package api

import (
	"encoding/json"
	"molr.ai/runtime/internal/business/dream"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDreamConflictsChecksUpcomingDatesWhenEditingOldSeries(t *testing.T) {
	f := fixture(t)
	id := f.SeedDreamAssistant()
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -150)
	old = time.Date(old.Year(), old.Month(), old.Day(), 3, 5, 0, 0, time.UTC)
	query := url.Values{"assistant_id": {string(id)}, "start_at": {old.Format(time.RFC3339)}, "timezone": {"UTC"}, "recurrence": {"FREQ=DAILY"}}
	check := func() []dream.Window {
		t.Helper()
		r := f.ReadRequest()
		r.URL.Path = "/api/dreams/conflicts"
		r.URL.RawQuery = query.Encode()
		w := httptest.NewRecorder()
		f.Server.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var windows []dream.Window
		if err := json.Unmarshal(w.Body.Bytes(), &windows); err != nil {
			t.Fatal(err)
		}
		return windows
	}
	windows := check()
	if len(windows) == 0 {
		t.Fatal("old series lost upcoming conflicts")
	}
	for _, window := range windows {
		if window.Start.Before(now.Add(-10 * time.Minute)) {
			t.Fatal("returned first 90 days instead of upcoming dates", window)
		}
	}
	query.Set("recurrence", "FREQ=DAILY;COUNT=2")
	if len(check()) != 0 {
		t.Fatal("moving check range reset COUNT")
	}
	future := now.AddDate(0, 0, 180)
	future = time.Date(future.Year(), future.Month(), future.Day(), 3, 5, 0, 0, time.UTC)
	query.Set("start_at", future.Format(time.RFC3339))
	query.Set("recurrence", "")
	if len(check()) != 1 {
		t.Fatal("distant one-time occurrence not checked")
	}
}
