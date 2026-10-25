package app

import (
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/modelcatalog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestWorkspaceContextDiscoversScopesAndRefreshesLoadedSkills(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		need(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700))
		need(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0600))
	}
	write("AGENTS.md", "root rules")
	write("src/AGENTS.md", "src rules")
	write("node_modules/pkg/AGENTS.md", "must not inject dependency")
	write(".agents/skills/review/SKILL.md", "---\nname: review\ndescription: Review code\n---\nprivate skill workflow v1")
	docs, err := workspaceContext(t.Context(), root, directoryVisit("/work/src"))
	need(t, err)
	if len(docs) != 3 {
		t.Fatalf("sources=%+v", docs)
	}
	var skill agent.ContextDocument
	for _, d := range docs {
		if d.Kind == "skill" {
			skill = d
			if d.Loaded || strings.Contains(d.Content, "private skill workflow") || !strings.Contains(d.Content, "Review code") {
				t.Fatal("skill discovery loaded body or lost metadata")
			}
		}
		if d.Source == "/work/src/AGENTS.md" && d.Scope != "/work/src" {
			t.Fatal("nested instruction scope lost")
		}
	}
	args, _ := json.Marshal(map[string]any{"path": "/work/.agents/skills/review/SKILL.md"})
	request := agent.ContextRequest{Previous: docs, Inputs: []model.Input{
		{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: "file_read", Arguments: args}}},
		{Role: model.Tool, ReplyTo: "read", Text: `{"Text":"private skill workflow v1"}`},
	}}
	loaded, err := workspaceContext(t.Context(), root, request)
	need(t, err)
	for _, d := range loaded {
		if d.Kind == "skill" && (!d.Loaded || !strings.Contains(d.Content, "workflow v1")) {
			t.Fatal("read skill not activated")
		}
	}
	write(".agents/skills/review/SKILL.md", "---\nname: review\n---\nprivate skill workflow v2")
	changed, err := workspaceContext(t.Context(), root, agent.ContextRequest{Previous: loaded})
	need(t, err)
	for _, d := range changed {
		if d.Kind == "skill" && (!d.Loaded || !strings.Contains(d.Content, "workflow v2") || skill.Revision == d.Revision) {
			t.Fatal("loaded skill did not refresh after restart")
		}
	}
	need(t, os.Remove(filepath.Join(root, ".agents/skills/review/SKILL.md")))
	need(t, os.Remove(filepath.Join(root, "src/AGENTS.md")))
	deleted, err := workspaceContext(t.Context(), root, agent.ContextRequest{Previous: changed})
	need(t, err)
	if len(deleted) != 1 || deleted[0].Source != "/work/AGENTS.md" {
		t.Fatal("deleted sources survived snapshot")
	}
}
func TestWorkspaceContextRejectsUnsafeOrOversizedSources(t *testing.T) {
	for _, name := range []string{"symlink", "hardlink", "oversize", "binary"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "AGENTS.md")
			switch name {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "secret")
				need(t, os.WriteFile(outside, []byte("private"), 0600))
				need(t, os.Symlink(outside, source))
			case "hardlink":
				outside := filepath.Join(t.TempDir(), "secret")
				need(t, os.WriteFile(outside, []byte("private"), 0600))
				need(t, os.Link(outside, source))
			case "oversize":
				need(t, os.WriteFile(source, []byte(strings.Repeat("x", (64<<10)+1)), 0600))
			case "binary":
				need(t, os.WriteFile(source, []byte{0, 1, 2}, 0600))
			}
			if _, err := workspaceContext(t.Context(), root, agent.ContextRequest{}); err == nil {
				t.Fatal("unsafe or partial context accepted")
			}
		})
	}
}
func TestWorkspaceContextAuthorizesIdentityAndSkillReads(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	need(t, os.MkdirAll(filepath.Join(root, ".agents/skills/check"), 0700))
	need(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("private rules"), 0600))
	need(t, os.WriteFile(filepath.Join(root, ".agents/skills/check/SKILL.md"), []byte("secret workflow"), 0600))
	fs := newResourceFileTools(resourceFileOptions{adapters: f.ad})
	sources, err := fs.contextSources(t.Context(), agent.ContextRequest{Execution: f.execution})
	need(t, err)
	if len(sources) != 5 {
		t.Fatal("composition did not read workspace context")
	}
	forged := f.execution
	forged.AssistantID = core.ID("other")
	if _, err = fs.contextSources(t.Context(), agent.ContextRequest{Execution: forged}); err == nil {
		t.Fatal("forged execution read sources")
	}
	for _, reply := range []bool{false, true} {
		args := json.RawMessage(`{"resource_id":"external","path":"/work/.agents/skills/check/SKILL.md"}`)
		if !reply {
			args = json.RawMessage(`{"path":"/work/.agents/skills/check/SKILL.md"}`)
		}
		inputs := []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: "file_read", Arguments: args}}}}
		if reply {
			inputs = append(inputs, model.Input{Role: model.Tool, ReplyTo: "read", Text: "external workflow"})
		}
		sources, err = workspaceContext(t.Context(), root, agent.ContextRequest{Inputs: inputs})
		need(t, err)
		for _, d := range sources {
			if d.Kind == "skill" && d.Loaded {
				t.Fatal("uncompleted or external read activated workspace skill")
			}
		}
	}
	skills, err := workspaceSkills(t.Context(), root)
	need(t, err)
	if len(skills) != 1 || skills[0].ID != "/work/.agents/skills/check/SKILL.md" || !skills[0].Enabled {
		t.Fatal("workspace skill capability not available")
	}
}

func TestLoopLoadsAndUpdatesWorkspaceContextThroughRealTools(t *testing.T) {
	cfg := testConfig(t)
	cfg.Sandbox.Enabled = true // Storage stays available even without a ready compute backend.
	cfg.Debug.Enabled = true
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	need(t, showWorkbench(t, b, p, true))
	pref, err := b.Services.Settings.Get(t.Context(), p)
	need(t, err)
	pref.Timezone, pref.Language, pref.Currency = "Asia/Shanghai", "zh", "CNY"
	pref, err = b.Services.Settings.Update(t.Context(), p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: "initial-zone", ExpectedVersion: pref.Version}, Settings: pref})
	need(t, err)
	var requests []struct {
		Messages []map[string]any `json:"messages"`
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []map[string]any `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		requests = append(requests, request)
		step := len(requests)
		if step == 1 {
			pref.Timezone, pref.Language, pref.Currency = "America/Los_Angeles", "en", "USD"
			updated, updateErr := b.Services.Settings.Update(r.Context(), p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: "changed-zone", ExpectedVersion: pref.Version}, Settings: pref})
			if updateErr != nil {
				t.Error(updateErr)
				http.Error(w, "settings update failed", 500)
				return
			}
			pref = updated
		}
		if step == 4 {
			pref.Theme = settings.Dark
			_, updateErr := b.Services.Settings.Update(r.Context(), p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: "unrelated-theme", ExpectedVersion: pref.Version}, Settings: pref})
			if updateErr != nil {
				t.Error(updateErr)
				http.Error(w, "settings update failed", 500)
				return
			}
		}
		msg := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		if step <= 4 {
			name, args := "file_read", `{"path":"/work/.agents/skills/check/SKILL.md"}`
			if step == 1 {
				args = `{"path":"/work/.agents/skills/check/SKILL.md","offset":-1}`
			}
			if step == 3 {
				name, args = "file_write", `{"path":"/work/AGENTS.md","text":"updated project rule","overwrite":true}`
			}
			msg["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("call-%d", step), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}
			finish = "tool_calls"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "context-test", "choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 5}})
	}))
	defer provider.Close()
	binding := resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{{Name: "test", SupportsTools: true, ContextLimit: 65536, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Name: "provider", Kind: resource.Model, Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(t.Context(), p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "context test", ModelID: modelcatalog.Entries(r, binding)[0].ID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	cancelPendingBirth(t, b, a)
	root := platform.AssistantWorkspace(cfg.DataDir, "", p.OwnerID, a.ID)
	need(t, os.MkdirAll(filepath.Join(root, ".agents/skills/check"), 0700))
	need(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original project rule"), 0600))
	need(t, os.WriteFile(filepath.Join(root, ".agents/skills/check/SKILL.md"), []byte("---\nname: check\n---\nloaded workflow body"), 0600))
	_, err = b.Services.Conversations.SendUser(t.Context(), p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "message"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "exercise context"}})
	need(t, err)
	pending, err := conversation.NewOutbox(b.store).Pending(t.Context(), 100)
	need(t, err)
	consumer := scheduler.NewMessageConsumer(b.store, scheduler.Options{Conversations: conversation.NewRepository(b.store)})
	for _, message := range pending {
		need(t, consumer.AcceptMessage(t.Context(), message))
	}
	claim, err := scheduler.NewQueue(b.store).Claim(t.Context(), time.Now())
	need(t, err)
	_, err = b.Services.Agent.(agent.Runtime).Handle(t.Context(), agent.Turn{Principal: claim.Principal, Event: claim.Job.Event})
	need(t, err)
	if len(requests) != 6 {
		t.Fatalf("requests=%d", len(requests))
	}
	var stableContext string
	for step, request := range requests {
		count := 0
		for _, message := range request.Messages {
			text, _ := message["content"].(string)
			if !strings.HasPrefix(text, "Runtime context_changed: ") {
				continue
			}
			count++
			var snapshot struct {
				Version string `json:"version"`
				Sources []struct {
					Kind    string
					Content json.RawMessage
				} `json:"sources"`
			}
			need(t, json.Unmarshal([]byte(strings.SplitN(strings.TrimPrefix(text, "Runtime context_changed: "), "\n", 2)[0]), &snapshot))
			found := false
			workspaceFound := false
			for _, source := range snapshot.Sources {
				if source.Kind == "workspace" {
					workspaceFound = true
					var workspace workspaceDescription
					need(t, json.Unmarshal(source.Content, &workspace))
					if !workspace.Available || workspace.Path != "/work" || len(workspace.Directories) != 8 {
						t.Fatalf("invalid workspace context: %s", source.Content)
					}
				}
				if source.Kind == "time_context" || strings.Contains(string(source.Content), "current_time") {
					t.Fatal("volatile time source survived")
				}
				if source.Kind != "user_preferences" {
					continue
				}
				found = true
				var value userPreferences
				need(t, json.Unmarshal(source.Content, &value))
				zone, language, currency := "Asia/Shanghai", "zh", "CNY"
				if step > 0 {
					zone, language, currency = "America/Los_Angeles", "en", "USD"
				}
				if value.Timezone != zone || value.Language != language || value.Currency != currency {
					t.Fatalf("request %d preferences = %+v", step, value)
				}
			}
			if !found || !workspaceFound {
				t.Fatal("provider request missing user preferences or workspace")
			}
			if step == 3 {
				stableContext = text
			}
			if step == 4 && text != stableContext {
				t.Fatal("unchanged preferences or unrelated settings edit changed snapshot")
			}
			if (step > 1) != strings.Contains(text, "loaded workflow body") {
				t.Fatal("failed read activated skill, or successful read did not activate skill")
			}
			if step >= 3 && (!strings.Contains(text, "updated project rule") || strings.Contains(text, "original project rule")) {
				t.Fatal("real file_write failed to replace project instruction")
			}
		}
		if count != 1 {
			t.Fatal("wire request has duplicate or missing context notifications")
		}
	}
	runs, err := b.Trace.Runs(t.Context(), p, debugtrace.Query{AssistantID: a.ID})
	need(t, err)
	run, err := b.Trace.Get(t.Context(), p, runs.Items[0].ID)
	need(t, err)
	changes := 0
	for _, step := range run.Steps {
		if step.Kind == "context.changed" {
			changes++
		}
	}
	if changes != 4 {
		t.Fatalf("context changes in trace=%d", changes)
	}
}

func TestGlobalContextDiscoveryAndExplicitSkillRead(t *testing.T) {
	root := t.TempDir()
	need(t, os.MkdirAll(filepath.Join(root, ".agents/skills/shared"), 0700))
	need(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Shared instructions"), 0600))
	need(t, os.WriteFile(filepath.Join(root, ".agents/skills/shared/SKILL.md"), []byte("---\nname: shared\n---\nShared workflow"), 0600))
	docs, err := scanRootContext(t.Context(), root, agent.ContextRequest{}, ".", "/global")
	need(t, err)
	if len(docs) != 2 {
		t.Fatal(docs)
	}
	request := agent.ContextRequest{Inputs: []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: "file_read", Arguments: json.RawMessage(`{"path":"/global/.agents/skills/shared/SKILL.md"}`)}}}, {Role: model.Tool, ReplyTo: "read", Text: `{"Text":"Shared workflow","SandboxID":"local"}`}}}
	docs, err = scanRootContext(t.Context(), root, request, ".", "/global")
	need(t, err)
	for _, d := range docs {
		if !strings.HasPrefix(d.Source, "/global/") {
			t.Fatal("wrong global source", d)
		}
		if d.Kind == "skill" && (!d.Loaded || !strings.Contains(d.Content, "Shared workflow")) {
			t.Fatal("skill not loaded", d)
		}
	}
	request.Inputs[1].Text = `{"Text":"remote workflow","SandboxID":"remote"}`
	docs, err = scanRootContext(t.Context(), root, request, ".", "/global")
	need(t, err)
	for _, d := range docs {
		if d.Kind == "skill" && d.Loaded {
			t.Fatal("remote reply activated local shared skill")
		}
	}
}

func directoryVisit(name string) agent.ContextRequest {
	args, _ := json.Marshal(map[string]string{"path": name})
	result, _ := json.Marshal(map[string]string{"Path": name, "SandboxID": "local"})
	return agent.ContextRequest{Inputs: []model.Input{
		{Role: model.Assistant, Calls: []model.ToolCall{{ID: "visit", Name: "file_list", Arguments: args}}},
		{Role: model.Tool, ReplyTo: "visit", Text: string(result)},
	}}
}

func TestInstructionDiscoveryIgnoresBulkDataAndLoadsVisitedAncestors(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		need(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700))
		need(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0600))
	}
	write("AGENTS.md", "workspace rules")
	for _, name := range []string{"inbox", "outputs", ".local/state/molr", ".cache", "projects/large/node_modules/pkg"} {
		write(name+"/AGENTS.md", "untrusted data")
	}
	write("projects/AGENTS.md", "project defaults")
	write("projects/large/AGENTS.md", "large project")
	write("projects/large/src/AGENTS.md", "src overrides")
	write("projects/unrelated/AGENTS.md", "unrelated instructions")
	for i := 0; i < 4200; i++ {
		write(fmt.Sprintf("projects/large/data/%d.txt", i), "data")
	}
	for i := 0; i < 4200; i++ {
		write(fmt.Sprintf(".local/state/molr/%d.txt", i), "output")
	}
	docs, err := workspaceContext(t.Context(), root, agent.ContextRequest{})
	need(t, err)
	if len(docs) != 1 || docs[0].Content != "workspace rules" {
		t.Fatalf("initial context=%+v", docs)
	}
	visited, err := workspaceContext(t.Context(), root, directoryVisit("/work/projects/large/src"))
	need(t, err)
	if len(visited) != 4 {
		t.Fatalf("visited sources=%+v", visited)
	}
	archived := directoryVisit("/work/projects/large/src")
	archived.Inputs[1].Text = `{"full_result_path":"/work/.local/state/molr/tool-result-example.json","details":{"Path":"/work/projects/large/src","SandboxID":"local"}}`
	archivedDocs, err := workspaceContext(t.Context(), root, archived)
	need(t, err)
	if len(archivedDocs) != 4 {
		t.Fatal("archived listing lost instruction scope")
	}

	write("projects/large/src/AGENTS.md", "new src overrides")
	restored, err := workspaceContext(t.Context(), root, agent.ContextRequest{Previous: visited})
	need(t, err)
	if len(restored) != 4 {
		t.Fatal("lost scope after restore")
	}
	for _, d := range restored {
		if d.Scope == "/work/projects/large/src" && d.Content != "new src overrides" {
			t.Fatal("stale scoped instruction")
		}
	}
	for _, name := range []string{"/work/inbox", "/work/outputs", "/work/.local/state/molr", "/work/.cache", "/work/projects/large/node_modules/pkg"} {
		docs, err = workspaceContext(t.Context(), root, directoryVisit(name))
		need(t, err)
		if len(docs) != 1 {
			t.Fatalf("data directory activated instructions: %s %+v", name, docs)
		}
	}
}

func TestInstructionDiscoveryRequiresSuccessfulLocalWorkspaceRead(t *testing.T) {
	root := t.TempDir()
	need(t, os.MkdirAll(filepath.Join(root, "project"), 0700))
	need(t, os.WriteFile(filepath.Join(root, "project/AGENTS.md"), []byte("local only"), 0600))
	for _, variant := range []string{"remote", "external", "failed", "uncompleted", "full-access"} {
		t.Run(variant, func(t *testing.T) {
			request := directoryVisit("/work/project")
			switch variant {
			case "remote":
				request.Inputs[1].Text = `{"Path":"/work/project","SandboxID":"remote"}`
			case "external":
				request.Inputs[0].Calls[0].Arguments = json.RawMessage(`{"path":"/work/project","resource_id":"external"}`)
			case "failed":
				request.Inputs[1].Text = `{"ok":false,"error":{"message":"failed"},"Path":"/work/project"}`
			case "uncompleted":
				request.Inputs = request.Inputs[:1]
			case "full-access":
				request.Inputs[0].Calls[0].Arguments = json.RawMessage(`{"path":"/work/project","full_access":true}`)
			}
			docs, err := workspaceContext(t.Context(), root, request)
			need(t, err)
			if len(docs) != 0 {
				t.Fatalf("unexpected sources: %+v", docs)
			}
		})
	}
}
