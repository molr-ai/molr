package plan

import (
	"context"
	"encoding/base64"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"time"
)

// DispatchWatch atomically freezes a bounded event batch and starts at most one
// assistant execution. Later observations remain queued without model calls.
func (s *Coordinator) DispatchWatch(ctx context.Context, owner, id core.ID) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		w, err := watch.Load(ctx, tx.Store(), owner, id)
		if err != nil {
			return err
		}
		if w.Paused {
			return nil
		}
		rows, err := persistence.Select[task.Task](ctx, tx.Store(), "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Plan.Kind", "watch"), persistence.Eq("Plan.ID", id), {Field: "State", Op: "in", Value: []task.State{task.Pending, task.Running, task.Waiting, task.Paused}}}, Limit: 1})
		if err != nil {
			return err
		}
		for _, run := range rows {
			if run.State == task.Paused || run.State == task.Waiting {
				return nil
			}
			jobs, err := persistence.Select[event.Job](ctx, tx.Store(), "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), persistence.Eq("Event.TaskID", run.ID)}, Unordered: true})
			if err != nil {
				return err
			}
			terminal := task.State("")
			for _, job := range jobs {
				if job.State == event.Queued || job.State == event.Running || job.State == event.Retry {
					return nil
				}
				if job.State == event.Failed {
					terminal = task.Failed
				}
				if job.State == event.Canceled {
					terminal = task.Canceled
				}
			}
			if terminal == "" {
				return nil
			}
			previous := run.Version
			run.State = terminal
			run.Version++
			run.UpdatedAt = time.Now().UTC()
			if err := task.NewRepository(tx.Store()).Save(ctx, run, previous); err != nil {
				return err
			}
		}
		w.HandlingTaskID = ""
		if len(w.Pending) == 0 {
			return tx.Put(ctx, watch.Bucket, persistence.Key(owner, id), w)
		}
		n := min(8, len(w.Pending))
		w.Batch = append([]watch.Signal(nil), w.Pending[:n]...)
		w.Pending = append([]watch.Signal(nil), w.Pending[n:]...)
		w.PendingCount = len(w.Pending)
		if err := tx.Put(ctx, watch.Bucket, persistence.Key(owner, id), w); err != nil {
			return err
		}
		source, err := s.source(ctx, owner, core.Object{Kind: "watch", ID: id})
		if err != nil {
			return err
		}
		run, err := s.startTrusted(ctx, owner, source, nil, true)
		if err != nil {
			return err
		}
		w.HandlingTaskID = run.ID
		if err := tx.Put(ctx, watch.Bucket, persistence.Key(owner, id), w); err != nil {
			return err
		}

		return nil
	})
}

// WatchPage lists continuous plans independently of any calendar date window.
func (s *Coordinator) WatchPage(ctx context.Context, p core.Principal, scope, state string, page core.PageRequest) (core.Page[Summary], error) {
	out := core.Page[Summary]{Items: []Summary{}}
	if err := s.authorize(ctx, p, core.Object{}, "read"); err != nil {
		return out, err
	}
	page, err := page.Normalize()
	if err != nil {
		return out, err
	}
	if state != "" && state != "all" && state != "open" && state != "done" {
		return out, core.Invalid("state", "invalid state")
	}
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}, OrderBy: "key", Limit: page.Limit + 1}
	if scope == "me" {
		return out, nil
	}
	if p.Kind == core.ActorAssistant {
		if scope != "" && scope != "all" && scope != string(p.SubjectID) {
			return out, core.NewError(core.Forbidden, "another assistant")
		}
		scope = string(p.SubjectID)
	}
	if scope != "" && scope != "all" {
		q.Where = append(q.Where, persistence.Eq("AssistantID", scope))
	}
	if state == "done" {
		q.Where = append(q.Where, persistence.Eq("State", "completed"))
	} else if state == "open" {
		q.Where = append(q.Where, persistence.Filter{Field: "State", Op: "!=", Value: "completed"})
	}
	binding := string(p.OwnerID) + "/" + scope + "/" + state + "/"
	if page.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		if err != nil || !strings.HasPrefix(string(raw), binding) {
			return out, core.Invalid("cursor", "invalid watcher cursor")
		}
		q.Where = append(q.Where, persistence.Filter{Field: "key", Op: ">", Value: strings.TrimPrefix(string(raw), binding)})
	}
	rows, err := persistence.Select[watch.Watch](ctx, s.store, watch.Bucket, q)
	if err != nil {
		return out, err
	}
	more := len(rows) > page.Limit
	if more {
		rows = rows[:page.Limit]
	}
	for _, w := range rows {
		v, err := s.Detail(ctx, p, core.Object{Kind: "watch", ID: w.ID}, nil)
		if err != nil {
			return out, err
		}
		v.Watch = watchSummary(&w)
		out.Items = append(out.Items, v)
	}
	if more && len(rows) > 0 {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(binding + persistence.Key(p.OwnerID, rows[len(rows)-1].ID)))
	}
	return out, nil
}

func watchSummary(w *watch.Watch) *watch.Watch {
	if w == nil {
		return nil
	}
	v := *w
	v.Watcher.Script = ""
	v.Checkpoint = nil
	v.Pending = nil
	v.Recent = nil
	v.Batch = nil
	v.Instructions = ""
	v.AuthorizationFingerprint = ""
	return &v
}

// RecordWatchHandling records Runtime-observed progress for a watcher event
// batch. It never marks ordinary goal tasks delivered or completes the watcher.
// The caller joins this to the agent checkpoint/acknowledgement transaction.
func (s *Coordinator) RecordWatchHandling(ctx context.Context, owner, assistant, id core.ID, state task.State, reason string) error {
	if id == "" {
		return nil
	}
	if state != task.Running && state != task.Waiting && state != task.Completed {
		return core.Invalid("state", "invalid watch handling state")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		repo := task.NewRepository(tx.Store())
		v, err := repo.Get(ctx, owner, id)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if v.Plan.Kind != "watch" {
			return nil
		}
		if v.AssistantID != assistant {
			return core.NewError(core.Forbidden, "watch batch belongs to another assistant")
		}
		if v.State == task.Completed || v.State == task.Failed || v.State == task.Canceled || v.State == task.Paused {
			return nil
		}
		previous := v.Version
		v.State = state
		v.WaitingReason = reason
		v.UpdatedAt = time.Now().UTC()
		v.Version++
		if err := repo.Save(ctx, v, previous); err != nil {
			return err
		}
		if state == task.Completed {
			w, err := watch.Load(ctx, tx.Store(), owner, v.Plan.ID)
			if persistence.Missing(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if w.HandlingTaskID == id {
				w.HandlingTaskID = ""
				return tx.Put(ctx, watch.Bucket, persistence.Key(owner, w.ID), w)
			}
		}
		return nil
	})
}
