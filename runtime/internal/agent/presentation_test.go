package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

func TestContextPresentationDropsToolsAndKeepsExplicitEmptyAuthorization(t *testing.T) {
	memory := contextEntry{ContextDocument: ContextDocument{Kind: "memory", Source: "memory/m", Scope: "owner", Revision: "3", Content: `{"Text":"<unchanged memory>"}`}, Version: "hash"}
	snapshot := &automaticContext{Sources: []contextEntry{
		{ContextDocument: ContextDocument{Kind: "tool", Source: "file_read", Content: `{"InputSchema":{"type":"object"},"Description":"duplicate-tool-description"}`}},
		{ContextDocument: ContextDocument{Kind: "permissions", Source: "permissions", Scope: "assistant", Revision: "private", Content: `{"Granted":[],"Total":0,"Truncated":false,"Revision":"private"}`}},
		{ContextDocument: ContextDocument{Kind: "permission_requests", Source: "permission_requests", Scope: "assistant", Content: `[]`}},
		{ContextDocument: ContextDocument{Kind: "project_instruction", Source: "/work/AGENTS.md", Scope: "/work", Content: "<instruction>\r\nUse exact paths."}}, memory,
	}, Removed: []contextReference{{Kind: "tool", Source: "obsolete"}, {Kind: "project_instruction", Source: "/work/old/AGENTS.md", Scope: "/work/old"}}}
	text := contextInput(snapshot).Text
	for _, bad := range []string{"duplicate-tool-description", "InputSchema", "obsolete", `"content":"{\"Granted`, `"kind":"permission_requests"`, `\u003cinstruction`} {
		if strings.Contains(text, bad) {
			t.Errorf("unexpected %s", bad)
		}
	}
	for _, want := range []string{`"Granted":[]`, "empty Granted list means none", "/work/old/AGENTS.md", "<instruction>"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s", want)
		}
	}
	original, _ := json.Marshal(memory)
	if !strings.Contains(text, string(original)) {
		t.Fatal("memory presentation changed")
	}
}

func TestRuntimeNotificationRolesPreserveContentAndRecoveryMetadata(t *testing.T) {
	inputs := []model.Input{
		{Role: model.System, Text: loopInstructions},
		{Role: model.System, RuntimeContext: true, Text: "Runtime context_changed: current permissions"},
		{Role: model.System, Text: prompt.Text("expedite", nil)},
		{Role: model.System, Text: `Runtime recovery: model operation "op" ended with state "unknown".`},
		{Role: model.System, Text: `Runtime reconciled earlier operation "op"; result is reference data.`},
		{Role: model.System, Text: `Runtime execution handoff: previous event "old"; current event "new".`},
		{Role: model.System, Text: `Runtime control event: {"Kind":"user_stopped"}`},
		{Role: model.System, Text: "Custom system instructions"},
		{Role: model.Tool, Text: `{"Text":"retained"}`, ReplyTo: "call", ToolOperationID: "op", ReadMessageIDs: []core.ID{"message"}},
	}
	before := contextHash(inputs)
	want := append([]model.Input(nil), inputs...)
	for i := 1; i <= 6; i++ {
		want[i].Role = model.User
	}
	out := runtimeNotificationRoles(inputs)
	if !reflect.DeepEqual(out, want) || contextHash(inputs) != before || !reflect.DeepEqual(runtimeNotificationRoles(out), out) {
		t.Fatal("role migration changed content/metadata, mutated original evidence, or is not idempotent")
	}
	// Same-event recovery and a new event built from an older checkpoint both
	// expose notifications as user inputs before model.start is recorded.
	progress, err := stateProgress(Checkpoint{}, privateContext{Inputs: inputs})
	if err != nil || !reflect.DeepEqual(progress.Inputs, want) {
		t.Fatal("resumed context retained system notifications", err)
	}
	f, _ := makeLoopFixture(t, nil)
	raw, _ := json.Marshal(privateContext{Inputs: inputs})
	turn := f.Turn
	turn.Checkpoint = &Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: turn.Event.AssistantID, FormatVersion: 1, Context: raw}
	built, err := f.Context.Build(t.Context(), turn)
	if err != nil || len(built) < len(want) || !reflect.DeepEqual(built[:len(want)], want) {
		t.Fatal("new event retained system notifications", err)
	}
}
func TestToolPresentationDoesNotMutateRecoveryEvidence(t *testing.T) {
	raw := `{"SandboxID":"local","Text":"skill content","OwnerID":"internal"}`
	inputs := []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "reused", Name: "file_read"}}}, {Role: model.Tool, ReplyTo: "reused", Text: raw, ToolOperationID: "op"}, {Role: model.Assistant, Calls: []model.ToolCall{{ID: "reused", Name: "file_search"}}}, {Role: model.Tool, ReplyTo: "reused", Text: raw}}
	out := modelInputs(inputs)
	if inputs[1].Text != raw || out[1].ToolOperationID != "op" || strings.Contains(out[1].Text, "OwnerID") || inputs[3].Text != raw || strings.Contains(out[3].Text, "OwnerID") {
		t.Fatal("projection corrupted evidence or leaked private file fields")
	}
	if contextHash(out) != contextHash(modelInputs(out)) {
		t.Fatal("request rendering is not idempotent")
	}
}
func TestTemplateMigrationReplacesLegacySnapshotWithoutSourceChange(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	exec := core.Execution{Principal: f.Turn.Principal, AssistantID: f.Turn.Event.AssistantID, OperationID: "refresh"}
	inputs, _, snapshot, err := e.refreshContext(t.Context(), exec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs[len(inputs)-1].Text = "Runtime context_changed: legacy internal serialized data"
	next, _, same, err := e.refreshContext(t.Context(), exec, inputs, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != snapshot.Version || strings.Contains(next[len(next)-1].Text, "legacy internal") {
		t.Fatal("unchanged old snapshot not re-rendered")
	}
}
func TestAllInstalledToolsHavePresentationTemplates(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	defs, err := e.options.Dependencies.Tools.Definitions(t.Context(), f.Turn.Principal)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range append(defs, tools.Definition{Name: "file_read"}, tools.Definition{Name: "shell_execute"}, tools.Definition{Name: "permission_list"}) {
		if strings.Contains(toolText(d.Name, `{}`), "unavailable") {
			t.Fatalf("missing template %s", d.Name)
		}
	}
}

func TestPluginHTTPResultsReachModelWithoutPrivateEnvelope(t *testing.T) {
	name := "plg_" + strings.Repeat("a", 48)
	cases := []struct{ raw, want string }{
		{`{"provider":"fixture","data":{"text":"A: fixture","items":[{"url":"https://example.com"}]},"usage":1,"duration_ms":2,"connection_id":"private-connection","OwnerID":"private-owner"}`, `"text":"A: fixture"`},
	}
	for _, c := range cases {
		out := toolText(name, c.raw)
		if !strings.Contains(out, c.want) || strings.Contains(out, "private-") {
			t.Fatal(out)
		}
		if toolText(name, out) != out {
			t.Fatal("plugin presentation is not idempotent", out)
		}
	}
}
func TestEventDoesNotExposeOwnershipAndEmptyMetadata(t *testing.T) {
	text := eventText(event.AssistantEvent{ID: "event", Kind: event.UserMessage, OwnerID: "private-owner", MessageID: "message"}, "chat")
	for _, bad := range []string{"private-owner", "OwnerID", "AssistantID", "0001-01-01"} {
		if strings.Contains(text, bad) {
			t.Fatal(text)
		}
	}
	if !strings.Contains(text, `"MessageID":"message"`) {
		t.Fatal(text)
	}
}

func TestLegacyProjectionDoesNotRewriteCustomInstructions(t *testing.T) {
	raw := "You are an independent assistant. Custom constraints must survive."
	inputs := []model.Input{{Role: model.System, Text: raw}, {Role: model.System, Text: loopInstructions}, {Role: model.User, Text: raw}}
	out := modelInputs(inputs)
	if out[0].Text != raw || out[1].Text != loopInstructions || out[2].Text != raw || contextHash(out) != contextHash(modelInputs(out)) {
		t.Fatal("rewrote custom instructions")
	}
}

func TestTriggeringEventIsAUserTurnAndLegacyProjectionKeepsEvidence(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	inputs, err := f.Context.Build(t.Context(), f.Turn)
	if err != nil {
		t.Fatal(err)
	}
	current := inputs[len(inputs)-1]
	if current.Role != model.User || !strings.HasPrefix(current.Text, "Runtime event: ") {
		t.Fatalf("event is not a query: %+v", current)
	}
	legacy := current
	legacy.Role = model.System
	original := []model.Input{{Role: model.System, Text: loopInstructions}, legacy}
	projected := modelInputs(original)
	if projected[1].Role != model.User || projected[1].Text != current.Text || original[1].Role != model.System {
		t.Fatal("legacy projection lost event content or mutated checkpoint")
	}
	if contextHash(projected) != contextHash(modelInputs(projected)) {
		t.Fatal("legacy event migration is not idempotent")
	}
	custom := []model.Input{{Role: model.System, Text: `Runtime event: {"custom":"not a runtime event"}`}}
	if contextHash(custom) != contextHash(modelInputs(custom)) {
		t.Fatal("rewrote an unrecognized system message")
	}
}

func TestProfileContextAndResultPresentation(t *testing.T) {
	snapshot := &automaticContext{Sources: []contextEntry{{ContextDocument: ContextDocument{Kind: "profile", Source: "profile", Scope: "assistant", Content: `{"Name":"小满","Avatar":{"Type":"preset","ID":"cat"},"Version":3,"OwnerID":"private-owner"}`}}}}
	text := contextInput(snapshot).Text
	if !strings.Contains(text, `"Name":"小满"`) || !strings.Contains(text, `"Version":3`) || strings.Contains(text, "private-owner") {
		t.Fatal(text)
	}
	raw := `{"Changed":false,"Profile":{"Name":"小满","Avatar":{"Type":"initial"},"Version":3},"Budget":"private-budget"}`
	text, err := prompt.Structured("profile_update", raw)
	if err != nil || !strings.Contains(text, `"Changed":false`) || strings.Contains(text, "private-budget") {
		t.Fatal(text, err)
	}
	if err = validateContextPresentation(ContextDocument{Kind: "profile", Content: `{broken`}); err == nil {
		t.Fatal("malformed profile accepted")
	}
}
