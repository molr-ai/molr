package tools

import (
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/quota"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestSkippedToolHasNoPossibleEffects(t *testing.T) {
	r := SkippedResult(Call{Name: "shell_execute", Execution: core.Execution{OperationID: "skip"}}, core.NewError(core.Canceled, "waiting for permission"))
	if r.Failure == nil || r.Failure.ExecutionState != "not_started" || r.Failure.SideEffectsPossible || r.OperationID != "skip" || !strings.Contains(string(r.Content), "waiting for permission") {
		t.Fatalf("incorrect skipped receipt: %+v", r)
	}
}

func TestToolFailuresReturnConcreteResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		state   string
		effects bool
	}{
		{"missing", core.NewError(core.NotFound, "file is missing"), "failed", false},
		{"stale", core.WithReason(core.NewError(core.Conflict, "file version changed"), "file_version_conflict"), "failed", false},
		{"denied", core.NewError(core.Forbidden, "storage access revoked"), "failed", false},
		{"unknown", core.Wrap(core.Unavailable, "worker disconnected", errors.New("PRIVATE_CAUSE")), "unknown", true},
		{"canceled", context.Canceled, "unknown", true},
		{"timeout", context.DeadlineExceeded, "unknown", true},
		{"internal", errors.New("PRIVATE_INTERNAL_ERROR"), "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newContractFixture(t)
			ex := f.Executor.(*executor)
			ex.options.FileWrite = func(context.Context, core.Execution, FileWriteRequest) (json.RawMessage, error) { return nil, tc.err }
			result, err := ex.Execute(t.Context(), f.Call)
			testkit.RequireCode(t, err, core.CodeOf(tc.err))
			if result.Failure == nil || result.Failure.Code != core.CodeOf(tc.err) || result.Failure.ExecutionState != tc.state || result.Failure.SideEffectsPossible != tc.effects || result.OperationID != f.Call.Execution.OperationID {
				t.Fatalf("result=%+v", result)
			}
			if !json.Valid(result.Content) || strings.Contains(string(result.Content), "PRIVATE_") || !strings.Contains(string(result.Content), `"ok":false`) {
				t.Fatalf("invalid or unsafe result: %s", result.Content)
			}
			if result.Failure.Message == "" || result.Failure.Guidance == "" {
				t.Fatal("failure lacks diagnostics")
			}
			if tc.name == "stale" && (result.Failure.Message != "file version changed" || result.Failure.Reason != "file_version_conflict") {
				t.Fatal("reason wrapper hid the specific diagnostic")
			}

		})
	}
	// Decorators without durable evidence still return a concrete conservative outcome.
	f := newContractFixture(t)
	result := ErrorResult(f.Call, core.Invalid("path", "path must be relative"))
	if result.Failure.Field != "path" || result.Failure.Message != "path must be relative" || !result.Failure.SideEffectsPossible {
		t.Fatalf("fallback=%+v", result.Failure)
	}
}

func TestUnknownToolOutcomeIsReturnedWithoutRepeatingEffects(t *testing.T) {
	f := newContractFixture(t)
	ex := f.Executor.(*executor)
	effects := 0
	ex.options.ShellExecute = func(context.Context, core.Execution, ShellRequest) (json.RawMessage, error) {
		effects++
		return nil, core.NewError(core.FailedPrecondition, "process ended without a receipt")
	}
	call := f.Call
	call.Name = "shell_execute"
	call.Arguments = json.RawMessage(`{"compute_id":"compute","script":"something"}`)
	for i := 0; i < 2; i++ {
		result, err := ex.Execute(t.Context(), call)
		testkit.RequireCode(t, err, core.FailedPrecondition)
		if result.Failure == nil || result.Failure.ExecutionState != "unknown" || !result.Failure.SideEffectsPossible || !strings.Contains(result.Failure.Guidance, "before repeating") {
			t.Fatalf("unknown effect=%s", result.Content)
		}
	}
	if effects != 1 {
		t.Fatal("unresolved operation was executed again")
	}
	usage, err := quota.CallTotals(t.Context(), ex.store, "tool", call.Execution.Principal.OwnerID, "", "", time.Time{}, time.Time{})
	testkit.RequireNoError(t, err)
	if usage.Active != 0 || usage.Calls != 1 || usage.Unknown != 1 {
		t.Fatalf("unknown tool retained a slot or lost history: %+v", usage)
	}

}
func TestRejectedToolArgumentsHaveSpecificFailure(t *testing.T) {
	f := newContractFixture(t)
	call := f.Call
	call.Arguments = json.RawMessage(`{"path":"a","text":"b"}`)
	result, err := f.Executor.Execute(t.Context(), call)
	testkit.RequireCode(t, err, core.InvalidArgument)
	if result.Failure == nil || result.Failure.Field != "overwrite" || result.Failure.ExecutionState != "not_started" || result.Failure.SideEffectsPossible || f.Effects() != 0 {
		t.Fatalf("rejection=%s", result.Content)
	}
}
