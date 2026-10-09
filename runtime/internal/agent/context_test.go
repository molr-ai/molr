package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type changingTools struct {
	tools.Executor
	phase *atomic.Int64
}

func (t changingTools) Definitions(ctx context.Context, p core.Principal) ([]tools.Definition, error) {
	defs, err := t.Executor.Definitions(ctx, p)
	if t.phase.Load() > 0 {
		out := []tools.Definition{}
		for _, d := range defs {
			if d.Name != "conversation_publish" {
				out = append(out, d)
			}
		}
		return out, err
	}
	return defs, err
}
func snapshotFrom(t *testing.T, inputs []model.Input) automaticContext {
	t.Helper()
	count := 0
	var snapshot automaticContext
	for _, input := range inputs {
		if !input.RuntimeContext {
			continue
		}
		count++
		if input.Role != model.User {
			t.Fatal("runtime notification must be a user input")
		}
		data := strings.SplitN(strings.TrimPrefix(input.Text, "Runtime context_changed: "), "\n", 2)[0]
		var view struct {
			Sources []struct {
				Kind, Source, Scope string
				Content             json.RawMessage
			}
			Removed []contextReference
		}
		testkit.RequireNoError(t, json.Unmarshal([]byte(data), &view))
		snapshot.Version = contextHash(input.Text)
		snapshot.Removed = view.Removed
		for _, source := range view.Sources {
			snapshot.Sources = append(snapshot.Sources, contextEntry{ContextDocument: ContextDocument{Kind: source.Kind, Source: source.Source, Scope: source.Scope, Content: string(source.Content)}})
		}
	}
	if count != 1 {
		t.Fatalf("want one current snapshot, got %d", count)
	}
	return snapshot
}
func TestContextChangesRefreshWithinTurnAndAcrossRestart(t *testing.T) {
	var phase atomic.Int64
	var calls atomic.Int64
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			phase.Store(1)
			_, _ = w.Write([]byte(`{"id":"completion","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"read-` + string(rune('0'+n)) + `","type":"function","function":{"name":"conversation_read","arguments":"{\"conversation_id\":\"chat\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
		} else {
			_, _ = w.Write([]byte(`{"id":"completion","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
		}
	}))
	engine := f.Runtime.(*Engine)
	engine.options.Dependencies.Tools = changingTools{engine.options.Dependencies.Tools, &phase}
	engine.options.ContextSources = func(ctx context.Context, r ContextRequest) ([]ContextDocument, error) {
		if phase.Load() >= 2 {
			return nil, nil
		}
		text := "old project instructions"
		if phase.Load() == 1 {
			text = "new project instructions"
		}
		return []ContextDocument{{Kind: "project_instruction", Source: "/work/AGENTS.md", Scope: "/work", Content: text}}, nil
	}
	out, err := engine.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	requests := f.ModelRequests()
	if len(requests) != 4 {
		t.Fatalf("requests: %d", len(requests))
	}
	first := snapshotFrom(t, requests[0].Inputs)
	second := snapshotFrom(t, requests[1].Inputs)
	third := snapshotFrom(t, requests[2].Inputs)
	if first.Version == second.Version || second.Version != third.Version {
		t.Fatal("changes missed or unchanged context reinjected")
	}
	if len(requests[0].Tools) != 2 || len(requests[1].Tools) != 1 || requests[1].Tools[0].Name != "conversation_read" {
		t.Fatal("tool removal did not reach next model request")
	}
	if len(second.Removed) != 0 {
		t.Fatal("tool metadata leaked into model snapshot")
	}
	for _, input := range requests[1].Inputs {
		if strings.Contains(input.Text, "old project instructions") {
			t.Fatal("stale injected body retained")
		}
	}
	for _, request := range requests {
		snapshot := snapshotFrom(t, request.Inputs)
		for _, d := range snapshot.Sources {
			if d.Kind == "tool" {
				t.Fatal("tool definition duplicated in context")
			}
		}
		for _, in := range request.Inputs {
			if in.RuntimeContext && (strings.Contains(in.Text, "InputSchema") || strings.Contains(in.Text, "revision")) {
				t.Fatal("private metadata leaked")
			}
		}
	}

	var saved privateContext
	testkit.RequireNoError(t, json.Unmarshal(out.Checkpoint.Context, &saved))
	if saved.Automatic == nil || contextHash(contextInput(saved.Automatic).Text) != second.Version {
		t.Fatal("applied version not checkpointed")
	}
	for _, input := range saved.Inputs {
		if input.RuntimeContext {
			t.Fatal("automatic context persisted as ordinary history")
		}
	}
	phase.Store(2)
	turn := f.Turn
	turn.Event.ID, turn.Principal.ExecutionID = "next-event", "next-run"
	restarted := NewRuntime(db, engine.options)
	_, err = restarted.Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
	fourth := snapshotFrom(t, f.ModelRequests()[4].Inputs)
	if len(fourth.Removed) != 1 {
		t.Fatal("restart lost deletions or previous tombstones")
	}
	turn.Event.ID, turn.Principal.ExecutionID = "third-event", "third-run"
	_, err = NewRuntime(db, engine.options).Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
	fifth := snapshotFrom(t, f.ModelRequests()[6].Inputs)
	if fourth.Version != fifth.Version {
		t.Fatal("unchanged restored context changed version")
	}
	if f.VisibleMessageCount() != 0 {
		t.Fatal("private update leaked into chat")
	}
}

func TestContextRefreshFailureDoesNotDispatchOrReserve(t *testing.T) {
	for _, name := range []string{"read error", "oversize", "duplicate", "malformed structured source"} {
		t.Run(name, func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			e.options.ContextSources = func(context.Context, ContextRequest) ([]ContextDocument, error) {
				d := ContextDocument{Kind: "project_instruction", Source: "/work/AGENTS.md", Scope: "/work", Content: "instruction"}
				switch name {
				case "read error":
					return nil, core.NewError(core.Unavailable, "source temporarily unavailable")
				case "oversize":
					d.Content = strings.Repeat("x", (256<<10)+1)
				case "duplicate":
					return []ContextDocument{d, d}, nil
				case "malformed structured source":
					d.Kind, d.Content = "permissions", `{"Granted":"not an array"}`
				}
				return []ContextDocument{d}, nil
			}
			_, err := e.Handle(t.Context(), f.Turn)
			if err == nil || len(f.ModelRequests()) != 0 {
				t.Fatal("failed context refresh dispatched a request")
			}
			rows, err := persistence.Scan[struct{ Reservation policy.Reservation }](t.Context(), db, "security.reservations")
			testkit.RequireNoError(t, err)
			if len(rows) != 0 {
				t.Fatal("refresh failure reserved budget")
			}
			checkpoint, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			saved, err := decodeState(checkpoint)
			testkit.RequireNoError(t, err)
			if saved.Automatic != nil || saved.Current == nil || saved.Current.Event.ID != f.Turn.Event.ID {
				t.Fatal("failed refresh lost the trigger or committed invalid automatic sources")
			}
		})
	}
}

type contextTestTools struct {
	tools.Executor
	definitions []tools.Definition
}

func (x *contextTestTools) Definitions(context.Context, core.Principal) ([]tools.Definition, error) {
	return x.definitions, nil
}

func TestToolContextAddSchemaChangeReorderAndReaddition(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	executor := &contextTestTools{Executor: e.options.Dependencies.Tools, definitions: []tools.Definition{{Name: "first", Description: "first tool", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	e.options.Dependencies.Tools = executor
	execution := core.Execution{Principal: f.Turn.Principal, AssistantID: f.Turn.Event.AssistantID, OperationID: "refresh"}
	inputs, definitions, initial, err := e.refreshContext(t.Context(), execution, []model.Input{{Role: model.System, Text: "base"}}, nil)
	testkit.RequireNoError(t, err)
	if len(definitions) != 1 {
		t.Fatal("initial tool missing")
	}
	executor.definitions = append(executor.definitions, tools.Definition{Name: "second", Description: "new tool", InputSchema: json.RawMessage(`{"type":"object"}`)})
	inputs, definitions, added, err := e.refreshContext(t.Context(), execution, inputs, initial)
	testkit.RequireNoError(t, err)
	if len(definitions) != 2 || len(added.Changes) != 1 || added.Changes[0].Action != "add" {
		t.Fatal("new tool not injected")
	}
	executor.definitions[0], executor.definitions[1] = executor.definitions[1], executor.definitions[0]
	inputs, _, reordered, err := e.refreshContext(t.Context(), execution, inputs, added)
	testkit.RequireNoError(t, err)
	if reordered.Version != added.Version {
		t.Fatal("catalog ordering created a false update")
	}
	executor.definitions[0].InputSchema = json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`)
	inputs, definitions, changed, err := e.refreshContext(t.Context(), execution, inputs, reordered)
	testkit.RequireNoError(t, err)
	if len(changed.Changes) != 1 || changed.Changes[0].Action != "replace" || !strings.Contains(string(definitions[1].InputSchema), "path") {
		t.Fatal("schema change not synchronized")
	}
	executor.definitions = executor.definitions[:1]
	inputs, _, removed, err := e.refreshContext(t.Context(), execution, inputs, changed)
	testkit.RequireNoError(t, err)
	if len(removed.Removed) != 1 {
		t.Fatal("missing withdrawal")
	}
	executor.definitions = append(executor.definitions, tools.Definition{Name: "first", Description: "restored tool", InputSchema: json.RawMessage(`{"type":"object"}`)})
	inputs, _, restored, err := e.refreshContext(t.Context(), execution, inputs, removed)
	testkit.RequireNoError(t, err)
	if len(restored.Removed) != 0 || len(restored.Changes) != 1 || restored.Changes[0].Action != "add" {
		t.Fatal("readdition retained stale withdrawal")
	}
	snapshotFrom(t, inputs)
}

func TestUserPreferencesReplaceLegacyTimeContextAndStayStableAcrossClockChanges(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	execution := core.Execution{Principal: f.Turn.Principal, AssistantID: f.Turn.Event.AssistantID, OperationID: "refresh"}
	legacy := ContextDocument{Kind: "time_context", Source: "runtime/time", Scope: "assistant", Content: `{"current_time":"2026-09-29T00:00:00Z"}`}
	previous := &automaticContext{Sources: []contextEntry{{ContextDocument: legacy, Version: contextHash(legacy)}}}
	preference := ContextDocument{Kind: "user_preferences", Source: "runtime/user_preferences", Scope: "assistant", Content: `{"timezone":"Asia/Shanghai","language":"zh","currency":"CNY"}`}
	e.options.ContextSources = func(context.Context, ContextRequest) ([]ContextDocument, error) {
		return []ContextDocument{preference}, nil
	}
	inputs, _, first, err := e.refreshContext(t.Context(), execution, []model.Input{contextInput(previous)}, previous)
	testkit.RequireNoError(t, err)
	if len(first.Removed) != 1 || first.Removed[0].Kind != "time_context" {
		t.Fatal("legacy time source not withdrawn")
	}
	if strings.Contains(inputs[len(inputs)-1].Text, "current_time") {
		t.Fatal("stale current time still injected")
	}
	now := e.options.Now()
	e.options.Now = func() time.Time { return now.Add(24 * time.Hour) }
	inputs, _, next, err := e.refreshContext(t.Context(), execution, inputs, first)
	testkit.RequireNoError(t, err)
	if next.Version != first.Version || next != first {
		t.Fatal("clock changed preferences snapshot")
	}
	snapshotFrom(t, inputs)
	preference.Content = `{"timezone":"Asia/Shanghai","language":"en","currency":"CNY"}`
	inputs, _, changed, err := e.refreshContext(t.Context(), execution, inputs, next)
	testkit.RequireNoError(t, err)
	if changed.Version == next.Version || len(changed.Changes) != 1 || changed.Changes[0].Kind != "user_preferences" || changed.Changes[0].Action != "replace" {
		t.Fatal("preference update not replaced")
	}
	restored := &privateContext{Inputs: contextHistory(inputs), Automatic: changed}
	inputs, _, resumed, err := e.refreshContext(t.Context(), execution, restored.Inputs, restored.Automatic)
	testkit.RequireNoError(t, err)
	if resumed.Version != changed.Version {
		t.Fatal("checkpoint reconstruction changed preferences version")
	}
	snapshotFrom(t, inputs)
}

func TestChatChannelContextReachesModelAndIsRemovedOnDisconnect(t *testing.T) {
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"completion","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
	}))
	engine := f.Runtime.(*Engine)
	connected := true
	engine.options.ContextSources = func(context.Context, ContextRequest) ([]ContextDocument, error) {
		if !connected {
			return nil, nil
		}
		return []ContextDocument{{Kind: "chat_channel", Source: "runtime:chat_channel", Scope: "assistant", Content: "Current chat channel: Weixin. Avoid interactive cards."}}, nil
	}
	_, err := engine.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	first := snapshotFrom(t, f.ModelRequests()[0].Inputs)
	if len(first.Sources) != 1 || !strings.Contains(first.Sources[0].Content, "Avoid interactive cards") {
		t.Fatal("model did not receive channel context", first)
	}
	connected = false
	turn := f.Turn
	turn.Event.ID, turn.Principal.ExecutionID = "after-disconnect", "next-run"
	_, err = NewRuntime(db, engine.options).Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
	next := snapshotFrom(t, f.ModelRequests()[2].Inputs)
	if len(next.Sources) != 0 || len(next.Removed) != 1 || next.Removed[0].Kind != "chat_channel" {
		t.Fatal("stale channel context retained", next)
	}
}

func TestWorkspaceContextSurvivesCompactionAndRefreshesAfterRecovery(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 65536, nil }
	document := ContextDocument{Kind: "workspace", Source: "runtime/workspace", Scope: "assistant", Content: `{"Path":"/work","Available":true,"Lifetime":"assistant"}`}
	e.options.ContextSources = func(context.Context, ContextRequest) ([]ContextDocument, error) {
		return []ContextDocument{document}, nil
	}
	execution := core.Execution{Principal: f.Turn.Principal, AssistantID: f.Turn.Event.AssistantID, OperationID: "workspace-refresh"}
	history := []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("historical work ", 4000)}}
	inputs, definitions, automatic, err := e.refreshContext(t.Context(), execution, history, nil)
	testkit.RequireNoError(t, err)
	p := turnProgress{Inputs: inputs, Definitions: definitions, Automatic: automatic}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 63000, Inputs: usageInputs(inputs, model.StateSource{}), Tools: contextHash(definitions)}
	interrupted, err := e.prepareContext(t.Context(), f.Turn, "model", &p)
	testkit.RequireNoError(t, err)
	if interrupted || p.Automatic == nil || p.Automatic.Version != automatic.Version {
		t.Fatal("compaction lost workspace metadata")
	}
	snapshotFrom(t, p.Inputs)
	checkpoint, err := f.Checkpoints.Load(t.Context(), f.Turn.Principal.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	restored, err := decodeState(checkpoint)
	testkit.RequireNoError(t, err)
	resumed, _, unchanged, err := e.refreshContext(t.Context(), execution, restored.Inputs, restored.Automatic)
	testkit.RequireNoError(t, err)
	if unchanged.Version != automatic.Version {
		t.Fatal("restoration changed stable workspace metadata")
	}
	snapshotFrom(t, resumed)
	document.Content = `{"Path":"/work","Available":false,"Reason":"sandbox_disabled","Lifetime":"assistant"}`
	refreshed, _, changed, err := e.refreshContext(t.Context(), execution, resumed, unchanged)
	testkit.RequireNoError(t, err)
	if changed.Version == unchanged.Version {
		t.Fatal("availability change was not injected")
	}
	snapshot := snapshotFrom(t, refreshed)
	for _, source := range snapshot.Sources {
		if source.Kind == "workspace" && !strings.Contains(source.Content, `"Available":false`) {
			t.Fatal("stale workspace availability")
		}
	}
}
