package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Seed one authorized built-in side-effecting tool and a denied built-in tool.
// Effects counts the actual dependency's accepted external effects. BlockingCall
// is a real pending operation; Cancel must reach its cancellation dependency.
type contractFixture struct {
	Executor        Executor
	Owner           core.Principal
	Call            Call
	DeniedTool      string
	Effects         func() int
	VisibleMessages func() int
	Revoke          func()
	BlockingCall    func() Call
	WasCanceled     func(core.ID) bool
	SecretSentinel  string
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDToolDefinitionsAreFilteredAndExecutionIsIdempotent(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/tools", newContractFixture)
	ctx := testkit.Context(t)
	defs, err := f.Executor.Definitions(ctx, f.Call.Execution.Principal)
	testkit.RequireNoError(t, err)
	found := false
	for _, d := range defs {
		if d.Name == f.DeniedTool {
			t.Fatal("denied tool advertised")
		}
		if d.Name == f.Call.Name {
			found = true
		}
		if !json.Valid(d.InputSchema) {
			t.Fatalf("invalid tool schema: %+v", d)
		}
	}
	if !found {
		t.Fatal("authorized tool missing")
	}
	got, err := f.Executor.Execute(ctx, f.Call)
	testkit.RequireNoError(t, err)
	again, err := f.Executor.Execute(ctx, f.Call)
	testkit.RequireNoError(t, err)
	if got.OperationID != f.Call.Execution.OperationID || again.OperationID != got.OperationID || f.Effects() != 1 {
		t.Fatalf("tool replay repeated effects: %+v %+v, count %d", got, again, f.Effects())
	}
	if !json.Valid(got.Content) || strings.Contains(string(got.Content), f.SecretSentinel) {
		t.Fatalf("unsafe result: %s", got.Content)
	}
	if f.VisibleMessages() != 0 {
		t.Fatal("tool result automatically became a chat message")
	}
}
func TestTDDToolExecutionRechecksGrantsAndRejectsUnknownTools(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/tools", newContractFixture)
	ctx := testkit.Context(t)
	_, err := f.Executor.Definitions(ctx, f.Call.Execution.Principal)
	testkit.RequireNoError(t, err)
	f.Revoke()
	_, err = f.Executor.Execute(ctx, f.Call)
	testkit.RequireCode(t, err, core.Forbidden)
	if f.Effects() != 0 {
		t.Fatal("revoked execution caused an effect")
	}
	unknown := f.Call
	unknown.Name = "load_remote_plugin"
	_, err = f.Executor.Execute(ctx, unknown)
	if err == nil {
		t.Fatal("unknown tool accepted")
	}
}
func TestTDDToolArgumentsCannotOverrideTrustedIdentity(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/tools", newContractFixture)
	ctx := testkit.Context(t)
	call := f.Call
	call.Arguments = json.RawMessage(`{"principal":{"owner_id":"other","kind":"owner"},"resource_id":"foreign-resource"}`)
	_, err := f.Executor.Execute(ctx, call)
	if err == nil {
		t.Fatal("foreign resource accessed through forged tool identity")
	}
	if f.Effects() != 0 {
		t.Fatal("forged identity caused an effect")
	}
	call.Arguments = json.RawMessage(`{`)
	_, err = f.Executor.Execute(ctx, call)
	testkit.RequireCode(t, err, core.InvalidArgument)
}
func TestTDDToolCancelIsExecutionScoped(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/tools", newContractFixture)
	ctx := testkit.Context(t)
	call := f.BlockingCall()
	foreign := call.Execution
	foreign.Principal.OwnerID = "foreign-owner"
	err := f.Executor.Cancel(ctx, foreign, call.Execution.OperationID)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("foreign cancellation = %v", err)
	}
	if f.WasCanceled(call.Execution.OperationID) {
		t.Fatal("foreign principal canceled operation")
	}
	testkit.RequireNoError(t, f.Executor.Cancel(ctx, call.Execution, call.Execution.OperationID))
	if !f.WasCanceled(call.Execution.OperationID) {
		t.Fatal("authorized cancellation did not reach dependency")
	}
}

func TestOwnerToolCatalogIsMetadataOnlyAndRejectsAssistant(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/tools", newContractFixture)
	catalog, err := f.Executor.Catalog(testkit.Context(t), f.Owner)
	testkit.RequireNoError(t, err)
	if len(catalog) == 0 {
		t.Fatal("installed tools missing")
	}
	if f.Effects() != 0 || f.VisibleMessages() != 0 {
		t.Fatal("inspection executed tools")
	}
	if _, err := f.Executor.Catalog(testkit.Context(t), f.Call.Execution.Principal); err == nil {
		t.Fatal("assistant accessed owner tool catalog")
	}
	f.Revoke()
	_, err = f.Executor.Catalog(testkit.Context(t), f.Owner)
	testkit.RequireCode(t, err, core.Forbidden)
}
