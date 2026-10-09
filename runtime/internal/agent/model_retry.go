package agent

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
)

type RetryPolicy struct {
	MaxAttempts                            int
	BaseDelay, MaxDelay, MaxWait, Deadline time.Duration
}

func (p RetryPolicy) defaults() RetryPolicy {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 4
	}
	if p.BaseDelay == 0 {
		p.BaseDelay = 2 * time.Second
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.MaxWait == 0 {
		p.MaxWait = 120 * time.Second
	}
	if p.Deadline == 0 {
		p.Deadline = model.DefaultRequestTimeout
	}
	return p
}

type modelRetry struct {
	OwnerID, AssistantID, EventID core.ID
	ContextStored                 bool
	LogicalID, ModelID            core.ID
	Attempt                       int
	StartedAt, Deadline, NextAt   time.Time
	Wait                          time.Duration
	Policy                        RetryPolicy
	Inputs                        []model.Input
	Definitions                   []model.ToolDefinition
	OutputTokens                  int
	Failure                       *model.Failure `json:",omitempty"`
}

func (r modelRetry) operationID() core.ID {
	if r.Attempt <= 1 {
		return r.LogicalID
	}
	return stableID(string(r.LogicalID), "attempt", strconv.Itoa(r.Attempt))
}
func (e *Engine) modelAttemptID(ctx context.Context, owner, logical core.ID) (core.ID, error) {
	r, err := persistence.Load[modelRetry](ctx, e.store, "agent.model_retries", persistence.Key(owner, logical))
	if persistence.Missing(err) {
		return logical, nil
	}
	if err != nil {
		return "", err
	}
	return r.operationID(), nil
}

type terminalModelError struct{ cause error }

func (e *terminalModelError) Error() string      { return e.cause.Error() }
func (e *terminalModelError) Unwrap() error      { return e.cause }
func (e *terminalModelError) KnownFailure() bool { return true }
func modelTerminal(err error) error              { return &terminalModelError{err} }

func (e *Engine) generateContextStep(ctx context.Context, t Turn, modelID core.ID, definitions []model.ToolDefinition, p turnProgress, logicalID core.ID, outputTokens int) (model.Input, bool, error) {
	pendingKey := persistence.Key(t.Principal.OwnerID, t.Event.ID)
	var r modelRetry
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		r, err = loadRetry(ctx, tx, t.Principal.OwnerID, logicalID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if persistence.Missing(err) {
			policy := e.options.ModelRetry.defaults()
			now := e.options.Now().UTC()
			r = modelRetry{OwnerID: t.Principal.OwnerID, AssistantID: t.Event.AssistantID, EventID: t.Event.ID, LogicalID: logicalID, ModelID: modelID, Attempt: 1, StartedAt: now, Deadline: now.Add(policy.Deadline), Policy: policy, Inputs: p.Inputs, Definitions: definitions, OutputTokens: outputTokens}
			if deadline, ok := ctx.Deadline(); ok && deadline.Before(r.Deadline) {
				r.Deadline = deadline
			}
			if err := saveRetry(ctx, tx, r); err != nil {
				return err
			}
			// Dispatch the canonical persisted snapshot even on the first attempt.
			// A restart must not change raw JSON schemas/arguments and request digests.
			r, err = loadRetry(ctx, tx, t.Principal.OwnerID, logicalID)
			if err != nil {
				return err
			}
		}
		if r.ModelID != modelID {
			return modelTerminal(core.WithReason(core.NewError(core.FailedPrecondition, "model binding changed"), core.ModelBindingChanged))
		}
		return tx.Put(ctx, "agent.pending_model_retry", pendingKey, logicalID)
	})
	if err != nil {
		return model.Input{}, false, err
	}
	pending, err := e.pendingInterrupts(ctx, t, p.Joined)
	if err != nil {
		return model.Input{}, false, err
	}
	if len(pending) > 0 {
		err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Delete(ctx, "agent.pending_model_retry", pendingKey)
		})
		return model.Input{}, true, err
	}
	now := e.options.Now()
	// A previous process may have dispatched this attempt without committing its
	// output to the checkpoint. There is no result cache: retain its billing
	// evidence and use a fresh operation/reservation for the replacement request.
	known, receiptErr := model.NewReceipts(e.store).ByOperation(ctx, t.Principal.OwnerID, r.operationID())
	if receiptErr != nil && !persistence.Missing(receiptErr) {
		return model.Input{}, false, receiptErr
	}
	if !now.Before(r.Deadline) {
		return model.Input{}, false, modelTerminal(core.WithReason(core.NewError(core.DeadlineExceeded, "model retry deadline exhausted"), core.ModelRetryExhausted))
	}
	if receiptErr == nil && (known.Failure == nil || !known.OutcomeKnown) {
		if r.Attempt >= r.Policy.MaxAttempts {
			return model.Input{}, false, modelTerminal(core.WithReason(core.NewError(core.Unavailable, "model retry limit exhausted"), core.ModelRetryExhausted))
		}
		r.Attempt++
		r.NextAt, r.Failure = time.Time{}, nil
		if err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return saveRetry(ctx, tx, r) }); err != nil {
			return model.Input{}, false, err
		}
	}
	if now.Before(r.NextAt) {
		return model.Input{}, false, &scheduler.Deferred{At: r.NextAt, Cause: r.Failure}
	}
	p.Inputs, p.Definitions = r.Inputs, r.Definitions
	attemptCtx := ctx
	if now.Before(r.Deadline) {
		var cancel context.CancelFunc
		attemptCtx, cancel = context.WithDeadline(ctx, r.Deadline)
		defer cancel()
	}
	var result model.Input
	var interrupted bool
	if receiptErr == nil && known.Failure != nil && known.OutcomeKnown {
		// Resume the recorded failure decision without reserving or dispatching the
		// same operation again if the process exited before scheduling its retry.
		err = known.Failure
	} else {
		result, interrupted, err = e.generateContextAttempt(attemptCtx, t, modelID, r.Definitions, p, r.operationID(), r.OutputTokens, r)
	}
	if interrupted || err == nil {
		clearCtx, clearCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer clearCancel()
		clearErr := e.store.Write(clearCtx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Delete(ctx, "agent.pending_model_retry", pendingKey)
		})
		if clearErr != nil {
			return model.Input{}, false, clearErr
		}
		return result, interrupted, err
	}
	recordCtx, recordCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer recordCancel()
	receipt, loadErr := model.NewReceipts(e.store).ByOperation(recordCtx, t.Principal.OwnerID, r.operationID())
	state := "failed"
	if core.CodeOf(err) == core.Canceled || ctx.Err() != nil {
		state = "canceled"
	}
	e.options.Trace.Record(recordCtx, "model.complete", state, r.operationID(), map[string]any{"LogicalID": logicalID, "Attempt": r.Attempt, "MaxAttempts": r.Policy.MaxAttempts, "Code": core.CodeOf(err), "Reason": core.ReasonOf(err), "Failure": model.FailureOf(err)})
	if ctx.Err() != nil {
		return result, false, err
	}
	if model.FailureOf(err) == nil && (persistence.Missing(loadErr) || (loadErr == nil && receipt.Charged != nil)) {
		err = modelTerminal(err)
	}
	failure := model.FailureOf(err)
	if failure == nil || !failure.Retryable || !failure.OutcomeKnown || !failure.NoCharge {
		return result, false, err
	}
	delay := (retry.Policy{BaseDelay: r.Policy.BaseDelay, MaxDelay: r.Policy.MaxDelay}).Delay(r.Attempt)
	delay = delay/2 + time.Duration(rand.Int64N(max(1, int64(delay-delay/2))))
	delay = max(delay, failure.RetryAfter)
	next := e.options.Now().Add(delay)
	if r.Attempt >= r.Policy.MaxAttempts || r.Wait+delay > r.Policy.MaxWait || !next.Before(r.Deadline) {
		exhausted := modelTerminal(core.WithReason(core.NewError(core.Unavailable, "model retry limit exhausted"), core.ModelRetryExhausted))
		e.options.Trace.Record(ctx, "model.retry_exhausted", "failed", r.operationID(), map[string]any{"LogicalID": logicalID, "Attempt": r.Attempt, "MaxAttempts": r.Policy.MaxAttempts, "Failure": failure, "Reason": core.ModelRetryExhausted})
		return result, false, exhausted
	}
	attemptedID := r.operationID()
	r.Attempt++
	r.Wait += delay
	r.NextAt = next
	r.Failure = failure
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := saveRetry(ctx, tx, r); err != nil {
			return err
		}
		job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", pendingKey)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil {
			if err := scheduler.NewQueue(e.store).Retry(ctx, t.Principal, scheduler.RetryRequest{JobID: job.Event.ID, ExpectedVersion: job.Version, NotBefore: next, FailureCode: failure.Code}); err != nil {
				return err
			}
		}
		statusKey := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, statusKey)
		if err != nil {
			return err
		}
		if status.ExecutionID != t.Principal.ExecutionID || (status.State != Working && status.State != Dreaming) {
			return core.NewError(core.Canceled, "execution stopped")
		}
		status.ModelRetry = &ModelRetryStatus{Attempt: r.Attempt, MaxAttempts: r.Policy.MaxAttempts, NextAt: next, Reason: string(failure.Reason)}
		status.WaitingReason = string(failure.Reason)
		if err := e.saveStatus(ctx, tx, statusKey, status); err != nil {
			return err
		}
		if err := realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(logicalID), "retry", strconv.Itoa(r.Attempt)), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: e.options.Now().UTC()}); err != nil {
			return err
		}
		e.options.Trace.Record(ctx, "model.retry", "retrying", attemptedID, map[string]any{"LogicalID": logicalID, "Attempt": r.Attempt - 1, "NextAttempt": r.Attempt, "MaxAttempts": r.Policy.MaxAttempts, "NextAt": next, "DelayMs": delay.Milliseconds(), "Reason": failure.Reason, "Status": failure.Status})
		return nil
	})
	if err != nil {
		return result, false, err
	}
	return result, false, &scheduler.Deferred{At: next, Cause: failure}
}
