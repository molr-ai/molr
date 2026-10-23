package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sort"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

const jobsBucket = "agent.jobs"

type queue struct{ store persistence.Store }

func NewQueue(store persistence.Store) Queue { return &queue{store} }

// boundaries is reused only within the caller's transaction; nil reads fresh state.
func visibleJob(ctx context.Context, store persistence.Store, job Job, boundaries map[string]event.InterruptBoundary) (Job, error) {
	job.Event.DeliveryMode = job.Event.Delivery()
	if job.State == Done || job.State == Canceled || job.State == Interrupted {
		return job, nil
	}
	key := persistence.Key(job.Event.OwnerID, job.Event.AssistantID)
	boundary, known := boundaries[key]
	if !known {
		var err error
		boundary, err = persistence.Load[event.InterruptBoundary](ctx, store, "agent.interrupt_boundaries", key)
		if err != nil && !persistence.Missing(err) {
			return job, err
		}
		if boundaries != nil {
			boundaries[key] = boundary
		}
	}
	if boundary.Blocks(job.Event) {
		job.State = Canceled
	}
	return job, nil
}
func (q *queue) Claim(ctx context.Context, now time.Time) (Claim, error) {
	claim, _, err := q.claim(ctx, now)
	return claim, err
}

// claim reads only active candidates in bounded pages. next is the earliest
// time at which a currently blocked candidate can become eligible without a write.
func (q *queue) claim(ctx context.Context, now time.Time) (claim Claim, next time.Time, err error) {
	remember := func(at time.Time) {
		if at.After(now) && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	err = q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		type dreamSession struct {
			ID, OwnerID, AssistantID core.ID
			State                    string
			Deadline                 time.Time
			Started                  bool
		}
		type assistantState struct {
			boundary event.InterruptBoundary
			running  bool
			retry    core.ID
			dream    dreamSession
		}
		states := map[string]assistantState{}
		after := ""
		for {
			// Read each state through its own ordered index, then merge at most
			// two pages. An OR/IN query would sort the whole active queue.
			var jobs []Job
			for _, status := range []State{Queued, Retry} {
				query := persistence.Query{OrderBy: "QueueKey", Limit: 100, Where: []persistence.Filter{persistence.Eq("State", status)}}
				if after != "" {
					query.Where = append(query.Where, persistence.Filter{Field: "QueueKey", Op: ">", Value: after})
				}
				page, err := persistence.Select[Job](ctx, q.store, jobsBucket, query)
				if err != nil {
					return err
				}
				jobs = append(jobs, page...)
			}
			sort.Slice(jobs, func(i, j int) bool { return jobs[i].QueueKey < jobs[j].QueueKey })
			if len(jobs) > 100 {
				jobs = jobs[:100]
			}

			for _, job := range jobs {
				after = job.QueueKey
				key := persistence.Key(job.Event.OwnerID, job.Event.AssistantID)
				state, known := states[key]
				if !known {
					state.boundary, err = persistence.Load[event.InterruptBoundary](ctx, q.store, "agent.interrupt_boundaries", key)
					if err != nil && !persistence.Missing(err) {
						return err
					}
					for _, status := range []State{Running, Retry} {
						active, err := persistence.Select[Job](ctx, q.store, jobsBucket, persistence.Query{
							Where: []persistence.Filter{persistence.Eq("Event.OwnerID", job.Event.OwnerID), persistence.Eq("Event.AssistantID", job.Event.AssistantID), persistence.Eq("State", status)}, OrderBy: "QueueKey"})
						if err != nil {
							return err
						}
						for _, j := range active {
							if state.boundary.Blocks(j.Event) {
								continue
							}
							if status == Running {
								state.running = true
							} else {
								state.retry = j.Event.ID
							}
						}
					}
					dreams, err := persistence.Select[dreamSession](ctx, q.store, "business.dream_sessions_index", persistence.Query{
						Where: []persistence.Filter{persistence.Eq("OwnerID", job.Event.OwnerID), persistence.Eq("AssistantID", job.Event.AssistantID), persistence.Eq("State", "active")}})
					if err != nil {
						return err
					}
					for _, dream := range dreams {
						if dream.Started && now.Before(dream.Deadline) {
							state.dream = dream
						}
					}
					states[key] = state
				}
				if state.boundary.Blocks(job.Event) || job.Event.Kind == event.UserStopped {
					job.State = Canceled
					job.Version++
					if err := saveJob(ctx, tx, job); err != nil {
						return err
					}
					continue
				}
				if job.JoinedTo != "" || state.running || (state.retry != "" && state.retry != job.Event.ID) {
					continue
				}
				var sessionID core.ID
				if job.Event.Kind == event.DreamDue {
					var run struct{ SessionID core.ID }
					err := tx.Get(ctx, "business.dream_run_sessions", persistence.Key(job.Event.OwnerID, job.Event.OperationID), &run)
					if err != nil && !persistence.Missing(err) {
						return err
					}
					sessionID = run.SessionID
					if sessionID != "" {
						var session dreamSession
						err := tx.Get(ctx, "business.dream_sessions_index", persistence.Key(job.Event.OwnerID, sessionID), &session)
						if err != nil && !persistence.Missing(err) {
							return err
						}
						if err == nil && (session.State != "active" || !now.Before(session.Deadline)) {
							job.State = Canceled
							job.Version++
							if err := saveJob(ctx, tx, job); err != nil {
								return err
							}
							continue
						}
					}
				}
				if state.dream.ID != "" && (job.Event.Kind != event.DreamDue || sessionID != state.dream.ID) {
					remember(state.dream.Deadline)
					continue
				}
				if job.NotBefore.After(now) {
					remember(job.NotBefore)
					continue
				}
				id := make([]byte, 16)
				if _, err := rand.Read(id); err != nil {
					return err
				}
				job.State = Running
				if job.ExecutionID == "" {
					job.ExecutionID = core.ID(hex.EncodeToString(id))
				}
				job.Attempts++
				job.Version++
				if err := saveJob(ctx, tx, job); err != nil {
					return err
				}
				claim = Claim{Job: job, Principal: core.Principal{OwnerID: job.Event.OwnerID, SubjectID: job.Event.AssistantID, Kind: core.ActorAssistant, ExecutionID: job.ExecutionID}}
				return nil
			}
			if len(jobs) < 100 {
				return nil
			}
		}
	})
	if err == nil && claim.Job.Event.ID == "" {
		err = core.NewError(core.NotFound, "no eligible job")
	}
	return
}
func bound(ctx context.Context, tx persistence.Tx, p core.Principal, id core.ID, version core.Version) (Job, error) {
	var job Job
	if err := p.Validate(); err != nil {
		return job, err
	}
	if p.Kind != core.ActorAssistant {
		return job, core.NewError(core.Forbidden, "assistant identity required")
	}
	if err := tx.Get(ctx, jobsBucket, persistence.Key(p.OwnerID, id), &job); err != nil {
		return job, err
	}
	if job.ExecutionID != p.ExecutionID || job.Event.AssistantID != p.SubjectID {
		return job, core.NewError(core.Forbidden, "job execution mismatch")
	}
	if job.Version != version {
		return job, core.NewError(core.Conflict, "job version changed")
	}
	if job.State != Running {
		return job, core.NewError(core.FailedPrecondition, "job is not running")
	}
	return job, nil
}
func (q *queue) Ack(ctx context.Context, p core.Principal, id core.ID, version core.Version) error {
	return q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		job, err := bound(ctx, tx, p, id, version)
		if err != nil {
			return err
		}
		job.State = Done
		job.Version++
		return saveJob(ctx, tx, job)
	})
}
func (q *queue) Retry(ctx context.Context, p core.Principal, r RetryRequest) error {
	return q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		job, err := bound(ctx, tx, p, r.JobID, r.ExpectedVersion)
		if err != nil {
			return err
		}
		job.State = Retry
		job.NotBefore = r.NotBefore
		job.FailureCode = r.FailureCode
		job.Version++
		return saveJob(ctx, tx, job)
	})
}
func (q *queue) List(ctx context.Context, owner core.ID, r Query) (page core.Page[Job], err error) {
	filters := []persistence.Filter{persistence.Eq("Event.OwnerID", owner)}
	if r.AssistantID != "" {
		filters = append(filters, persistence.Eq("Event.AssistantID", r.AssistantID))
	}
	query := persistence.Query{Where: filters}
	// Only cancellation changes a stored state when applying a stop boundary.
	// Queries that include canceled jobs must also inspect other stored states.
	if len(r.States) > 0 && !slices.Contains(r.States, Canceled) {
		// Select candidate keys first so key ordering cannot favor a history scan.
		query.Any = [][]persistence.Filter{{{Field: "State", Op: "in", Value: r.States}}}
	}
	request := r.Page
	r.Page = core.PageRequest{}
	err = q.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var readErr error
		normalized := map[core.ID]Job{}
		boundaries := map[string]event.InterruptBoundary{}
		page, readErr = persistence.Page[Job](ctx, q.store, jobsBucket, query, request, r, func(raw Job) bool {
			job, e := visibleJob(ctx, q.store, raw, boundaries)
			if e != nil {
				err = e
				return true
			}
			if len(r.States) == 0 {
				normalized[job.Event.ID] = job
				return true
			}
			for _, state := range r.States {
				if state == job.State {
					normalized[job.Event.ID] = job
					return true
				}
			}
			return false
		})
		if err != nil {
			return err
		}
		for i, job := range page.Items {
			page.Items[i] = normalized[job.Event.ID]
		}
		return readErr
	})
	return
}
func (q *queue) Cancel(ctx context.Context, owner, id core.ID) error {
	return q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		job, err := persistence.Load[Job](ctx, q.store, jobsBucket, persistence.Key(owner, id))
		if err != nil {
			return err
		}
		job, err = visibleJob(ctx, q.store, job, nil)
		if err != nil {
			return err
		}
		if job.State == Done {
			return core.NewError(core.FailedPrecondition, "job already complete")
		}
		if job.State == Canceled {
			return nil
		}
		job.State = Canceled
		job.Version++
		return saveJob(ctx, tx, job)
	})
}

// Pending returns ordered, unclaimed inputs eligible to join the current turn.
// Call inside the checkpoint transaction when consuming; the scheduler uses the
// same job records and stop boundaries, preventing double dispatch.
func Pending(ctx context.Context, store persistence.Store, owner, assistant, current core.ID, now time.Time, interruptOnly bool) ([]Job, error) {
	jobs, _, err := PendingWithDeadline(ctx, store, owner, assistant, current, now, interruptOnly)
	return jobs, err
}

// PendingWithDeadline also returns the earliest future eligible input time, so
// consumers can wait for that deadline without polling or requiring another write.
func PendingWithDeadline(ctx context.Context, store persistence.Store, owner, assistant, current core.ID, now time.Time, interruptOnly bool) ([]Job, time.Time, error) {
	jobs, err := persistence.Select[Job](ctx, store, jobsBucket, persistence.Query{Where: []persistence.Filter{
		persistence.Eq("Event.OwnerID", owner), persistence.Eq("Event.AssistantID", assistant), persistence.Eq("State", Queued)}, OrderBy: "QueueKey"})
	if err != nil {
		return nil, time.Time{}, err
	}
	boundary, err := persistence.Load[event.InterruptBoundary](ctx, store, "agent.interrupt_boundaries", persistence.Key(owner, assistant))
	if err != nil && !persistence.Missing(err) {
		return nil, time.Time{}, err
	}
	var out []Job
	var next time.Time
	for _, job := range jobs {
		if boundary.Blocks(job.Event) || job.Event.OwnerID != owner || job.Event.AssistantID != assistant || job.Event.ID == current || job.State != Queued || job.JoinedTo != "" {
			continue
		}
		mode := job.Event.Delivery()
		if mode == event.WhenIdle || (interruptOnly && mode != event.Interrupt) {
			continue
		}
		if job.NotBefore.After(now) {
			if next.IsZero() || job.NotBefore.Before(next) {
				next = job.NotBefore
			}
			continue
		}
		out = append(out, job)
	}
	return out, next, nil
}
