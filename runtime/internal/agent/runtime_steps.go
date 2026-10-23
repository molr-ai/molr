package agent

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// runModelStep returns true when an interrupt requires another preparation
// pass. Only an accepted model response can advance to the tool stage.
func (e *Engine) runModelStep(runCtx context.Context, t Turn, key string, modelID core.ID, progress *turnProgress, pendingRetry *core.ID, execution core.Execution, estimate *contextEstimate) (bool, error) {
	var err error
	e.setPhase(key, "preparing")
	_, receiptErr := model.NewReceipts(e.store).ByOperation(runCtx, t.Principal.OwnerID, execution.OperationID)
	if receiptErr != nil && !persistence.Missing(receiptErr) {
		return false, receiptErr
	}
	if persistence.Missing(receiptErr) && *pendingRetry == "" && t.Event.Kind != event.DreamDue {
		if err := e.intakeEvents(runCtx, t, progress); err != nil {
			return false, err
		}
		progress.Inputs, progress.Definitions, progress.Automatic, err = e.refreshContext(runCtx, execution, progress.Inputs, progress.Automatic)
		if err != nil {
			return false, err
		}
	}
	if persistence.Missing(receiptErr) && t.Event.Kind != event.DreamDue {
		if err := e.saveProgress(runCtx, t, progress, progressIfChanged); err != nil {
			return false, err
		}
		interrupted, compactErr := e.prepareContext(runCtx, t, modelID, progress)
		if compactErr != nil {
			return false, compactErr
		}
		if interrupted {
			*pendingRetry = ""
			return true, nil
		}
	}
	// Persist the exact inputs before dispatch. If no response reaches
	// the checkpoint, a later attempt can repeat the request; a pending
	// expedite supersedes it before any tools are started.

	if err := e.saveProgress(runCtx, t, progress, progressIfChanged); err != nil {
		return false, err
	}
	response, interrupted, err := e.generateStep(runCtx, t, modelID, progress.Definitions, *progress)
	if err != nil {
		return false, err
	}
	if interrupted {
		*pendingRetry = ""
		progress.Step++
		if err := e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
			if err := e.saveProgress(ctx, t, progress, progressIfChanged); err != nil {
				return err
			}
			return tx.Put(ctx, "agent.superseded_models", persistence.Key(t.Principal.OwnerID, execution.OperationID), true)
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	*pendingRetry = ""
	progress.Handled = append([]core.ID{}, progress.Joined...)
	progress.Usage = e.measureContext(runCtx, t, modelID, *progress)
	progress.Inputs = append(progress.Inputs, response)
	if t.Event.Kind != event.DreamDue && progress.Usage != nil {
		limit, threshold, err := e.contextLimits(runCtx, t, modelID)
		if err != nil {
			return false, err
		}
		tokens, source := contextTokens(*progress, modelID, progress.Usage.Source)
		*estimate = contextEstimate{tokens: tokens, source: source, modelSource: progress.Usage.Source}
		if err = e.recordContextStatus(runCtx, t, ContextStatus{ModelID: modelID, Limit: limit, Tokens: tokens, Source: source, CompressionThresholdTokens: threshold}); err != nil {
			return false, err
		}
	}
	// Model completion is a durable observation even when the response repeats.
	progress.Response = &response
	if err := e.saveProgress(runCtx, t, progress, progressAlways); err != nil {
		return false, err
	}
	return false, nil
}
func (e *Engine) runToolSteps(runCtx context.Context, t Turn, key string, modelID core.ID, progress *turnProgress, execution core.Execution) error {
	response := *progress.Response

	skipped := false
	for progress.NextTool < len(response.Calls) {
		call := response.Calls[progress.NextTool]
		if err := runCtx.Err(); err != nil {
			return err
		}
		pending, err := e.pendingInterrupts(runCtx, t, progress.Joined)
		if err != nil {
			return err
		}
		toolExecution := execution
		toolExecution.OperationID = stableID(string(t.Event.ID), "tool", call.ID)
		invocation := tools.Call{ModelID: modelID, Execution: toolExecution, Name: call.Name, Arguments: call.Arguments}
		var result tools.Result
		skip := skipped || len(pending) > 0 || progress.PermissionPending
		if skip {
			// A crash can occur after an effect commits but before its reply
			// is saved. Replay durable tool evidence instead of claiming that
			// an already attempted operation was skipped.
			prior, loadErr := operation.NewRepository(e.store).FindByKey(runCtx, t.Principal.OwnerID, string(toolExecution.OperationID))
			if loadErr != nil && !persistence.Missing(loadErr) {
				return loadErr
			}
			if loadErr == nil && prior.State != operation.Prepared {
				skip = false
			}
		}
		if skip {
			skipped = true
			message := "not executed: user expedited new messages; reconsider after reading them"
			if progress.PermissionPending {
				message = "not executed: waiting for a permission decision; recheck permissions before continuing"
			}
			result = tools.SkippedResult(invocation, core.NewError(core.Canceled, message))
			e.options.Trace.Record(runCtx, "tool.skipped", "canceled", toolExecution.OperationID, map[string]any{"Name": call.Name, "CallID": call.ID, "Reason": "expedited"})
		} else {
			result, err = e.executeTurnTool(runCtx, key, progress, call, invocation)
			if err != nil {
				return err
			}
		}
		if call.Name == "permission_request" && result.Failure == nil {
			var request struct{ State string }
			if json.Unmarshal(result.Content, &request) == nil && request.State == "pending" {
				progress.PermissionPending = true
			}
		}
		readIDs := conversationReadIDs(call, result)
		if e.options.PrepareToolResult != nil {
			result, err = e.options.PrepareToolResult(runCtx, toolExecution, result)
			if err != nil {
				return err
			} // durable tool receipt prevents replaying effects
		}
		progress.Inputs = append(progress.Inputs, model.Input{ToolOperationID: toolExecution.OperationID, Role: model.Tool, Text: string(result.Content), Media: result.Media, ReplyTo: call.ID, ReadMessageIDs: readIDs})
		progress.NextTool++
		if err := e.saveProgress(runCtx, t, progress, progressIfChanged); err != nil {
			return err
		}
	}
	progress.Response = nil
	progress.NextTool = 0
	progress.Step++
	progress.Finished = len(response.Calls) == 0 || progress.PermissionPending
	// Check publication since the latest accepted user event, including
	// subsequent permission decisions and background executions. The
	// checkpoint keeps the marker together with the reminder input.
	if progress.Finished && !progress.PermissionPending && progress.Publish.UserEventID != "" && !progress.Publish.Attempted && !progress.Publish.Reminded {
		progress.Inputs = append(progress.Inputs, model.Input{Role: model.User, Text: prompt.Text("publish-reminder", nil)})
		progress.Publish.Reminded = true
		progress.Finished = false
	}
	if err := e.saveProgress(runCtx, t, progress, progressIfChanged); err != nil {
		return err
	}
	return nil
}

// executeTurnTool is the only tool-dispatch boundary in a turn. The caller
// persists the result before advancing the tool cursor.
func (e *Engine) executeTurnTool(runCtx context.Context, key string, progress *turnProgress, call model.ToolCall, invocation tools.Call) (tools.Result, error) {
	if err := e.checkEnvironment(runCtx); err != nil {
		return tools.Result{}, err
	}
	e.setPhase(key, "tool")
	if invocation.Name == "conversation_publish" {
		progress.Publish.Attempted = true
	}
	e.options.Trace.Record(runCtx, "tool.start", "started", invocation.Execution.OperationID, map[string]any{"Name": call.Name, "CallID": call.ID, "Arguments": call.Arguments})
	result, err := e.options.Dependencies.Tools.Execute(runCtx, invocation)
	e.setPhase(key, "preparing")
	if err != nil && result.Failure == nil {
		result = tools.ErrorResult(invocation, err)
	}
	if len(result.Content) == 0 || len(result.Content) > 64<<20 || !json.Valid(result.Content) {
		result = tools.ErrorResult(invocation, core.NewError(core.Internal, "tool returned an invalid result"))
	}
	toolState := "succeeded"
	var code core.Code
	var reason core.Reason
	if result.Failure != nil {
		toolState = "failed"
		code = result.Failure.Code
		reason = result.Failure.Reason
	}
	e.options.Trace.Record(runCtx, "tool.complete", toolState, invocation.Execution.OperationID, map[string]any{"Name": call.Name, "CallID": call.ID, "Result": result, "Code": code, "Reason": reason})
	return result, nil
}
