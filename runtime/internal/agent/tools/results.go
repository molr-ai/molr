package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Failure is a model-visible tool outcome, not an instruction to stop the loop.
// ExecutionState describes persisted operation evidence; unknown never means safe to retry.
type Failure struct {
	CurrentProfile      *assistant.Profile `json:"current_profile,omitempty"`
	Code                core.Code          `json:"code"`
	Reason              core.Reason        `json:"reason,omitempty"`
	Field               string             `json:"field,omitempty"`
	Message             string             `json:"message"`
	ExecutionState      string             `json:"execution_state"`
	SideEffectsPossible bool               `json:"side_effects_possible"`
	Guidance            string             `json:"guidance"`
}

// SkippedResult records a call that the engine deliberately never dispatched.
func SkippedResult(call Call, err error) Result { return errorResult(call, err, "not_started", false) }

// ErrorResult also handles errors from executor decorators or alternate executors.
// Without operation evidence, it conservatively reports an unknown outcome.
func ErrorResult(call Call, err error) Result {
	return errorResult(call, err, "unknown", true)
}
func errorResult(call Call, err error, state string, effects bool) Result {
	failure := &Failure{Code: core.CodeOf(err), Reason: core.ReasonOf(err), ExecutionState: state, SideEffectsPossible: effects}
	failure.Message = map[core.Code]string{
		core.InvalidArgument: "Invalid tool arguments.", core.Unauthenticated: "Tool authentication failed.", core.Forbidden: "Tool access was denied.", core.NotFound: "The requested tool or object was not found.", core.Conflict: "The request conflicts with the current state.", core.FailedPrecondition: "A tool precondition was not met.", core.RateLimited: "The tool is rate limited.", core.BudgetExceeded: "The tool's budget is exhausted.", core.Unavailable: "The tool is unavailable.", core.Canceled: "The tool invocation was canceled.", core.DeadlineExceeded: "The tool invocation timed out.", core.Internal: "An internal tool error occurred.",
	}[failure.Code]
	if failure.Message == "" {
		failure.Message = "The tool invocation failed."
	}
	// core.Error.Message is the trusted, safe diagnostic. Never publish arbitrary
	// error strings, OS errors or wrapped private causes.
	for current := err; current != nil; current = errors.Unwrap(current) {
		if detail, ok := current.(*core.Error); ok {
			if detail.Message != "" {
				failure.Message = boundedDiagnostic(detail.Message, 2048)
			}
			if detail.Field != "" {
				failure.Field = boundedDiagnostic(detail.Field, 128)
			}
		}
	}

	var conflict *assistant.ProfileConflict
	if errors.As(err, &conflict) {
		failure.CurrentProfile = &conflict.Current
	}
	failure.Guidance = "Use the code, reason and field to correct the request, choose another available tool, or explain the failure to the user."
	if effects {
		failure.Guidance = "This operation may already have produced side effects. Inspect current state or reconcile the operation before repeating it; you can continue other work or report the uncertainty to the user."
		if state == string(operation.Succeeded) {
			failure.Guidance = "This operation ID already records a successful execution. Do not repeat its effects; inspect the existing result or report the access error."
		}
	}
	if failure.Reason == "browser_snapshot_required" {
		failure.Message = "Browser control was returned by the user."
		failure.Guidance = "Read a fresh browser snapshot before continuing. Do not reuse old element references or repeat queued actions."
		failure.SideEffectsPossible = false
	}
	if failure.Reason == "browser_user_controlled" {
		failure.Message = "The user currently controls the browser."
		failure.Guidance = "You may read snapshots, screenshots, tabs and URL. Wait for the user to return control before any browser operation; do not bypass takeover through shell or another session."
		failure.SideEffectsPossible = false
	}
	content, _ := json.Marshal(struct {
		OK          bool     `json:"ok"`
		OperationID core.ID  `json:"operation_id"`
		Error       *Failure `json:"error"`
	}{false, call.Execution.OperationID, failure})
	return Result{OperationID: call.Execution.OperationID, Content: content, Failure: failure}
}
func boundedDiagnostic(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (e *executor) Execute(ctx context.Context, call Call) (Result, error) {
	var result Result
	var err error
	if call.Name == "permission_request" {
		// This tool only writes local request/notification records. Commit its effect
		// and operation receipt together, so a crash cannot leave an unknown request.
		err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			result, err = e.execute(ctx, call)
			return err
		})
	} else {
		result, err = e.execute(ctx, call)
	}
	if err == nil {
		return result, nil
	}
	// A tool-local cancellation must not suppress its result. Look up the durable
	// receipt with a bounded independent context even when this invocation ended.
	evidenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	state := "unknown"
	effects := true
	for _, definition := range e.definitions() {
		if definition.Name == call.Name {
			effects = definition.SideEffect
			break
		}
	}
	if call.Execution.Validate() != nil {
		state = "not_started"
		effects = false
	} else {
		op, lookup := operation.NewRepository(e.store).Get(evidenceCtx, call.Execution.Principal.OwnerID, call.Execution.OperationID)
		if persistence.Missing(lookup) {
			state = "not_started"
			effects = false
		} else if lookup == nil && op.AssistantID == call.Execution.AssistantID && op.ExecutionID == call.Execution.Principal.ExecutionID && op.TaskID == call.Execution.TaskID && op.Action == call.Name {
			sum := sha256.Sum256(call.Arguments)
			if op.InputDigest == hex.EncodeToString(sum[:]) {
				state = string(op.State)
				if op.State == operation.Prepared || op.State == operation.Failed {
					effects = false
				}
			}
		}
	}
	return errorResult(call, err, state, effects), err
}
