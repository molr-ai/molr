package plugins

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, core.Principal, Package) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, Options{ComputeAvailable: func(context.Context, core.Principal, core.ID) (bool, error) { return true, nil }, Authorize: func(context.Context, core.Principal, bool) error { return nil }, CheckAssistant: func(context.Context, core.Principal, core.ID) error { return nil }, CheckCompute: func(context.Context, core.Principal, core.ID) error { return nil }, ReadPlans: func(context.Context, core.Principal) (any, error) { return []string{"plan"}, nil }})
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	pkg := Package{Manifest: Manifest{ManifestVersion: 1, PluginAPI: 2, ID: "demo.test", Name: "Test", Version: "1.0.0", Contributes: Contributions{Pages: []Page{{ID: "board", Title: "Board", Entry: "ui.html", Capabilities: []string{"plans.read"}}}, Tools: []Tool{{ID: "report", Description: "Report", Entry: "report.sh", Execution: ToolExecution{Type: "shell"}, Capabilities: []string{"compute.execute", "workspace.write"}, InputSchema: Schema{Type: "object", Properties: map[string]Property{"label": {Type: "string"}}, Required: []string{"label"}}}}, Skills: []Skill{{ID: "review", Name: "Review", Entry: "skills/SKILL.md", Tools: []string{"report"}}}}}, Files: map[string]string{"ui.html": "<p>test</p>", "report.sh": "printf '%s' \"$1\"", "skills/SKILL.md": "Review this report."}}
	return s, p, pkg
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func activate(t *testing.T, s *Service, p core.Principal, v Installation) Installation {
	t.Helper()
	out, err := s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true, Grants: Requested(v.Manifest), Bindings: []Binding{{AssistantID: "assistant", ComputeID: "compute", Tools: []string{"report"}, Skills: []string{"review"}}}})
	must(t, err)
	return out
}
func execution() core.Execution {
	return core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant", OperationID: "operation"}
}
func TestInstallIsolationImmutableUpgradeAndRemoval(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	list, err := s.List(t.Context(), p)
	must(t, err)
	if len(list) != 1 || list[0].Enabled {
		t.Fatal(list)
	}
	other := p
	other.OwnerID = "other"
	other.SubjectID = "other"
	list, err = s.List(t.Context(), other)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("owner leak")
	}
	v = activate(t, s, p, v)
	q := UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "board", Method: "plans.list"}
	_, err = s.UI(t.Context(), p, q)
	must(t, err)
	pkg.Files["ui.html"] = "mutated"
	if _, err = s.Install(t.Context(), p, pkg); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	pkg.Manifest.Version = "2.0.0"
	staged, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	if staged.Digest != v.Digest || staged.Manifest.Version != "1.0.0" {
		t.Fatal("installation switched active release")
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("lost update", err)
	}
	updated, err := s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: staged.Version, Enabled: true, Release: "2.0.0", Grants: []string{"plans.read"}})
	must(t, err)
	if _, err = s.UI(t.Context(), p, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("old UI accepted", err)
	}
	must(t, s.Remove(t.Context(), p, updated.ID, updated.Version))
	list, err = s.List(t.Context(), p)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("uninstall not persisted")
	}
	reinstalled, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	if reinstalled.Enabled || len(reinstalled.Grants) > 0 || len(reinstalled.Bindings) > 0 {
		t.Fatal("reinstall restored grants")
	}
}
func TestToolBindingsSkillActivationAndRevocation(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	e := execution()
	defs, err := s.Definitions(t.Context(), e.Principal, false)
	must(t, err)
	if len(defs) != 2 {
		t.Fatal(defs)
	}
	wrong := e.Principal
	wrong.SubjectID = "different"
	defs, err = s.Definitions(t.Context(), wrong, false)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal("unbound assistant gained tools")
	}
	call := tools.Call{Execution: e, Name: SkillName(v, "review"), Arguments: json.RawMessage(`{}`)}
	raw, err := s.Execute(t.Context(), call)
	must(t, err)
	docs, err := s.Context(t.Context(), agent.ContextRequest{Execution: e})
	must(t, err)
	if len(docs) != 1 || docs[0].Loaded || strings.Contains(docs[0].Content, "Review this report.") {
		t.Fatal("eager Skill body")
	}
	req := agent.ContextRequest{Execution: e, Inputs: []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: call.Name, Arguments: call.Arguments}}}, {Role: model.Tool, ReplyTo: "read", Text: string(raw)}}}
	docs, err = s.Context(t.Context(), req)
	must(t, err)
	if len(docs) != 1 || !docs[0].Loaded || !strings.Contains(docs[0].Content, ToolName(v, "report")) {
		t.Fatal(docs)
	}
	started := make(chan string, 1)
	s.options.Shell = func(ctx context.Context, _ core.Execution, script string, _ core.ID) (json.RawMessage, error) {
		started <- script
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Execute(t.Context(), tools.Call{Execution: e, Name: ToolName(v, "report"), Arguments: json.RawMessage(`{"label":"'; touch /work/escaped; #"}`)})
		done <- err
	}()
	select {
	case script := <-started:
		if !strings.Contains(script, `'"'"'`) {
			t.Fatal("arguments not shell quoted")
		}
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false})
	must(t, err)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("execution was not canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel")
	}
	if err = s.ValidateCall(t.Context(), call); core.CodeOf(err) != core.Forbidden {
		t.Fatal("disabled skill accepted", err)
	}
	docs, err = s.Context(t.Context(), req)
	must(t, err)
	if len(docs) != 0 {
		t.Fatal("revoked context retained")
	}
}
func TestManifestAndBridgeFailClosed(t *testing.T) {
	s, p, pkg := fixture(t)
	bad := pkg
	bad.Manifest.PluginAPI = 1
	if bad.Validate() == nil {
		t.Fatal("unknown API")
	}
	pkg.Files["../escape"] = "bad"
	if pkg.Validate() == nil {
		t.Fatal("path traversal")
	}
	delete(pkg.Files, "../escape")
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true})
	must(t, err)
	for _, method := range []string{"document", "plans.list", "auth.sessions"} {
		if _, err = s.UI(t.Context(), p, UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "board", Method: method}); core.CodeOf(err) != core.Forbidden {
			t.Fatal(method, err)
		}
	}
	if err = pkg.Manifest.Contributes.Tools[0].InputSchema.Check(json.RawMessage(`{"label":"fine","compute_id":"host"}`)); err == nil {
		t.Fatal("unknown tool parameter")
	}
}

func TestComputeRevocationRemovesToolsAndDependentSkills(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	s.options.ComputeAvailable = func(context.Context, core.Principal, core.ID) (bool, error) { return false, nil }
	defs, err := s.Definitions(t.Context(), execution().Principal, false)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal("revoked compute remained in tool directory")
	}
	docs, err := s.Context(t.Context(), agent.ContextRequest{Execution: execution()})
	must(t, err)
	if len(docs) != 0 {
		t.Fatal("dependent Skill remained available")
	}
	ownerTools, err := s.Definitions(t.Context(), p, true)
	must(t, err)
	if len(ownerTools) != 2 {
		t.Fatal("owner catalog lost installed metadata")
	}
	capabilities := agent.Capabilities{AssistantID: "assistant"}
	for _, definition := range ownerTools {
		capabilities.Tools = append(capabilities.Tools, agent.ToolCapability{Definition: definition, Available: true})
	}
	must(t, s.Inspect(t.Context(), p, "assistant", &capabilities))
	for _, capability := range capabilities.Tools {
		if capability.Available {
			t.Fatal("inspection exposed revoked compute", capability.Name)
		}
	}
	if len(capabilities.Skills.Items) != 1 || capabilities.Skills.Items[0].Enabled {
		t.Fatal("inspection exposed dependent skill", capabilities.Skills)
	}
	_, err = s.Install(t.Context(), execution().Principal, pkg)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant installed package", err)
	}
	if err = s.Remove(t.Context(), core.Principal{}, v.ID, v.Version); core.CodeOf(err) != core.Unauthenticated {
		t.Fatal("anonymous removed package", err)
	}
}
