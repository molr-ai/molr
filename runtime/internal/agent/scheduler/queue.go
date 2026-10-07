package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

const jobsBucket = "agent.jobs"

type queue struct{ store persistence.Store }

func NewQueue(store persistence.Store) Queue { return &queue{store} }
func allJobs(ctx context.Context, store persistence.Store) ([]Job, error) {
	jobs, err := persistence.Scan[Job](ctx, store, jobsBucket)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		seen[persistence.Key(job.Event.OwnerID, job.Event.ID)] = true
	}
	events, err := persistence.Scan[event.AssistantEvent](ctx, store, "agent.events")
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if e.Kind == event.UserStopped {
			continue
		}
		if !seen[persistence.Key(e.OwnerID, e.ID)] {
			jobs = append(jobs, Job{Event: e, State: Queued, NotBefore: e.CreatedAt, Version: 1})
		}
	}
	boundaries := map[string]event.InterruptBoundary{}
	for i := range jobs {
		job := &jobs[i]
		job.Event.DeliveryMode = job.Event.Delivery()
		if job.State == Done || job.State == Canceled {
			continue
		}
		key := persistence.Key(job.Event.OwnerID, job.Event.AssistantID)
		boundary, ok := boundaries[key]
		if !ok {
			var err error
			boundary, err = persistence.Load[event.InterruptBoundary](ctx, store, "agent.interrupt_boundaries", key)
			if err != nil && !persistence.Missing(err) {
				return nil, err
			}
			boundaries[key] = boundary
		}
		if boundary.Blocks(job.Event) {
			job.State = Canceled
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if (jobs[i].Event.Kind == event.DreamDue) != (jobs[j].Event.Kind == event.DreamDue) {
			return jobs[i].Event.Kind != event.DreamDue
		}
		if jobs[i].NotBefore.Equal(jobs[j].NotBefore) {
			return jobs[i].Event.ID < jobs[j].Event.ID
		}
		return jobs[i].NotBefore.Before(jobs[j].NotBefore)
	})
	return jobs, nil
}
func (q *queue) Claim(ctx context.Context, now time.Time) (claim Claim, err error) {
	err = q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		jobs, err := allJobs(ctx, q.store)
		if err != nil {
			return err
		}
		type dreamSession struct {
			ID, OwnerID, AssistantID core.ID
			State                    string
			Deadline                 time.Time
			Started                  bool
		}
		type dreamRun struct{ ID, OwnerID, SessionID core.ID }
		dreams, err := persistence.Scan[dreamSession](ctx, q.store, "business.dream_sessions_index")
		if err != nil {
			return err
		}
		dreamRuns, err := persistence.Scan[dreamRun](ctx, q.store, "business.dream_run_sessions")
		if err != nil {
			return err
		}
		activeDreams := map[string]core.ID{}
		endedDreams := map[string]bool{}
		runSessions := map[string]core.ID{}
		for _, d := range dreams {
			if d.State == "active" && now.Before(d.Deadline) {
				if d.Started {
					activeDreams[persistence.Key(d.OwnerID, d.AssistantID)] = d.ID
				}
			} else {
				endedDreams[persistence.Key(d.OwnerID, d.ID)] = true
			}
		}
		for _, r := range dreamRuns {
			runSessions[persistence.Key(r.OwnerID, r.ID)] = r.SessionID
		}
		busy := map[string]bool{}
		retrying := map[string]core.ID{}
		for _, job := range jobs {
			if job.State == Retry {
				retrying[persistence.Key(job.Event.OwnerID, job.Event.AssistantID)] = job.Event.ID
			}
			if job.State == Running {
				busy[persistence.Key(job.Event.OwnerID, job.Event.AssistantID)] = true
			}
		}
		for _, job := range jobs {
			if id := retrying[persistence.Key(job.Event.OwnerID, job.Event.AssistantID)]; id != "" && id != job.Event.ID {
				continue
			}
			if job.Event.Kind == event.UserStopped || job.JoinedTo != "" || (job.State != Queued && job.State != Retry) || job.NotBefore.After(now) || busy[persistence.Key(job.Event.OwnerID, job.Event.AssistantID)] {
				continue
			}
			sessionID := runSessions[persistence.Key(job.Event.OwnerID, job.Event.OperationID)]
			if job.Event.Kind == event.DreamDue && sessionID != "" && endedDreams[persistence.Key(job.Event.OwnerID, sessionID)] {
				job.State = Canceled
				job.Version++
				if err := saveJob(ctx, tx, job); err != nil {
					return err
				}
				continue
			}
			if active := activeDreams[persistence.Key(job.Event.OwnerID, job.Event.AssistantID)]; active != "" && (job.Event.Kind != event.DreamDue || sessionID != active) {
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
			if err = saveJob(ctx, tx, job); err != nil {
				return err
			}
			claim = Claim{Job: job, Principal: core.Principal{OwnerID: job.Event.OwnerID, SubjectID: job.Event.AssistantID, Kind: core.ActorAssistant, ExecutionID: job.ExecutionID}}
			return nil
		}
		return core.NewError(core.NotFound, "no eligible job")
	})
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
	r.Page, err = r.Page.Normalize()
	if err != nil {
		return
	}
	type cursor struct {
		Owner     core.ID
		Assistant core.ID
		States    string
		After     core.ID
	}
	states, _ := json.Marshal(r.States)
	position := cursor{Owner: owner, Assistant: r.AssistantID, States: string(states)}
	if r.Page.Cursor != "" {
		data, e := base64.RawURLEncoding.DecodeString(r.Page.Cursor)
		if e != nil {
			return page, core.Invalid("cursor", "invalid cursor")
		}
		var c cursor
		if json.Unmarshal(data, &c) != nil || c.Owner != owner || c.Assistant != r.AssistantID || c.States != string(states) {
			return page, core.Invalid("cursor", "cursor query mismatch")
		}
		position.After = c.After
	}
	err = q.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		jobs, e := allJobs(ctx, q.store)
		if e != nil {
			return e
		}
		page.Items = []Job{}
		for _, job := range jobs {
			if job.Event.OwnerID != owner || job.Event.ID <= position.After || (r.AssistantID != "" && job.Event.AssistantID != r.AssistantID) {
				continue
			}
			if len(r.States) > 0 {
				found := false
				for _, state := range r.States {
					if state == job.State {
						found = true
					}
				}
				if !found {
					continue
				}
			}
			page.Items = append(page.Items, job)
		}
		sort.Slice(page.Items, func(i, j int) bool { return page.Items[i].Event.ID < page.Items[j].Event.ID })
		if len(page.Items) > r.Page.Limit {
			page.Items = page.Items[:r.Page.Limit]
			position.After = page.Items[len(page.Items)-1].Event.ID
			data, _ := json.Marshal(position)
			page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		}
		return nil
	})
	return
}
func (q *queue) Cancel(ctx context.Context, owner, id core.ID) error {
	return q.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		jobs, err := allJobs(ctx, q.store)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.OwnerID == owner && job.Event.ID == id {
				if job.State == Done {
					return core.NewError(core.FailedPrecondition, "job already complete")
				}
				if job.State == Canceled {
					return nil
				}
				job.State = Canceled
				job.Version++
				return saveJob(ctx, tx, job)
			}
		}
		return core.NewError(core.NotFound, "job not found")
	})
}

// Pending returns ordered, unclaimed inputs eligible to join the current turn.
// Call inside the checkpoint transaction when consuming; the scheduler uses the
// same job records and stop boundaries, preventing double dispatch.
func Pending(ctx context.Context, store persistence.Store, owner, assistant, current core.ID, now time.Time, interruptOnly bool) ([]Job, error) {
	jobs, err := allJobs(ctx, store)
	if err != nil {
		return nil, err
	}
	var out []Job
	for _, job := range jobs {
		if job.Event.OwnerID != owner || job.Event.AssistantID != assistant || job.Event.ID == current || job.State != Queued || job.JoinedTo != "" || job.NotBefore.After(now) {
			continue
		}
		mode := job.Event.Delivery()
		if mode == event.WhenIdle || (interruptOnly && mode != event.Interrupt) {
			continue
		}
		out = append(out, job)
	}
	return out, nil
}
