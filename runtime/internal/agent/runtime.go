package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

var loopInstructions = prompt.Text("loop", nil)

type Options struct {
	ModelSource          func(context.Context, core.Principal, core.ID) (model.StateSource, error)
	CheckEnvironment     func(context.Context) error
	ModelRetry           RetryPolicy
	ContextWindow        func(context.Context, core.Principal, core.ID) (int, error)
	OutputLimit          func(context.Context, core.Principal, core.ID) (int, error)
	CompressionThreshold float64
	PrepareToolResult    func(context.Context, core.Execution, tools.Result) (tools.Result, error)
	DreamDeadline        func(context.Context, core.Principal, event.AssistantEvent) (time.Time, error)
	DreamActive          func(context.Context, core.Principal, event.AssistantEvent) (bool, error)
	DreamStop            func(context.Context, core.Principal, core.ID) error
	DreamOutput          func(context.Context, core.Principal, event.AssistantEvent) (string, error)
	DreamTools           []string
	DreamContext         func(context.Context, core.Principal, event.AssistantEvent) context.Context
	DreamPrepare         func(context.Context, core.Principal, event.AssistantEvent) (string, error)
	DreamCommit          func(context.Context, core.Principal, event.AssistantEvent, string) error
	Conversations        conversation.Service
	ContextSources       func(context.Context, ContextRequest) ([]ContextDocument, error)
	InspectCapabilities  func(context.Context, core.Principal, core.ID) (Capabilities, error)
	Trace                *debugtrace.Service
	Dependencies         Dependencies
	ResolveModel         func(context.Context, core.Principal, core.ID) (core.ID, error)
	Estimate             func(context.Context, core.ID, []model.Input, int) (core.Money, error)
	ConversationID       func(context.Context, core.Principal, core.ID) (core.ID, error)
	// Acknowledge joins the checkpoint transaction. It must not perform I/O.
	Acknowledge func(context.Context, Turn) error
	Now         func() time.Time
	MaxSteps    int
}
type activeExecution struct {
	eventID     core.ID
	id          core.ID
	cancel      context.CancelFunc
	modelCancel context.CancelCauseFunc
	phase       string
}

type Engine struct {
	store       persistence.Store
	options     Options
	checkpoints Checkpoints
	builder     ContextBuilder
	mu          sync.Mutex
	gates       map[string]chan struct{}
	active      map[string]activeExecution
}

func NewRuntime(store persistence.Store, options Options) *Engine {
	if options.CompressionThreshold <= 0 || options.CompressionThreshold >= 1 {
		options.CompressionThreshold = 0.8
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxSteps <= 0 {
		options.MaxSteps = 999
	}
	if options.MaxSteps > 999 {
		options.MaxSteps = 999
	}
	return &Engine{store: store, options: options, checkpoints: &checkpoints{store: store, trace: options.Trace}, builder: NewContextBuilder(options), gates: map[string]chan struct{}{}, active: map[string]activeExecution{}}
}

type contextBuilder struct{ options Options }

func NewContextBuilder(options Options) ContextBuilder { return &contextBuilder{options} }
func (b *contextBuilder) Build(ctx context.Context, t Turn) ([]model.Input, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if b.options.Dependencies.Policy == nil {
		return nil, core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	if err := b.options.Dependencies.Policy.Require(ctx, t.Principal, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, Action: policy.Read}); err != nil {
		return nil, err
	}
	inputs := []model.Input{{Role: model.System, Text: loopInstructions}}
	if t.Checkpoint != nil {
		var saved privateContext
		if len(t.Checkpoint.Context) > 256<<20 || json.Unmarshal(t.Checkpoint.Context, &saved) != nil {
			return nil, core.Invalid("checkpoint", "invalid private context")
		}
		if len(saved.Inputs) > 0 {
			inputs = runtimeNotificationRoles(saved.Inputs)
		}
	}
	var conversationID core.ID
	if b.options.ConversationID != nil {
		id, err := b.options.ConversationID(ctx, t.Principal, t.Event.AssistantID)
		if err != nil {
			return nil, err
		}
		conversationID = id
	}
	if t.Event.Kind != event.ContextCompressionRequested {
		inputs = append(inputs, model.Input{Role: model.User, Text: eventText(t.Event, conversationID)})
	}
	if err := checkContextSize(inputs); err != nil {
		return nil, err
	}
	return inputs, nil
}
func (e *Engine) authorize(ctx context.Context, p core.Principal, id core.ID, action policy.Action) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if e.options.Dependencies.Policy == nil {
		return core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	return e.options.Dependencies.Policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: id}, Action: action})
}
func (e *Engine) Status(ctx context.Context, p core.Principal, id core.ID) (Status, error) {
	if err := e.authorize(ctx, p, id, policy.Read); err != nil {
		return Status{}, err
	}
	status, err := persistence.Load[Status](ctx, e.store, statusBucket, persistence.Key(p.OwnerID, id))
	if err != nil {
		return status, err
	}
	if status.State == Dreaming && status.DreamDeadline != nil && !e.options.Now().Before(*status.DreamDeadline) {
		status.State = Idle
		status.DreamDeadline = nil
		status.DreamRunID = ""
	}
	boundary, err := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", persistence.Key(p.OwnerID, id))
	if err != nil && !persistence.Missing(err) {
		return status, err
	}
	if status.State != Paused && status.State != Stopped && e.options.CheckEnvironment != nil && e.options.CheckEnvironment(ctx) != nil {
		status.State = Waiting
		status.WaitingReason = "local_environment_unavailable"
	}
	status.InterruptedThroughSequence = boundary.ThroughSequence
	expedited, err := e.expediteStatus(ctx, p, id)
	if expedited.ThroughSequence != 0 {
		status.Expedite = &expedited
	}
	return status, err
}

// Lifecycle is a trusted business port. Business authorizes lifecycle commands
// before entering its transaction; this method only persists Loop state.
func (e *Engine) Lifecycle(ctx context.Context, p core.Principal, id core.ID, action string) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := id.Validate(); err != nil {
		return err
	}
	key := persistence.Key(p.OwnerID, id)
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		switch action {
		case "initialize":
			if err == nil {
				return core.NewError(core.Conflict, "assistant Loop already initialized")
			}
			current = Status{AssistantID: id, State: Idle}
		case "stop":
			if e.options.DreamStop != nil {
				if err := e.options.DreamStop(ctx, p, id); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			current.State = Stopped
			if err := tx.Delete(ctx, expediteBucket, key); err != nil && !persistence.Missing(err) {
				return err
			}
			// Cancel both already-materialized jobs and pending durable event inputs.
			// Rehiring must not resurrect an old execution identity or pending work.
			jobs, loadErr := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", p.OwnerID), persistence.Eq("Event.AssistantID", id), {Field: "State", Op: "in", Value: []scheduler.State{scheduler.Queued, scheduler.Running, scheduler.Retry, scheduler.NeedsReview}}}})
			if loadErr != nil {
				return loadErr
			}
			byID := map[core.ID]scheduler.Job{}
			for _, job := range jobs {
				if job.Event.OwnerID == p.OwnerID && job.Event.AssistantID == id {
					byID[job.Event.ID] = job
				}
			}
			// A message can still be in the IM outbox when Stop commits. Fence it
			// now so draining the outbox after a new message cannot revive old work.
			if e.options.ConversationID != nil {
				chatID, err := e.options.ConversationID(ctx, p, id)
				if err != nil {
					return err
				}
				pending, err := persistence.Select[conversation.MessageCreated](ctx, e.store, "im.outbox", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("ConversationID", chatID), persistence.Eq("Sender", conversation.User)}})
				if err != nil {
					return err
				}
				for _, input := range pending {
					if input.OwnerID != p.OwnerID || input.ConversationID != chatID || input.Sender != conversation.User {
						continue
					}
					source := scheduler.UserMessageEvent(p.OwnerID, id, conversation.Message{ID: input.MessageID, CreatedAt: input.At})
					if _, exists := byID[source.ID]; !exists {
						byID[source.ID] = scheduler.Job{Event: source, State: scheduler.Queued, NotBefore: source.CreatedAt, Version: 1}
					}
				}
			}
			for _, job := range byID {
				if job.State == scheduler.Done || job.State == scheduler.Failed || job.State == scheduler.Canceled {
					continue
				}
				job.State = scheduler.Canceled
				job.Version++
				if err := event.SaveJob(ctx, tx, job); err != nil {
					return err
				}
			}
		case "restore":
			if err != nil {
				return err
			}

			current.State = Idle
			current.ActiveTaskID = ""
			current.ExecutionID = ""
			current.WaitingReason = ""
		default:
			return core.Invalid("action", "unknown lifecycle action")
		}
		current.ModelRetry = nil
		current.TriggerMessageID = ""
		current.UpdatedAt = e.options.Now().UTC()
		return e.saveStatus(ctx, tx, key, current)
	})
	return err
}
func stableID(parts ...string) core.ID {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return core.ID(hex.EncodeToString(sum[:]))
}
func (e *Engine) Handle(ctx context.Context, t Turn) (outcome Outcome, resultErr error) {
	if err := t.Validate(); err != nil {
		return Outcome{}, err
	}
	if err := e.checkEnvironment(ctx); err != nil {
		return Outcome{}, err
	}
	if t.Event.Kind == event.UserStopped {
		return Outcome{}, core.NewError(core.FailedPrecondition, "control events do not start execution")
	}
	if err := e.authorize(ctx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
		return Outcome{}, err
	}
	if e.options.Dependencies.Models == nil || e.options.Dependencies.Tools == nil || e.options.Dependencies.Budget == nil || e.options.ResolveModel == nil || e.options.Estimate == nil {
		return Outcome{}, core.NewError(core.FailedPrecondition, "agent execution dependencies unavailable")
	}
	key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
	e.mu.Lock()
	gate := e.gates[key]
	if gate == nil {
		gate = make(chan struct{}, 1)
		e.gates[key] = gate
	}
	e.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return Outcome{}, persistence.CheckContext(ctx)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	e.active[key] = activeExecution{id: t.Principal.ExecutionID, eventID: t.Event.ID, cancel: cancel}
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.active, key); e.mu.Unlock() }()
	handledKey := persistence.Key(t.Principal.OwnerID, stableID(string(t.Event.AssistantID), string(t.Event.ID)))
	var previous Outcome
	err := e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
		err := tx.Get(ctx, "agent.handled", handledKey, &previous)
		if err == nil {
			if e.options.Acknowledge != nil {
				return e.options.Acknowledge(ctx, t)
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		boundary, boundaryErr := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", key)
		if boundaryErr != nil && !persistence.Missing(boundaryErr) {
			return boundaryErr
		}
		if boundary.Blocks(t.Event) {
			return core.NewError(core.Canceled, "event predates manual stop")
		}
		if err := e.checkContextEpoch(ctx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.CreatedAt); err != nil {
			return err
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if status.State == Stopped || status.State == Paused {
			return core.NewError(core.FailedPrecondition, "assistant execution is stopped")
		}
		if err := e.rejectSuperseded(ctx, t); err != nil {
			return err
		}
		if err := e.migrateProgress(ctx, t, status); err != nil {
			return err
		}
		if err := e.startTurnTiming(ctx, t); err != nil {
			return err
		}
		state := Working
		if t.Event.Kind == event.DreamDue {
			state = Dreaming
		}
		status = Status{Context: status.Context, AssistantID: t.Event.AssistantID, State: state, ActiveTaskID: t.Event.TaskID, ExecutionID: t.Principal.ExecutionID, UpdatedAt: e.options.Now().UTC()}
		if t.Event.Kind == event.UserMessage {
			status.TriggerMessageID = t.Event.MessageID
		}
		if err := realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(t.Event.ID), "turn-start", status.UpdatedAt.String()), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: status.UpdatedAt}); err != nil {
			return err
		}
		return e.saveStatus(ctx, tx, key, status)
	})
	if err != nil {
		return Outcome{}, err
	}
	if previous.Checkpoint.LastEventID != "" {
		return previous, nil
	}
	defer func() {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		if scheduler.IsDeferred(resultErr) {
			return
		}
		if err := e.finishTurnTiming(finishCtx, t); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	runCtx = e.options.Trace.Begin(runCtx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, t.Principal.ExecutionID, string(t.Event.Kind), t.Attempt, t.Event)
	defer func() {
		state, reason := "succeeded", ""
		if resultErr != nil {
			state = "failed"
			reason = string(core.CodeOf(resultErr))
			if r := core.ReasonOf(resultErr); r != "" {
				reason = string(r)
			}
			if core.CodeOf(resultErr) == core.Canceled {
				state = "canceled"
			}
		}
		if scheduler.IsDeferred(resultErr) {
			state = "retrying"
		}
		if resultErr == nil && outcome.Status.State == Waiting {
			state = "waiting"
			reason = outcome.Status.WaitingReason
		}
		traceCtx := runCtx
		if t.Event.Kind == event.DreamDue {
			var traceCancel context.CancelFunc
			traceCtx, traceCancel = context.WithTimeout(context.WithoutCancel(runCtx), time.Second)
			defer traceCancel()
		}
		if t.Event.Kind == event.DreamDue && resultErr != nil && !scheduler.IsDeferred(resultErr) {
			e.options.Trace.Record(traceCtx, "dream.ended", state, "", map[string]any{"Reason": reason, "FileChanges": "successful file operations retained", "Error": resultErr.Error()})
		}
		e.options.Trace.Finish(traceCtx, state, reason)
	}()
	defer func() {
		if resultErr == nil || scheduler.IsDeferred(resultErr) {
			return
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		_ = e.store.Write(finishCtx, func(ctx context.Context, tx persistence.Tx) error {
			status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
			if err != nil {
				return err
			}
			if (status.State != Working && status.State != Dreaming) || status.ExecutionID != t.Principal.ExecutionID {
				return nil
			}
			status.State = Waiting
			if status.Compression != nil && status.Compression.State == "running" {
				status.Compression.State = "failed"
			}
			status.ModelRetry = nil
			status.TriggerMessageID = ""
			if t.Event.Kind == event.DreamDue {
				status.State = Idle
				status.DreamDeadline = nil
				status.DreamRunID = ""
			}
			status.WaitingReason = string(core.CodeOf(resultErr))
			if reason := core.ReasonOf(resultErr); reason != "" {
				status.WaitingReason = string(reason)
			}
			status.UpdatedAt = e.options.Now().UTC()
			return e.saveStatus(ctx, tx, key, status)
		})
	}()

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
		if e.options.DreamPrepare == nil || e.options.DreamCommit == nil {
			return Outcome{}, core.NewError(core.Unavailable, "dream processor unavailable")
		}
		evidence, prepareErr := e.options.DreamPrepare(runCtx, t.Principal, t.Event)
		if prepareErr != nil {
			return Outcome{}, prepareErr
		}
		runCtx = tools.WithVisibleTools(runCtx, e.options.DreamTools)
		if e.options.DreamContext != nil {
			runCtx = e.options.DreamContext(runCtx, t.Principal, t.Event)
		}
		definitions, err := e.options.Dependencies.Tools.Definitions(runCtx, t.Principal)
		if err != nil {
			return Outcome{}, err
		}
		for _, d := range definitions {
			progress.Definitions = append(progress.Definitions, model.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
		}
		progress.Inputs = []model.Input{
			{Role: model.System, Text: prompt.Text("dream", nil)},
			{Role: model.User, Text: prompt.Text("dream-evidence", evidence)},
		}
		previous, loadErr := persistence.Load[turnProgress](runCtx, e.store, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID))
		if loadErr == nil {
			progress = previous
		} else if !persistence.Missing(loadErr) {
			return Outcome{}, loadErr
		}
		if e.options.DreamOutput != nil {
			saved, err := e.options.DreamOutput(runCtx, t.Principal, t.Event)
			if err != nil {
				return Outcome{}, err
			}
			if saved != "" {
				progress.Inputs = []model.Input{{Role: model.System, Text: prompt.Text("dream", nil)}, {Role: model.User, Text: prompt.Text("dream-evidence", evidence)}, {Role: model.Assistant, Text: saved}}
				progress.Finished = true
				progress.Response = nil
			}
		}
	} else {
		checkpoint, progress, err = e.prepareExecution(runCtx, t)
		if err != nil {
			return Outcome{}, err
		}
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
		if err = e.saveProgress(runCtx, t, &progress); err != nil {
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
	state := Idle
	waiting := ""
resume:
	if t.Event.Kind != event.DreamDue && progress.Response == nil && progress.Finished {
		if err := e.intakeEvents(runCtx, t, &progress); err != nil {
			return Outcome{}, err
		}
		if interrupted, err := e.prepareContext(runCtx, t, modelID, &progress); err != nil {
			return Outcome{}, err
		} else if interrupted {
			goto resume
		}
	}
	for !progress.Finished && progress.Step < e.options.MaxSteps {
		if err := e.authorize(runCtx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return Outcome{}, err
		}
		execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: modelOperationID(t, progress.Step)}
		if progress.Response == nil {
			e.setPhase(key, "preparing")
			_, receiptErr := model.NewReceipts(e.store).ByOperation(runCtx, t.Principal.OwnerID, execution.OperationID)
			if receiptErr != nil && !persistence.Missing(receiptErr) {
				return Outcome{}, receiptErr
			}
			if persistence.Missing(receiptErr) && pendingRetry == "" && t.Event.Kind != event.DreamDue {
				if err := e.intakeEvents(runCtx, t, &progress); err != nil {
					return Outcome{}, err
				}
				progress.Inputs, progress.Definitions, progress.Automatic, err = e.refreshContext(runCtx, execution, progress.Inputs, progress.Automatic)
				if err != nil {
					return Outcome{}, err
				}
			}
			if persistence.Missing(receiptErr) && t.Event.Kind != event.DreamDue {
				if err := e.saveProgress(runCtx, t, &progress); err != nil {
					return Outcome{}, err
				}
				interrupted, compactErr := e.prepareContext(runCtx, t, modelID, &progress)
				if compactErr != nil {
					return Outcome{}, compactErr
				}
				if interrupted {
					pendingRetry = ""
					continue
				}
			}
			// A committed model response may precede its local progress write.
			// Preserve that request's exact inputs/definitions for replay; a
			// pending expedite will supersede it before any tools are started.

			if err := e.saveProgress(runCtx, t, &progress); err != nil {
				return Outcome{}, err
			}
			response, interrupted, err := e.generateStep(runCtx, t, modelID, progress.Definitions, progress)
			if err != nil {
				return Outcome{}, err
			}
			if interrupted {
				pendingRetry = ""
				progress.Step++
				if err := e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
					if err := e.saveProgress(ctx, t, &progress); err != nil {
						return err
					}
					return tx.Put(ctx, "agent.superseded_models", persistence.Key(t.Principal.OwnerID, execution.OperationID), true)
				}); err != nil {
					return Outcome{}, err
				}
				continue
			}
			pendingRetry = ""
			progress.Handled = append([]core.ID{}, progress.Joined...)
			progress.Usage = e.measureContext(runCtx, t, modelID, progress)
			progress.Inputs = append(progress.Inputs, response)
			if t.Event.Kind != event.DreamDue && progress.Usage != nil {
				limit, err := e.contextWindow(runCtx, t, modelID)
				if err != nil {
					return Outcome{}, err
				}
				tokens, source := contextTokens(progress, modelID, progress.Usage.Source)
				if err = e.recordContextStatus(runCtx, t, ContextStatus{ModelID: modelID, Limit: limit, Tokens: tokens, Source: source, CompressionThreshold: e.options.CompressionThreshold}); err != nil {
					return Outcome{}, err
				}
			}
			progress.Response = &response
			if err := e.saveProgress(runCtx, t, &progress); err != nil {
				return Outcome{}, err
			}
		}
		if err := e.detectCompression(runCtx, t, modelID, &progress); err != nil {
			return Outcome{}, err
		}
		response := *progress.Response

		skipped := false
		for progress.NextTool < len(response.Calls) {
			call := response.Calls[progress.NextTool]
			if err := runCtx.Err(); err != nil {
				return Outcome{}, err
			}
			pending, err := e.pendingInterrupts(runCtx, t, progress.Joined)
			if err != nil {
				return Outcome{}, err
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
					return Outcome{}, loadErr
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
				if err := e.checkEnvironment(runCtx); err != nil {
					return Outcome{}, err
				}
				e.setPhase(key, "tool")
				if call.Name == "conversation_publish" {
					progress.PublishAttempted = true
				}
				e.options.Trace.Record(runCtx, "tool.start", "started", toolExecution.OperationID, map[string]any{"Name": call.Name, "CallID": call.ID, "Arguments": call.Arguments})
				result, err = e.options.Dependencies.Tools.Execute(runCtx, invocation)
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
				e.options.Trace.Record(runCtx, "tool.complete", toolState, toolExecution.OperationID, map[string]any{"Name": call.Name, "CallID": call.ID, "Result": result, "Code": code, "Reason": reason})
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
					return Outcome{}, err
				} // durable tool receipt prevents replaying effects
			}
			progress.Inputs = append(progress.Inputs, model.Input{ToolOperationID: toolExecution.OperationID, Role: model.Tool, Text: string(result.Content), Media: result.Media, ReplyTo: call.ID, ReadMessageIDs: readIDs})
			progress.NextTool++
			if err := e.saveProgress(runCtx, t, &progress); err != nil {
				return Outcome{}, err
			}
		}
		progress.Response = nil
		progress.NextTool = 0
		progress.Step++
		progress.Finished = len(response.Calls) == 0 || progress.PermissionPending
		// Continue a silent user-message turn once, using the normal step,
		// budget and cancellation limits. Persist the reminder with its input
		// so replay, retries and context compaction cannot replenish it.
		if progress.Finished && !progress.PermissionPending && !progress.PublishAttempted && !progress.PublishReminded && (t.Event.Kind == event.UserMessage || len(progress.Joined) > 0) {
			progress.Inputs = append(progress.Inputs, model.Input{Role: model.User, Text: prompt.Text("publish-reminder", nil)})
			progress.PublishReminded = true
			progress.Finished = false
		}
		if err := e.saveProgress(runCtx, t, &progress); err != nil {
			return Outcome{}, err
		}
		if t.Event.Kind != event.DreamDue {
			if err := e.intakeEvents(runCtx, t, &progress); err != nil {
				return Outcome{}, err
			}
			if interrupted, err := e.prepareContext(runCtx, t, modelID, &progress); err != nil {
				return Outcome{}, err
			} else if interrupted {
				progress.Finished = false
				continue
			}
		}
		if progress.Finished {
			break
		}
	}
	if progress.PermissionPending {
		state = Waiting
		waiting = "permission_pending"
	}
	if !progress.Finished && progress.Step >= e.options.MaxSteps {
		state = Waiting
		waiting = "step_limit"
	}
	inputs, automatic := progress.Inputs, progress.Automatic

	e.options.Trace.Record(runCtx, "context.final", "succeeded", "", map[string]any{"Inputs": inputs})
	private, err := json.Marshal(privateContext{Inputs: contextHistory(inputs), Automatic: automatic, Usage: progress.Usage})
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Status: Status{AssistantID: t.Event.AssistantID, State: state, ActiveTaskID: t.Event.TaskID, ExecutionID: t.Principal.ExecutionID, WaitingReason: waiting, UpdatedAt: e.options.Now().UTC()}, Checkpoint: Checkpoint{OwnerID: t.Principal.OwnerID, AssistantID: t.Event.AssistantID, ExecutionID: t.Principal.ExecutionID, FormatVersion: 1, Sequence: checkpoint.Sequence + 1, Context: private, LastEventID: t.Event.ID, SavedAt: e.options.Now().UTC()}}
	// Dream files have their own durable commit plan. Never place filesystem
	// effects inside the status/checkpoint transaction: its rollback cannot undo files.
	if t.Event.Kind == event.DreamDue {
		if !progress.Finished {
			return Outcome{}, core.NewError(core.FailedPrecondition, "dream incomplete")
		}
		if err := e.authorize(runCtx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return Outcome{}, err
		}
		if err := e.options.DreamCommit(runCtx, t.Principal, t.Event, progress.Inputs[len(progress.Inputs)-1].Text); err != nil {
			return Outcome{}, err
		}
	}
	err = e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.authorize(ctx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return err
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.State == Stopped || status.State == Paused || status.ExecutionID != t.Principal.ExecutionID {
			return core.NewError(core.Canceled, "assistant stopped before checkpoint commit")
		}
		pending, err := e.pendingExpedite(ctx, t, progress.Joined)
		if err != nil {
			return err
		}
		if len(pending) > 0 && progress.Step < e.options.MaxSteps {
			return errPendingExpedite
		}
		pendingEvents, err := e.hasBoundaryEvents(ctx, t)
		if err != nil {
			return err
		}
		if pendingEvents && progress.Step < e.options.MaxSteps {
			return errPendingExpedite
		}
		out.Status.Context = status.Context
		out.Status.Compression = status.Compression
		if t.Event.Kind != event.DreamDue {
			if err := e.finishExpedite(ctx, t, progress); err != nil {
				return err
			}
		}
		if t.Event.Kind == event.DreamDue {
			if e.options.DreamActive != nil {
				active, err := e.options.DreamActive(ctx, t.Principal, t.Event)
				if err != nil {
					return err
				}
				if active {
					out.Status.State = Dreaming
					out.Status.DreamDeadline = status.DreamDeadline
					out.Status.DreamRunID = status.DreamRunID
				}
			}
		} else {
			latest, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
			if err != nil {
				return err
			}
			if latest.Sequence != progress.Revision {
				return core.NewError(core.Conflict, "execution state advanced before completion")
			}
			saved, err := decodeState(latest)
			if err != nil {
				return err
			}
			saved.Inputs, saved.Automatic, saved.Usage, saved.Current = contextHistory(inputs), automatic, progress.Usage, nil
			out.Checkpoint.Context, err = json.Marshal(saved)
			if err != nil {
				return err
			}
			out.Checkpoint.Sequence = latest.Sequence + 1
			if err = e.checkpoints.Save(checkpointCause(ctx, "turn_completed"), out.Checkpoint, latest.Sequence); err != nil {
				return err
			}
		}
		if err := e.saveStatus(ctx, tx, key, out.Status); err != nil {
			return err
		}
		if t.Event.Kind == event.DreamDue {
			if err := realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(t.Event.ID), "dream-end"), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: e.options.Now().UTC()}); err != nil {
				return err
			}
		}
		if err := tx.Put(ctx, "agent.handled", handledKey, out); err != nil {
			return err
		}
		if e.options.Acknowledge != nil {
			return e.options.Acknowledge(ctx, t)
		}
		return nil
	})
	if err == errPendingExpedite {
		goto resume
	}
	if err == nil && t.Event.Kind == event.DreamDue {
		e.options.Trace.Record(runCtx, "dream.batch_committed", "succeeded", "", map[string]any{"Sequence": out.Checkpoint.Sequence, "SavedAt": out.Checkpoint.SavedAt})
	}
	return out, err
}

func (e *Engine) checkEnvironment(ctx context.Context) error {
	if e.options.CheckEnvironment != nil {
		if err := e.options.CheckEnvironment(ctx); err != nil {
			return &scheduler.Deferred{At: e.options.Now().Add(5 * time.Second), Cause: err}
		}
	}
	return nil
}
