package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

var loopInstructions = prompt.Text("loop", nil)

type Options struct {
	RecoveryAllowed     func(context.Context, Turn) (bool, error)
	ModelSource         func(context.Context, core.Principal, core.ID) (model.StateSource, error)
	CheckEnvironment    func(context.Context) error
	ModelRetry          RetryPolicy
	ContextWindow       func(context.Context, core.Principal, core.ID) (int, error)
	OutputLimit         func(context.Context, core.Principal, core.ID) (int, error)
	CompressionTokens   func(context.Context, core.Principal, core.ID) (int, error)
	PrepareToolResult   func(context.Context, core.Execution, tools.Result) (tools.Result, error)
	DreamDeadline       func(context.Context, core.Principal, event.AssistantEvent) (time.Time, error)
	DreamActive         func(context.Context, core.Principal, event.AssistantEvent) (bool, error)
	DreamStop           func(context.Context, core.Principal, core.ID) error
	DreamOutput         func(context.Context, core.Principal, event.AssistantEvent) (string, error)
	DreamTools          []string
	DreamContext        func(context.Context, core.Principal, event.AssistantEvent) context.Context
	DreamPrepare        func(context.Context, core.Principal, event.AssistantEvent) (string, error)
	DreamCommit         func(context.Context, core.Principal, event.AssistantEvent, string) error
	Conversations       conversation.Service
	ContextSources      func(context.Context, ContextRequest) ([]ContextDocument, error)
	InspectCapabilities func(context.Context, core.Principal, core.ID) (Capabilities, error)
	Trace               *debugtrace.Service
	Dependencies        Dependencies
	ResolveModel        func(context.Context, core.Principal, core.ID) (core.ID, error)
	Estimate            func(context.Context, core.ID, []model.Input, int) (core.Money, error)
	ConversationID      func(context.Context, core.Principal, core.ID) (core.ID, error)
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
			inputs = append([]model.Input(nil), saved.Inputs...)
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
			if err := releaseRetries(ctx, tx, p.OwnerID, id, ""); err != nil {
				return err
			}
			if err := contextstore.ReleaseKind(ctx, tx, p.OwnerID, id, "dream_progress"); err != nil {
				return err
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

func (e *Engine) checkEnvironment(ctx context.Context) error {
	if e.options.CheckEnvironment != nil {
		if err := e.options.CheckEnvironment(ctx); err != nil {
			return &scheduler.Deferred{At: e.options.Now().Add(5 * time.Second), Cause: err}
		}
	}
	return nil
}
