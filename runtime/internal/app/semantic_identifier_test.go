package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func TestGrantIDInResourceArgumentReturnsActionableDiagnosticWithoutAdmission(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	g := f.grant(t, f.storage.ID, "named-grant", []policy.Action{policy.Read}, policy.Scope{}, nil)
	executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), Browser: func(context.Context, core.Execution, string, tools.BrowserRequest) (json.RawMessage, error) {
		t.Fatal("invalid reference reached browser")
		return nil, nil
	}, ShellExecute: func(context.Context, core.Execution, tools.ShellRequest) (json.RawMessage, error) {
		t.Fatal("invalid reference reached shell")
		return nil, nil
	}})
	for _, name := range []string{"shell_execute", "browser_session"} {
		e := f.execution
		e.OperationID = core.ID("wrong-" + name)
		args, _ := json.Marshal(map[string]any{"compute_id": g.ID, "script": "echo ok"})
		if name == "browser_session" {
			args, _ = json.Marshal(map[string]any{"compute_id": g.ID, "action": "start"})
		}
		result, _ := executor.Execute(t.Context(), tools.Call{Execution: e, Name: name, Arguments: args})
		if result.Failure == nil || result.Failure.Field != "compute_id" || result.Failure.Code != core.InvalidArgument || result.Failure.SideEffectsPossible || !strings.Contains(result.Failure.Message, string(g.ResourceID)) {
			t.Fatalf("unhelpful failure: %+v", result.Failure)
		}
		if _, err := operation.NewRepository(f.b.store).Get(t.Context(), f.owner.OwnerID, e.OperationID); !persistence.Missing(err) {
			t.Fatal("operation admitted", err)
		}
	}
}
