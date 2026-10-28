package api

import (
	"bytes"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/core"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestPlanReadRoutesSeparateSummariesHistoryAndExecutionBodies(t *testing.T) {
	f := fixture(t)
	request := func(path string, body any, status int) []byte {
		t.Helper()
		r := f.ReadRequest()
		if body != nil {
			r = f.MutationRequest()
			raw, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			r.ContentLength = int64(len(raw))
		}
		u, err := url.Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		r.URL.Path = u.Path
		r.URL.RawQuery = u.RawQuery
		w := httptest.NewRecorder()
		f.Server.Handler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: status %d body %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	raw := request("/api/schedules", map[string]any{"Mutation": core.Mutation{IdempotencyKey: "series"}, "Title": "Series fixture", "Kind": "personal_schedule", "Instructions": "source-description-only", "Trigger": schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=5", MissedPolicy: schedule.Skip}}, 200)
	var source schedule.Schedule
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	ref := core.Object{Kind: "schedule", ID: source.ID}
	raw = request("/api/plans/execute", plan.ExecuteRequest{Mutation: core.Mutation{IdempotencyKey: "complete"}, Ref: ref, Action: "complete", OccurrenceAt: &at, Result: "execution-result-only"}, 200)
	var run plan.Execution
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	path := "/api/plans/schedule/" + string(source.ID)
	for _, endpoint := range []string{"/api/plans", "/api/plans?from=" + url.QueryEscape(at.Format(time.RFC3339)) + "&to=" + url.QueryEscape(at.AddDate(0, 0, 7).Format(time.RFC3339)), "/api/plans/events?from=" + url.QueryEscape(at.Format(time.RFC3339))} {
		raw = request(endpoint, nil, 200)
		if bytes.Contains(raw, []byte("source-description-only")) || bytes.Contains(raw, []byte("execution-result-only")) {
			t.Fatal("summary exposed body", endpoint)
		}
		if len(raw) > 10000 {
			t.Fatal("unexpected summary size", len(raw))
		}
		if bytes.Contains([]byte(endpoint), []byte("/events?")) {
			var page struct {
				Items []struct{ PlanKey string }
				Plans map[string]plan.Summary
			}
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 5 || len(page.Plans) != 1 {
				t.Fatal("events not normalized")
			}
			for _, item := range page.Items {
				if page.Plans[item.PlanKey].Ref != ref {
					t.Fatal("broken plan reference")
				}
			}
		}
	}
	raw = request(path+"?at="+url.QueryEscape(at.Format(time.RFC3339)), nil, 200)
	if !bytes.Contains(raw, []byte("source-description-only")) || bytes.Contains(raw, []byte("execution-result-only")) {
		t.Fatal("wrong detail projection")
	}
	raw = request(path+"/history?limit=1", nil, 200)
	var history core.Page[plan.ExecutionSummary]
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || history.Items[0].ID != run.ID || bytes.Contains(raw, []byte("execution-result-only")) {
		t.Fatal("history projection")
	}
	raw = request(path+"/executions/"+string(run.ID), nil, 200)
	if !bytes.Contains(raw, []byte("execution-result-only")) {
		t.Fatal("missing execution body")
	}
	request("/api/plans/selection?plan_id="+string(source.ID), nil, 200)
	request(path+"/history?limit=201", nil, 400)
	request(path+"/history?cursor=broken", nil, 400)
	request(path+"?at=broken", nil, 400)
	request("/api/plans/other/"+string(source.ID), nil, 400)
	request(path+"/executions/missing", nil, 404)
	r := f.ReadRequest()
	r.URL.Path = path + "/history"
	f.StripCredentials(r)
	w := httptest.NewRecorder()
	f.Server.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("anonymous history", w.Code)
	}
}
