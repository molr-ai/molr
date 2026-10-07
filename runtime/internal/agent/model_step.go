package agent

import (
	"context"
	"errors"
	"io"
	"strconv"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func modelOperationID(t Turn, step int) core.ID {
	return stableID(string(t.Event.ID), "model", strconv.Itoa(step))
}
func (e *Engine) setPhase(key, phase string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[key]
	active.phase = phase
	e.active[key] = active
}
func (e *Engine) generateStep(ctx context.Context, t Turn, modelID core.ID, definitions []model.ToolDefinition, p turnProgress) (model.Input, bool, error) {
	outputTokens, err := e.outputLimit(ctx, t, modelID)
	if err != nil {
		return model.Input{}, false, err
	}
	return e.generateContextStep(ctx, t, modelID, definitions, p, modelOperationID(t, p.Step), outputTokens)
}
func (e *Engine) generateContextAttempt(ctx context.Context, t Turn, modelID core.ID, definitions []model.ToolDefinition, p turnProgress, operationID core.ID, outputTokens int, retry modelRetry) (_ model.Input, _ bool, runErr error) {
	cap, err := e.outputLimit(ctx, t, modelID)
	if err != nil {
		return model.Input{}, false, err
	}
	window, err := e.contextWindow(ctx, t, modelID)
	if err != nil {
		return model.Input{}, false, err
	}
	p.Inputs = modelInputs(p.Inputs)
	p.Definitions = definitions
	tokens, _ := contextTokens(p, modelID)
	remaining := int64(window) - tokens
	if remaining <= 0 {
		return model.Input{}, false, contextCapacityError()
	}
	// Output is a maximum, not a requirement to fill the window. Small-context
	// models remain usable when the provider omits output metadata.
	outputTokens = min(outputTokens, cap, int(remaining))
	execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: operationID}
	empty := model.Input{}
	e.options.Trace.Record(ctx, "model.start", "started", execution.OperationID, map[string]any{"LogicalID": retry.LogicalID, "Attempt": retry.Attempt, "MaxAttempts": retry.Policy.MaxAttempts, "Step": p.Step + 1, "ModelID": modelID, "Inputs": p.Inputs, "Tools": definitions, "MaxOutputTokens": outputTokens})
	estimate, err := e.options.Estimate(ctx, modelID, p.Inputs, outputTokens)
	if err != nil {
		return empty, false, err
	}
	reservation, err := e.options.Dependencies.Budget.Reserve(ctx, policy.ReserveRequest{ModelID: modelID, Execution: execution, Estimate: estimate})
	budgetState := "succeeded"
	if err != nil {
		budgetState = "failed"
	}
	e.options.Trace.Record(ctx, "budget.check", budgetState, execution.OperationID, map[string]any{"Estimate": estimate, "Reservation": reservation, "Code": core.CodeOf(err), "Reason": core.ReasonOf(err)})
	if err != nil {
		return empty, false, err
	}
	// Every return after reservation, including cancellation and preparation errors,
	// ends the local hold. Preserve unknown costs without assuming zero charge.
	defer func() {
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := e.finishModelBudget(finishCtx, execution, reservation.ID); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	modelCtx, cancel := context.WithCancelCause(ctx)
	key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
	e.mu.Lock()
	active := e.active[key]
	active.modelCancel = cancel
	active.phase = "model"
	e.active[key] = active
	e.mu.Unlock()
	defer func() {
		cancel(nil)
		e.mu.Lock()
		active := e.active[key]
		active.modelCancel = nil
		active.phase = "preparing"
		e.active[key] = active
		e.mu.Unlock()
	}()
	// Register cancellation before checking the durable inbox, closing the window
	// between the last boundary check and an in-flight provider request.
	pending, err := e.pendingInterrupts(ctx, t, p.Joined)
	if err != nil || len(pending) > 0 {
		if len(pending) > 0 {
			e.options.Trace.Record(ctx, "model.complete", "canceled", execution.OperationID, map[string]any{"Reason": "expedited"})
		}
		return empty, len(pending) > 0, err
	}
	go e.watchInterrupts(modelCtx, t, p.Joined, cancel)
	stream, err := e.options.Dependencies.Models.Generate(modelCtx, model.Request{LogicalID: retry.LogicalID, Attempt: retry.Attempt, Execution: execution, ModelID: modelID, Inputs: p.Inputs, Tools: definitions, MaxOutputTokens: outputTokens, ReservationID: reservation.ID})
	var receipt *model.Receipt
	terminalSeen := false
	result := model.Input{Role: model.Assistant}
	if err == nil {
		for n := 0; n < 10000; n++ {
			var delta model.Delta
			delta, err = stream.Next(modelCtx)
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
			switch delta.Kind {
			case model.TextDelta:
				result.Text += delta.Text
			case model.ProviderStateDelta:
				result.ProviderState = delta.ProviderState
			case model.ToolCallDelta:
				if delta.ToolCall != nil {
					result.Calls = append(result.Calls, *delta.ToolCall)
				}
			case model.Completed:
				terminalSeen = true
				receipt = delta.Receipt
			}
		}
		_ = stream.Close()
	}
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	// The provider may have durably settled a response just as cancellation
	// arrived. Consult its receipt before marking an undrained stream unknown.
	if receipt == nil {
		known, loadErr := model.NewReceipts(e.store).ByOperation(finishCtx, t.Principal.OwnerID, execution.OperationID)
		if loadErr == nil {
			receipt = &known
		} else if !persistence.Missing(loadErr) {
			return empty, false, loadErr
		}
	}
	if ctx.Err() != nil {
		return empty, false, persistence.CheckContext(ctx)
	}
	if context.Cause(modelCtx) == errExpedited {
		e.options.Trace.Record(ctx, "model.complete", "canceled", execution.OperationID, map[string]any{"Reason": "expedited", "Receipt": receipt})
		return empty, true, nil
	}
	if err != nil {
		if receipt == nil || (receipt.Charged != nil && model.FailureOf(err) == nil) {
			err = modelTerminal(err)
		}
		return empty, false, err
	}
	if !terminalSeen || receipt == nil {
		return empty, false, core.NewError(core.Unavailable, "model stream lacks terminal receipt")
	}
	e.options.Trace.Record(ctx, "model.complete", "succeeded", execution.OperationID, map[string]any{"LogicalID": retry.LogicalID, "Attempt": retry.Attempt, "MaxAttempts": retry.Policy.MaxAttempts, "Text": result.Text, "ProviderState": result.ProviderState, "Calls": result.Calls, "Receipt": receipt})
	return result, false, nil
}

// finishModelBudget accepts durable evidence even if cancellation arrived before
// the engine consumed it. Unknown is distinct from a proven no-charge release
// and can still be settled when a late receipt becomes available.
func (e *Engine) finishModelBudget(ctx context.Context, execution core.Execution, reservationID core.ID) error {
	receipt, err := model.NewReceipts(e.store).ByOperation(ctx, execution.Principal.OwnerID, execution.OperationID)
	if persistence.Missing(err) {
		return e.options.Dependencies.Budget.Release(ctx, execution, reservationID)
	}
	if err != nil {
		return err
	}
	if receipt.Failure != nil && receipt.Failure.NoCharge {
		return e.options.Dependencies.Budget.Release(ctx, execution, reservationID)
	}
	if receipt.Charged != nil {
		_, err := e.options.Dependencies.Budget.Settle(ctx, execution, policy.SettleRequest{ReservationID: reservationID, OperationID: execution.OperationID, ReceiptID: receipt.ID, Actual: *receipt.Charged})
		return err
	}
	return e.options.Dependencies.Budget.MarkUnknown(ctx, execution, reservationID)
}

func (e *Engine) outputLimit(ctx context.Context, t Turn, id core.ID) (int, error) {
	if e.options.OutputLimit != nil {
		return e.options.OutputLimit(ctx, t.Principal, id)
	}
	return model.DefaultOutputLimit, nil
}
