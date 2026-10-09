package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"strings"
	"time"
)

func (s *Coordinator) run(ctx context.Context, p core.Principal, e event.AssistantEvent) (Run, error) {
	if p.Kind != core.ActorAssistant || p.SubjectID != e.AssistantID || p.OwnerID != e.OwnerID {
		return Run{}, core.NewError(core.Forbidden, "dream identity mismatch")
	}
	if err := s.authorize(ctx, p, e.AssistantID, false); err != nil {
		return Run{}, err
	}
	r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(p.OwnerID, e.OperationID))
	if err != nil {
		return r, err
	}
	if r.EventID != e.ID || r.AssistantID != p.SubjectID {
		return r, core.NewError(core.Forbidden, "dream event mismatch")
	}
	c, err := s.config(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return r, err
	}
	if !c.Enabled && !r.Manual {
		return r, core.NewError(core.Canceled, "automatic dreaming disabled")
	}
	return r, nil
}

// SavedOutput resumes a durable file plan without asking the provider to regenerate it.
func (s *Coordinator) SavedOutput(ctx context.Context, p core.Principal, e event.AssistantEvent) (string, error) {
	r, err := s.run(ctx, p, e)
	if err != nil {
		return "", err
	}
	if r.State == "completed" && r.Output == "" {
		return "Previously completed; retained summary cleared.", nil
	}
	return r.Output, nil
}

func (s *Coordinator) ensureOpen(ctx context.Context, r Run) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	deadline := r.Deadline
	if deadline.IsZero() {
		deadline = r.CreatedAt.Add(10 * time.Minute)
	}
	if !s.options.Now().Before(deadline) {
		return core.NewError(core.DeadlineExceeded, "dream deadline reached")
	}
	if r.SessionID != "" {
		v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(r.OwnerID, r.SessionID))
		if err != nil {
			return err
		}
		if v.State != "active" {
			return core.NewError(core.Canceled, "dream session has ended")
		}
	}
	return nil
}
func (s *Coordinator) Prepare(ctx context.Context, p core.Principal, e event.AssistantEvent) (string, error) {
	r, err := s.run(ctx, p, e)
	if err != nil {
		return "", err
	}
	// Files and batch progress are already durable; only Agent bookkeeping remains.
	if r.State == "completed" {
		return "{}", nil
	}

	if err = s.ensureOpen(ctx, r); err != nil {
		return "", err
	}
	if err = s.checkSources(ctx, p, r); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(struct {
		Day         string
		Timezone    string
		Backfill    bool
		Maintenance bool
		Sources     []Evidence
		Workspace   string
		Core        string
		Global      string
	}{r.Day, r.Timezone, r.Backfill, r.Maintenance, r.Sources, "/work/.agents/memory", "/work/MEMORY.md", "/global/.agents/memory"})
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.run(ctx, p, e)
		if err != nil {
			return err
		}
		if err := s.ensureOpen(ctx, current); err != nil {
			return err
		}
		if current.SessionID != "" {
			v, err := persistence.Load[Session](ctx, s.store, sessions, persistence.Key(r.OwnerID, r.SessionID))
			if err != nil {
				return err
			}
			v.Started = true
			if err := saveSession(ctx, tx, v); err != nil {
				return err
			}
		}
		current.State = "running"
		current.UpdatedAt = s.options.Now().UTC()
		return tx.Put(ctx, runs, persistence.Key(p.OwnerID, current.ID), current)
	})
	return string(payload), err
}
func (s *Coordinator) checkSources(ctx context.Context, p core.Principal, r Run) error {
	a, err := assistant.NewRepository(s.store).Get(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return err
	}
	if a.State != assistant.Active {
		return core.NewError(core.Canceled, "assistant inactive")
	}
	current, err := s.collect(ctx, Config{OwnerID: p.OwnerID}, a, r.Start, r.End)
	if err != nil {
		return err
	}
	valid := map[string]bool{}
	for _, e := range current {
		valid[e.Source.Kind+string(e.Source.ID)+e.Version] = true
	}
	for _, e := range r.Sources {
		if !valid[e.Source.Kind+string(e.Source.ID)+e.Version] {
			return core.NewError(core.Conflict, "dream source changed or forgotten")
		}
	}
	return nil
}
func (s *Coordinator) Commit(ctx context.Context, p core.Principal, e event.AssistantEvent, output string) error {
	output = strings.TrimSpace(output)
	if output == "" || len(output) > 8000 || sensitive(output) {
		return core.Invalid("summary", "bounded non-sensitive consolidation summary required")
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	r, err := s.run(ctx, p, e)
	if err != nil {
		return err
	}
	if r.State == "completed" {
		return nil
	}
	check := func(ctx context.Context) error {
		current, err := s.run(ctx, p, e)
		if err != nil {
			return err
		}
		if err = s.ensureOpen(ctx, current); err != nil {
			return err
		}
		if err = s.checkSources(ctx, p, current); err != nil {
			return err
		}
		return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "memory"}, Action: policy.Write})
	}
	if err = check(ctx); err != nil {
		return err
	}
	if r.Output != "" && r.Output != output {
		return core.NewError(core.Conflict, "dream summary already saved")
	}
	r.Output, r.Summary, r.State = output, output, "committing"
	if err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
	}); err != nil {
		return err
	}
	name := fmt.Sprintf("第%d批回顾.md", r.Batch)
	if r.Backfill {
		name = "补处理" + name
	}
	r.ReviewPath = fmt.Sprintf(".agents/reviews/%s/%s", r.Day, name)
	refs := []core.Object{}
	for _, source := range r.Sources {
		refs = append(refs, source.Source)
	}
	r.ReviewVersion, err = s.options.Files.WriteReview(ctx, p.OwnerID, p.SubjectID, r.ID, r.ReviewPath, "# "+r.Day+" 回顾\n\n"+output, refs, func() error { return check(ctx) })
	if err != nil {
		return err
	}
	r.State, r.FailureCode, r.UpdatedAt = "completed", "", s.options.Now().UTC()
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := check(ctx); err != nil {
			return err
		}
		if err := s.advanceSession(ctx, r); err != nil {
			return err
		}
		return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
	})
}

func (s *Coordinator) Failed(ctx context.Context, e event.AssistantEvent, cause error) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(e.OwnerID, e.OperationID))
		if err != nil {
			return err
		}
		if r.State == "completed" {
			return nil
		}
		if r.SessionID != "" {
			state := "failed"
			if core.CodeOf(cause) == core.DeadlineExceeded {
				state = "timed_out"
			}
			if core.CodeOf(cause) == core.Canceled {
				state = "canceled"
			}
			if err := s.closeSession(ctx, r.OwnerID, r.SessionID, state, s.options.Now().UTC()); err != nil {
				return err
			}
			r.State = state
		} else {
			r.State = "failed"
		}
		r.FailureCode = string(core.CodeOf(cause))
		r.UpdatedAt = s.options.Now().UTC()
		return tx.Put(ctx, runs, persistence.Key(e.OwnerID, r.ID), r)
	})
}
