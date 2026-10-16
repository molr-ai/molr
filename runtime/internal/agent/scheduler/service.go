package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type Options struct {
	// Ready is checked outside transactions; false retains queued jobs without attempts.
	Ready        func(context.Context) bool
	Authorizer   policy.Authorizer
	Now          func() time.Time
	Concurrency  int
	TickInterval time.Duration
	Handle       func(context.Context, Claim) error
	// DrainMessages consumes the IM outbox after committed message arrivals.
	DrainMessages func(context.Context) error
	// Tick persists due schedule occurrences; it does not publish virtual messages.
	Tick      func(context.Context, time.Time) error
	Reconcile func(context.Context, Job) (State, error)
	// RecoverInterrupted atomically hands ordinary work to a new resume event.
	// false leaves specialized jobs (dream batches) to normal reconciliation.
	RecoverInterrupted func(context.Context, Job) (bool, error)
	Conversations      conversation.Repository
}
type activeClaim struct {
	cancel  context.CancelFunc
	version core.Version
}
type service struct {
	store   persistence.Store
	queue   *queue
	options Options
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	active  map[string]activeClaim
}

func NewService(store persistence.Store, options Options) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.TickInterval <= 0 {
		options.TickInterval = time.Second
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 2
	}
	if options.Concurrency > 32 {
		options.Concurrency = 32
	}
	return &service{store: store, queue: &queue{store: store}, options: options, active: map[string]activeClaim{}}
}
func (s *service) authorize(ctx context.Context, p core.Principal, action policy.Action) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "scheduler", ID: p.OwnerID}, Action: action})
}
func (s *service) Jobs(ctx context.Context, p core.Principal, q Query) (core.Page[Job], error) {
	if err := s.authorize(ctx, p, policy.Read); err != nil {
		return core.Page[Job]{}, err
	}
	return s.queue.List(ctx, p.OwnerID, q)
}
func (s *service) Cancel(ctx context.Context, p core.Principal, id core.ID) error {
	if err := s.authorize(ctx, p, policy.Write); err != nil {
		return err
	}
	if err := s.queue.Cancel(ctx, p.OwnerID, id); err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.active[persistence.Key(p.OwnerID, id)].cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}
func (s *service) Run(ctx context.Context) error {
	if s.options.Handle == nil {
		return core.NewError(core.FailedPrecondition, "agent runtime required")
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return core.NewError(core.Conflict, "scheduler already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.cancel = nil; close(done); s.mu.Unlock() }()
	if err := s.recover(runCtx); err != nil {
		return err
	}
	var workers sync.WaitGroup
	failure := make(chan error, s.options.Concurrency+2)
	if s.options.DrainMessages != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			watch, err := s.store.Watch(conversation.OutboxSignal())
			if err != nil {
				failure <- err
				return
			}
			defer watch.Close()
			for {
				consumeWake(watch)
				if err := s.options.DrainMessages(runCtx); err != nil {
					if runCtx.Err() == nil {
						failure <- err
					}
					return
				}
				if err := waitForWork(runCtx, watch, time.Time{}, s.options.Now()); err != nil {
					if runCtx.Err() == nil {
						failure <- err
					}
					return
				}
			}
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(s.options.TickInterval)
		defer ticker.Stop()
		for {
			if s.options.Tick != nil {
				if err := s.options.Tick(runCtx, s.options.Now()); err != nil && runCtx.Err() == nil {
					select {
					case failure <- err:
					default:
					}
					return
				}
			}
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for range s.options.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			watch, err := s.store.Watch(event.DispatchSignal())
			if err != nil {
				failure <- err
				return
			}
			defer watch.Close()
			for {
				// Register before reading and consume notifications only before the query.
				consumeWake(watch)
				if runCtx.Err() != nil {
					return
				}
				if s.options.Ready != nil && !s.options.Ready(runCtx) {
					if err := waitForWork(runCtx, watch, s.options.Now().Add(time.Second), s.options.Now()); err != nil {
						if runCtx.Err() == nil {
							failure <- err
						}
						return
					}
					continue
				}
				claim, next, err := s.queue.claim(runCtx, s.options.Now())
				if err != nil {
					if runCtx.Err() != nil {
						return
					}
					if core.CodeOf(err) != core.NotFound {
						select {
						case failure <- err:
						default:
						}
						return
					}
					if err := waitForWork(runCtx, watch, next, s.options.Now()); err != nil {
						if runCtx.Err() == nil {
							failure <- err
						}
						return
					}
					continue
				}
				// A service shutdown is not an explicit user/job cancellation.
				turnCtx, stopCause := context.WithCancelCause(context.WithoutCancel(runCtx))
				stopWatch := context.AfterFunc(runCtx, func() { stopCause(ErrRuntimeInterrupted) })
				stop := func() { stopCause(context.Canceled) }
				s.mu.Lock()
				s.active[persistence.Key(claim.Job.Event.OwnerID, claim.Job.Event.ID)] = activeClaim{stop, claim.Job.Version}
				s.mu.Unlock()
				err = s.options.Handle(turnCtx, claim)
				interrupted := errors.Is(context.Cause(turnCtx), ErrRuntimeInterrupted)
				stopWatch()
				stop()
				s.mu.Lock()
				if key := persistence.Key(claim.Job.Event.OwnerID, claim.Job.Event.ID); s.active[key].version == claim.Job.Version {
					delete(s.active, key)
				}
				s.mu.Unlock()
				// A Runtime with an atomic checkpoint+Ack hook may already have marked Done.
				finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
				finishErr := s.store.Write(finishCtx, func(ctx context.Context, tx persistence.Tx) error {
					current, e := persistence.Load[Job](ctx, s.store, jobsBucket, persistence.Key(claim.Job.Event.OwnerID, claim.Job.Event.ID))
					if e != nil {
						return e
					}
					if current.State != Running || current.Version != claim.Job.Version {
						return nil
					}
					if err == nil {
						return s.queue.Ack(ctx, claim.Principal, current.Event.ID, current.Version)
					}
					if interrupted && !IsDeferred(err) {
						at := s.options.Now().UTC()
						current.InterruptedAt = &at
						current.Version++
						// Leave Running for startup reconciliation; never re-claim in this process.
						return saveJob(ctx, tx, current)
					}
					// A failure may follow an external effect. Recovery examines its durable
					// evidence before deciding whether another attempt is safe.
					if IsDeferred(err) {
						var deferred *Deferred
						errors.As(err, &deferred)
						return s.queue.Retry(ctx, claim.Principal, RetryRequest{JobID: current.Event.ID, ExpectedVersion: current.Version, NotBefore: deferred.At, FailureCode: core.CodeOf(err)})
					}
					current.State = failureState(err)
					current.FailureCode = core.CodeOf(err)
					current.Version++
					return saveJob(ctx, tx, current)
				})
				finishCancel()
				if finishErr != nil {
					select {
					case failure <- finishErr:
					default:
					}
					return
				}
			}
		}()
	}
	var err error
	select {
	case <-runCtx.Done():
	case err = <-failure:
		cancel()
	}
	workers.Wait()
	return err
}
func (s *service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	}
}
func (s *service) Recover(ctx context.Context) error {
	s.mu.Lock()
	running := s.cancel != nil
	s.mu.Unlock()
	if running {
		return core.NewError(core.FailedPrecondition, "recovery requires stopped workers")
	}
	return s.recover(ctx)
}
func (s *service) recover(ctx context.Context) error {
	jobs, err := persistence.Select[Job](ctx, s.store, jobsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", Running)}})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if s.options.RecoverInterrupted != nil {
			handled, err := s.options.RecoverInterrupted(ctx, job)
			if err != nil {
				return err
			}
			if handled {
				continue
			}
		}
		if job.State != Running {
			continue
		}
		state := Retry
		if s.options.Reconcile != nil {
			state, err = s.options.Reconcile(ctx, job)
			if err != nil {
				return err
			}
		} else {
			operations, err := persistence.Select[operation.Operation](ctx, s.store, "agent.operations", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", job.Event.OwnerID), persistence.Eq("ExecutionID", job.ExecutionID)}})
			if err != nil {
				return err
			}
			for _, op := range operations {
				if op.OwnerID == job.Event.OwnerID && op.ExecutionID == job.ExecutionID && (op.State == operation.Running || op.State == operation.Unknown) {
					// A deliberately superseded model call can retain unknown cost,
					// but it executed no tools. Its successor is durably recorded.
					if op.Action == "model.generate" {
						superseded, loadErr := persistence.Load[bool](ctx, s.store, "agent.superseded_models", persistence.Key(op.OwnerID, op.ID))
						if loadErr != nil && !persistence.Missing(loadErr) {
							return loadErr
						}
						if superseded {
							continue
						}
					}
					state = NeedsReview
				}
			}
		}
		if state != Retry && state != NeedsReview && state != Done && state != Canceled {
			return core.Invalid("recovery", "invalid recovery decision")
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			current, e := persistence.Load[Job](ctx, s.store, jobsBucket, persistence.Key(job.Event.OwnerID, job.Event.ID))
			if e != nil {
				return e
			}
			if current.Version != job.Version || current.State != Running {
				return core.NewError(core.Conflict, "recovery job changed")
			}
			current.State = state
			current.Version++
			if state == Retry {
				current.NotBefore = s.options.Now()
			}
			return saveJob(ctx, tx, current)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type messageConsumer struct {
	store         persistence.Store
	conversations conversation.Repository
	outbox        conversation.Outbox
	events        event.Writer
}

func NewMessageConsumer(store persistence.Store, options Options) MessageConsumer {
	repo := options.Conversations
	if repo == nil {
		repo = conversation.NewRepository(store)
	}
	return &messageConsumer{store, repo, conversation.NewOutbox(store), event.NewWriter(store)}
}
func (m *messageConsumer) AcceptMessage(ctx context.Context, e conversation.MessageCreated) error {
	if e.OwnerID.Validate() != nil || e.ConversationID.Validate() != nil || e.ID.Validate() != nil {
		return core.Invalid("message_event", "invalid event")
	}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := m.conversations.Get(ctx, e.OwnerID, e.ConversationID)
		if err != nil {
			return err
		}
		if e.Sender == conversation.User && e.SenderID != e.OwnerID {
			return core.NewError(core.Forbidden, "user sender mismatch")
		}
		if e.Sender == conversation.Assistant && e.SenderID != c.PeerID {
			return core.NewError(core.Forbidden, "assistant sender mismatch")
		}
		if e.Sender != conversation.User && e.Sender != conversation.Assistant {
			return core.Invalid("sender", "invalid sender")
		}
		if e.Sender == conversation.User && !c.Archived {
			input := UserMessageEvent(e.OwnerID, c.PeerID, conversation.Message{ID: e.MessageID, Sequence: e.Sequence, CreatedAt: e.At})
			if err = m.events.AppendAssistant(ctx, input); err != nil {
				return err
			}
		}
		return m.outbox.Acknowledge(ctx, e.ID)
	})
}

// UserMessageEvent shares the durable event identity with expedited delivery,
// including when the IM outbox has not yet been drained.
func UserMessageEvent(owner, assistant core.ID, m conversation.Message) event.AssistantEvent {
	sum := sha256.Sum256([]byte(string(owner) + "/" + string(assistant) + "/" + string(m.ID)))
	return event.AssistantEvent{ID: core.ID(hex.EncodeToString(sum[:])), OwnerID: owner, AssistantID: assistant, Kind: event.UserMessage, DeliveryMode: event.WhenIdle, MessageID: m.ID, MessageSequence: m.Sequence, CreatedAt: m.CreatedAt}
}

func consumeWake(w persistence.Watch) {
	select {
	case <-w.Changes():
	default:
	}
}

func waitForWork(ctx context.Context, w persistence.Watch, at, now time.Time) error {
	var deadline <-chan time.Time
	if !at.IsZero() {
		timer := time.NewTimer(at.Sub(now))
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	case <-w.Done():
		return core.NewError(core.Unavailable, "scheduler event store closed")
	case <-w.Changes():
		return nil
	case <-deadline:
		return nil
	}
}
