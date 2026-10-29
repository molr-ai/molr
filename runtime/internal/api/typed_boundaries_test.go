package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/sandboxmanager"
)

func boundaryRequest(t *testing.T, f contractFixture, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := f.MutationRequest()
	request.Method = method
	request.URL, _ = url.Parse("https://molr.example" + path)
	request.Body = io.NopCloser(strings.NewReader(body))
	response := httptest.NewRecorder()
	f.Server.Handler().ServeHTTP(response, request)
	return response
}

func TestHTTPDecodesSessionRequestsBeforeInvokingServices(t *testing.T) {
	f := fixture(t)
	server := f.Server.(*HTTPServer)
	calls := 0
	var received any
	accept := func(q any) (json.RawMessage, error) {
		calls++
		received = q
		return json.RawMessage(`{"Accepted":true}`), nil
	}
	server.options.Browser = func(_ context.Context, _ core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
		if id != "assistant" {
			t.Fatalf("assistant scope: %s", id)
		}
		return accept(q)
	}
	server.options.ShellTasks = func(_ context.Context, _ core.Principal, id core.ID, q tools.ShellTaskRequest) (json.RawMessage, error) {
		return accept(q)
	}
	for _, tc := range []struct {
		method, path, body string
		want               any
	}{
		{"POST", "/api/assistants/assistant/browser", `{"action":"start","compute_id":"compute","full_access":true}`, tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: "compute", FullAccess: true}},
		{"POST", "/api/assistants/assistant/shell-tasks", `{"action":"write","task_id":"terminal","client_id":"window","generation":7,"sequence":8,"data":"hello\n"}`, tools.ShellTaskRequest{Action: "write", TaskID: "terminal", ClientID: "window", Generation: 7, Sequence: 8, Data: "hello\n"}},
		{"GET", "/api/assistants/assistant/browser", "", tools.BrowserRequest{Action: browser.Action{Action: "status"}}},
		{"GET", "/api/assistants/assistant/shell-tasks", "", tools.ShellTaskRequest{Action: "list"}},
	} {
		before := calls
		response := boundaryRequest(t, f, tc.method, tc.path, tc.body)
		if response.Code != http.StatusOK || calls != before+1 || !reflect.DeepEqual(received, tc.want) {
			t.Fatalf("%s: %d %s; received=%#v want=%#v", tc.path, response.Code, response.Body, received, tc.want)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/assistants/assistant/browser", `{"action":"status","unknown":true}`},
		{"POST", "/api/assistants/assistant/browser", `{"action":"status"} {}`},
		{"POST", "/api/assistants/assistant/shell-tasks", `{"action":"write","task_id":"terminal","rows":12}`},
		{"POST", "/api/assistants/assistant/shell-tasks", `{"action":"list","unknown":true}`},
	} {
		before := calls
		response := boundaryRequest(t, f, tc.method, tc.path, tc.body)
		if response.Code != http.StatusBadRequest || calls != before {
			t.Fatalf("invalid request reached service: %s: %d %s", tc.path, response.Code, response.Body)
		}
	}
}

func TestTypedReadModelsPreserveJSONShape(t *testing.T) {
	f := fixture(t)
	server := f.Server.(*HTTPServer)
	server.options.Accounts = f.Services.Auth.(*auth.Manager)
	server.accountRoutes()
	fail := false
	server.options.AccountQuotas = func(context.Context, core.Principal) (accountview.AccountQuotas, error) {
		if fail {
			return accountview.AccountQuotas{}, core.NewError(core.Forbidden, "denied")
		}
		return accountview.AccountQuotas{Resources: []accountview.ResourceUsage{}, Summary: accountview.UsageSummary{Spent: []core.Money{}}}, nil
	}
	server.options.AdminCatalog = func(context.Context, core.Principal) ([]accountview.CatalogItem, error) {
		return []accountview.CatalogItem{{ID: "model", Kind: "model", Name: "Fixture"}}, nil
	}
	server.options.AdminUsage = func(context.Context, core.Principal) (accountview.AdminUsage, error) {
		return accountview.AdminUsage{Users: []accountview.UsageSummary{}}, nil
	}
	server.options.UsageAnalytics = func(_ context.Context, _ core.Principal, q accountview.UsageQuery, _ bool) (accountview.AnalyticsView, error) {
		return accountview.AnalyticsView{From: q.From, Summary: accountview.AnalyticsTotals{ModelCalls: 2, Spent: []core.Money{{Currency: "USD", Micros: 12}}}, Rows: []accountview.AnalyticsRow{{ID: "model", AnalyticsTotals: accountview.AnalyticsTotals{ModelCalls: 2}}}, Daily: []accountview.AnalyticsDay{}}, nil
	}
	server.options.DiscoverProviders = func(context.Context, core.Principal) ([]resource.LocalProvider, error) { return nil, nil }
	server.options.SandboxInfo = func(context.Context, core.Principal) (sandboxmanager.RuntimeStatus, error) {
		return sandboxmanager.RuntimeStatus{Capabilities: platform.Capabilities{Reason: "sandbox_disabled"}}, nil
	}
	server.options.SkillsCatalog = func(context.Context, core.Principal) (agent.SkillsCatalog, error) {
		return agent.SkillsCatalog{Items: []agent.CatalogSkill{}, Assistants: []agent.SkillAssistant{}}, nil
	}
	server.options.Skill = func(context.Context, core.Principal, core.ID, string) (agent.SkillDocument, error) {
		return agent.SkillDocument{Text: "# Fixture", Version: "version"}, nil
	}
	for _, tc := range []struct {
		path   string
		fields map[string]string
		absent []string
	}{
		{"/api/account/quotas", map[string]string{"Resources": "[]", "Summary.Spent": "[]", "Summary.Reserved": "null"}, nil},
		{"/api/admin/catalog", map[string]string{"0.ID": `"model"`, "0.FullAccessAvailable": "false"}, []string{"0.GPUs", "0.GPUAccessMode", "0.Limits"}},
		{"/api/admin/usage", map[string]string{"Users": "[]"}, nil},
		{"/api/account/usage?from=2026-01-01", map[string]string{"From": `"2026-01-01"`, "Summary.ModelCalls": "2", "Rows.0.ModelCalls": "2", "Daily": "[]", "Options.Models": "null"}, []string{"Rows.0.AnalyticsTotals"}},
		{"/api/sandbox", map[string]string{"Networks": "null", "Directories": "null", "Capabilities.Reason": `"sandbox_disabled"`}, nil},
		{"/api/skills", map[string]string{"Items": "[]", "Assistants": "[]"}, nil},
		{"/api/skills?path=/builtin/SKILL.md", map[string]string{"Text": `"# Fixture"`, "Version": `"version"`}, []string{"Items", "Assistants"}},
	} {
		response := boundaryRequest(t, f, "GET", tc.path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body)
		}
		var body any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		value := func(path string) (any, bool) {
			current := body
			for _, part := range strings.Split(path, ".") {
				switch node := current.(type) {
				case map[string]any:
					var exists bool
					current, exists = node[part]
					if !exists {
						return nil, false
					}
				case []any:
					if part != "0" || len(node) == 0 {
						return nil, false
					}
					current = node[0]
				default:
					return nil, false
				}
			}
			return current, true
		}
		for path, want := range tc.fields {
			v, exists := value(path)
			raw, _ := json.Marshal(v)
			if !exists || string(raw) != want {
				t.Fatalf("%s field %s: %s want %s", tc.path, path, raw, want)
			}
		}
		for _, path := range tc.absent {
			if _, exists := value(path); exists {
				t.Fatalf("%s unexpectedly contains %s", tc.path, path)
			}
		}
	}
	response := boundaryRequest(t, f, "GET", "/api/resources/discover-local", "")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "null" {
		t.Fatalf("nil discovery: %d %s", response.Code, response.Body)
	}
	fail = true
	response = boundaryRequest(t, f, "GET", "/api/account/quotas", "")
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "Resources") {
		t.Fatalf("typed zero result masked error: %d %s", response.Code, response.Body)
	}
}
