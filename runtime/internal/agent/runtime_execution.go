package agent

import (
	"context"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func (e *Engine) executeTurn(ctx, runCtx context.Context, t Turn, key string) (outcome Outcome, resultErr error) {
	defer func() { e.finishTurn(ctx, runCtx, t, key, outcome, &resultErr) }()
	var err error
	if t.Event.Kind == event.DreamDue && e.options.DreamDeadline != nil {
		deadline, err := e.options.DreamDeadline(runCtx, t.Principal, t.Event)
		if err != nil {
			return Outcome{}, err
		}
		if !deadline.IsZero() {
			var stop context.CancelFunc
			runCtx, stop = context.WithTimeout(runCtx, deadline.Sub(e.options.Now()))
			defer stop()
		}
		defer func() {
			if runCtx.Err() == context.DeadlineExceeded && outcome.Checkpoint.LastEventID == "" {
				resultErr = core.NewError(core.DeadlineExceeded, "dream deadline reached")
			}
		}()
		if err := e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
			status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
			if err != nil {
				return err
			}
			status.DreamDeadline = &deadline
			status.DreamRunID = t.Event.OperationID
			return e.saveStatus(ctx, tx, key, status)
		}); err != nil {
			return Outcome{}, err
		}
		e.options.Trace.Record(runCtx, "dream.started", "succeeded", "", map[string]any{"Deadline": deadline, "RunID": t.Event.OperationID})
	}

	var checkpoint Checkpoint
	var progress turnProgress
	if t.Event.Kind == event.DreamDue {
		runCtx, progress, err = e.prepareDream(runCtx, t)
	} else {
		checkpoint, progress, err = e.prepareExecution(runCtx, t)
	}
	if err != nil {
		return Outcome{}, err
	}
	e.options.Trace.Record(runCtx, "context.ready", "succeeded", "", map[string]any{"Inputs": progress.Inputs})
	modelID, err := e.options.ResolveModel(runCtx, t.Principal, t.Event.AssistantID)
	if err != nil {
		return Outcome{}, err
	}
	pendingRetry, pendingErr := persistence.Load[core.ID](runCtx, e.store, "agent.pending_model_retry", persistence.Key(t.Principal.OwnerID, t.Event.ID))
	if pendingErr != nil && !persistence.Missing(pendingErr) {
		return Outcome{}, pendingErr
	}
	if pendingRetry != "" {
		prior, err := persistence.Load[modelRetry](runCtx, e.store, "agent.model_retries", persistence.Key(t.Principal.OwnerID, pendingRetry))
		if err != nil {
			return Outcome{}, err
		}
		if prior.ModelID != modelID {
			return Outcome{}, modelTerminal(core.WithReason(core.NewError(core.FailedPrecondition, "model changed"), core.ModelBindingChanged))
		}
	}
	if t.Event.Kind == event.ContextCompressionRequested {
		progress.Finished = true
		// Compression is maintenance, not a new ordinary model conversation.
		execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, OperationID: t.Event.ID}
		progress.Inputs, progress.Definitions, progress.Automatic, err = e.refreshContext(runCtx, execution, progress.Inputs, progress.Automatic)
		if err != nil {
			return Outcome{}, err
		}
		if err = e.saveProgress(runCtx, t, &progress, progressIfChanged); err != nil {
			return Outcome{}, err
		}
		if err := e.intakeEvents(runCtx, t, &progress); err != nil {
			return Outcome{}, err
		}
		if interrupted, err := e.consumeCompression(runCtx, t, modelID, &progress, t.Event); err != nil {
			return Outcome{}, err
		} else if interrupted {
			return Outcome{}, &scheduler.Deferred{At: e.options.Now(), Cause: core.NewError(core.Canceled, "compression yielded to interrupt")}
		}
	}
	for {
		if err := e.runTurnSteps(runCtx, t, key, modelID, &progress, &pendingRetry); err != nil {
			return Outcome{}, err
		}
		out, err := e.commitTurn(runCtx, t, key, checkpoint, progress)
		if err == errPendingExpedite {
			continue
		}
		return out, err
	}
}
func (e *Engine) prepareDream(runCtx context.Context, t Turn) (context.Context, turnProgress, error) {
	var progress turnProgress
	if e.options.DreamPrepare == nil || e.options.DreamCommit == nil {
		return runCtx, progress, core.NewError(core.Unavailable, "dream processor unavailable")
	}
	evidence, prepareErr := e.options.DreamPrepare(runCtx, t.Principal, t.Event)
	if prepareErr != nil {
		return runCtx, progress, prepareErr
	}
	runCtx = tools.WithVisibleTools(runCtx, e.options.DreamTools)
	if e.options.DreamContext != nil {
		runCtx = e.options.DreamContext(runCtx, t.Principal, t.Event)
	}
	definitions, err := e.options.Dependencies.Tools.Definitions(runCtx, t.Principal)
	if err != nil {
		return runCtx, progress, err
	}
	for _, d := range definitions {
		progress.Definitions = append(progress.Definitions, model.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
	}
	progress.Inputs = []model.Input{
		{Role: model.System, Text: prompt.Text("dream", nil)},
		{Role: model.User, Text: prompt.Text("dream-evidence", evidence)},
	}
	previous, loadErr := loadDreamProgress(runCtx, e.store, t)
	if loadErr == nil {
		progress = previous
	} else if !persistence.Missing(loadErr) {
		return runCtx, progress, loadErr
	}
	if e.options.DreamOutput != nil {
		saved, err := e.options.DreamOutput(runCtx, t.Principal, t.Event)
		if err != nil {
			return runCtx, progress, err
		}
		if saved != "" {
			progress.Inputs = []model.Input{{Role: model.System, Text: prompt.Text("dream", nil)}, {Role: model.User, Text: prompt.Text("dream-evidence", evidence)}, {Role: model.Assistant, Text: saved}}
			progress.Finished = true
			progress.Response = nil
		}
	}
	return runCtx, progress, nil
}
func (e *Engine) runTurnSteps(runCtx context.Context, t Turn, key string, modelID core.ID, progress *turnProgress, pendingRetry *core.ID) error {
	for t.Event.Kind != event.DreamDue && progress.Response == nil && progress.Finished {
		if err := e.intakeEvents(runCtx, t, progress); err != nil {
			return err
		}
		if interrupted, err := e.prepareContext(runCtx, t, modelID, progress); err != nil {
			return err
		} else if interrupted {
			continue
		}
		break
	}
	for !progress.Finished && progress.Step < e.options.MaxSteps {
		if err := e.authorize(runCtx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return err
		}
		execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: modelOperationID(t, progress.Step)}
		if progress.Response == nil {
			interrupted, err := e.runModelStep(runCtx, t, key, modelID, progress, pendingRetry, execution)
			if err != nil {
				return err
			}
			if interrupted {
				continue
			}
		}
		if err := e.detectCompression(runCtx, t, modelID, progress); err != nil {
			return err
		}
		if err := e.runToolSteps(runCtx, t, key, modelID, progress, execution); err != nil {
			return err
		}
		if t.Event.Kind != event.DreamDue {
			if err := e.intakeEvents(runCtx, t, progress); err != nil {
				return err
			}
			if interrupted, err := e.prepareContext(runCtx, t, modelID, progress); err != nil {
				return err
			} else if interrupted {
				progress.Finished = false
				continue
			}
		}
		if progress.Finished {
			break
		}
	}
	return nil
}
