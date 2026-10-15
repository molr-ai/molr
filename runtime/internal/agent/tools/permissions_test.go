package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

func TestPermissionRequestAndReceiptCommitTogether(t *testing.T) {
	f := newContractFixture(t)
	ex := f.Executor.(*executor)
	db := ex.store.(*sqlitestore.Store)
	effects := 0
	ex.options.PermissionRequest = func(ctx context.Context, e core.Execution, r policy.PermissionRequestInput) (json.RawMessage, error) {
		effects++
		err := ex.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test.requests", "request", r) })
		return json.RawMessage(`{"ID":"request","State":"pending"}`), err
	}
	call := f.Call
	call.Name = "permission_request"
	call.Arguments = json.RawMessage(`{"resource_kind":"storage","resource_id":"storage","resource_description":"Reports","actions":["read"],"path_prefixes":["/reports"],"expires_at":"2099-01-01T00:00:00Z","reason":"Analyze reports"}`)
	db.FailNextCommit(errors.New("injected commit failure"))
	result, err := ex.Execute(t.Context(), call)
	if err == nil || result.Failure == nil || result.Failure.SideEffectsPossible {
		t.Fatalf("unexpected commit outcome: %+v %v", result, err)
	}
	_, err = persistence.Load[policy.PermissionRequestInput](t.Context(), db, "test.requests", "request")
	if !persistence.Missing(err) {
		t.Fatal("request escaped rollback")
	}
	_, err = operation.NewRepository(db).Get(t.Context(), f.Owner.OwnerID, call.Execution.OperationID)
	if !persistence.Missing(err) {
		t.Fatal("receipt escaped rollback")
	}
	_, err = ex.Execute(t.Context(), call)
	testkit.RequireNoError(t, err)
	_, err = ex.Execute(t.Context(), call)
	testkit.RequireNoError(t, err)
	if effects != 2 {
		t.Fatal("successful request replayed its effects")
	}
	op, err := operation.NewRepository(db).Get(t.Context(), f.Owner.OwnerID, call.Execution.OperationID)
	testkit.RequireNoError(t, err)
	if op.State != operation.Succeeded {
		t.Fatalf("receipt: %+v", op)
	}
}

func TestPermissionRequestRejectsIdentityInjectionAndMalformedArguments(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"resource_kind":"storage","resource_id":"storage","resource_description":"Reports","actions":["read"],"path_prefixes":["/reports"],"expires_at":"2099-01-01T00:00:00Z","reason":"Read","assistant_id":"other"}`, `{"resource_kind":"storage","resource_id":"storage","resource_description":"Reports","actions":["read"],"path_prefixes":["/reports"],"expires_at":"2099-01-01T00:00:00Z","reason":"Read"} {}`} {
		if _, err := parsePermissionRequest(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestNativePermissionRequestUsesDurationAndNetworkToggle(t *testing.T) {
	raw := json.RawMessage(`{"resource_kind":"compute","resource_id":"compute","resource_description":"Native commands","actions":["execute"],"expires_at":"2099-01-01T00:00:00Z","reason":"Build the project","constraints":{"compute":{"cpu_percent":0,"memory_bytes":0,"processes":0,"temp_bytes":0,"max_duration_seconds":120,"unlimited_duration":false}},"additional_requests":[{"resource_kind":"network","resource_id":"network","resource_description":"Network access","actions":["execute"],"reason":"Download dependencies"}]}`)
	input, err := parsePermissionRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if input.Constraints.Compute.CPUPercent != 0 || len(input.Additional) != 1 || input.Additional[0].Constraints != nil {
		t.Fatalf("%+v", input)
	}
	// The advertised schema must allow the same zero-valued duration-only shape.
	var schema map[string]any
	if err = json.Unmarshal(permissionRequestDefinition().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	compute := properties["constraints"].(map[string]any)["properties"].(map[string]any)["compute"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"cpu_percent", "memory_bytes", "processes", "temp_bytes"} {
		if compute[name].(map[string]any)["minimum"] != float64(0) {
			t.Fatalf("%s excludes native requests", name)
		}
	}
}
