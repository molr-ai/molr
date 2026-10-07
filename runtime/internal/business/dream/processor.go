package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
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
	if err = s.ensureOpen(ctx, r); err != nil {
		return "", err
	}
	if err = s.checkSources(ctx, p, r); err != nil {
		return "", err
	}
	existing := []memory.Memory{}
	for _, scope := range []memory.Scope{memory.Assistant, memory.Global} {
		page, err := s.options.Memories.List(ctx, p, memory.Query{Scope: scope, Page: core.PageRequest{Limit: 200}})
		if err != nil {
			return "", err
		}
		existing = append(existing, page.Items...)
	}
	// Bound comparison context independently of the evidence batch.
	relevant := []memory.Memory{}
	comparisonBytes := 0
	query := ""
	for _, v := range r.Sources {
		query += " " + v.Text
	}
	for _, m := range existing {
		if len(relevant) >= 40 {
			break
		}
		encoded, _ := json.Marshal(m)
		if memory.Score(query, m.Text+" "+m.Context) > 0 && comparisonBytes+len(encoded) < 16<<10 {
			comparisonBytes += len(encoded)
			relevant = append(relevant, m)
		}
	}
	payload, _ := json.Marshal(struct {
		Day      string
		Sources  []Evidence
		Existing []memory.Memory
	}{r.Day, r.Sources, relevant})
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
	if strings.HasPrefix(output, "```json") {
		output = strings.TrimPrefix(output, "```json")
		output = strings.TrimSuffix(strings.TrimSpace(output), "```")
	}
	var result Result
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if len(output) > 32<<10 || decoder.Decode(&result) != nil || !json.Valid([]byte(output)) || strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 8000 || len(result.Memories) > 8 || sensitive(result.Summary) {
		return core.Invalid("dream", "invalid consolidation result")
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return func() error {
		r, err := s.run(ctx, p, e)
		if err != nil {
			return err
		}
		if r.State == "completed" {
			return nil
		}
		if err = s.ensureOpen(ctx, r); err != nil {
			return err
		}
		if err = s.checkSources(ctx, p, r); err != nil {
			return err
		}
		if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "memory"}, Action: policy.Write}); err != nil {
			return err
		}
		allowed := map[core.Object]Evidence{}
		for _, src := range r.Sources {
			allowed[src.Source] = src
		}
		existing := []memory.Memory{}
		for _, scope := range []memory.Scope{memory.Assistant, memory.Global} {
			page, err := s.options.Memories.List(ctx, p, memory.Query{Scope: scope, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				return err
			}
			existing = append(existing, page.Items...)
		}
		if s.options.Files == nil {
			return core.NewError(core.Unavailable, "memory file storage unavailable")
		}
		planned := []memory.Memory{}
		expected := map[string]string{}
		prior := r
		r.MemoryFiles = nil
		r.Decisions = nil
		r.MemoryVersions = map[string]string{}
		r.MemoryUpdated = map[string]bool{}
		if r.MemoryStarted == nil {
			r.MemoryStarted = map[string]bool{}
		}
		if prior.Output != "" && prior.Output != output {
			return core.NewError(core.Conflict, "dream output differs from committed plan")
		}
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range result.Memories {
			if sensitive(c.Text+" "+c.Context) || strings.TrimSpace(c.Text) == "" || len(c.Text) > 4800 || len(c.Context) > 1024 || len(c.Sources) == 0 || len(c.Sources) > 16 {
				return core.Invalid("memory", "invalid dream memory")
			}
			explicit, verified := false, false
			for _, ref := range c.Sources {
				src, ok := allowed[ref]
				if !ok {
					return core.Invalid("sources", "source outside evidence snapshot")
				}
				explicit = explicit || src.Kind == "user"
				verified = verified || src.Kind == "task_completed"
			}
			state := memory.Pending
			switch c.Evidence {
			case "explicit":
				if explicit {
					state = memory.Active
				}
			case "verified":
				if verified {
					state = memory.Active
				}
			case "inferred":
			default:
				return core.Invalid("evidence", "invalid evidence kind")
			}
			ref := memory.Ref{Scope: memory.Assistant, AssistantID: p.SubjectID, Path: c.Path}
			if err := ref.Validate(false); err != nil {
				return err
			}
			if !strings.HasPrefix(c.Path, ".agents/memory/") {
				return core.Invalid("path", "dreams write detailed memory files")
			}
			if seen[c.Path] {
				return core.Invalid("path", "duplicate candidate path")
			}
			seen[c.Path] = true
			if c.Action != "create" && c.Action != "update" {
				return core.Invalid("action", "create or update required")
			}
			if c.Action == "update" && c.Replaces != nil {
				return core.Invalid("replaces", "revision proposals must use a separate create path")
			}
			if c.Action == "update" && len(c.ExpectedVersion) != 64 {
				return core.Invalid("expected_version", "existing file version required")
			}
			if c.Action == "create" && c.Replaces == nil && c.ExpectedVersion != "" {
				return core.Invalid("expected_version", "creation has no previous version")
			}
			if c.Action == "update" && prior.MemoryVersions[c.Path] == "" {
				old, err := s.options.Memories.Get(ctx, p, ref)
				if err != nil {
					return err
				}
				if old.Version != c.ExpectedVersion {
					return core.NewError(core.Conflict, "comparison file changed")
				}
				if state == memory.Pending && old.State != memory.Pending {
					return core.NewError(core.Conflict, "uncertain changes require a separate proposal file")
				}
				if old.Confirmed {
					return core.NewError(core.Conflict, "confirmed file requires a separate semantic revision proposal")
				}
			}
			m := memory.Memory{Ref: ref, OwnerID: p.OwnerID, Text: c.Text, Context: c.Context, Evidence: c.Evidence, State: state, Sources: c.Sources, DreamID: r.ID, Replaces: c.Replaces, ReplacesVersion: c.ExpectedVersion, CreatedAt: r.CreatedAt, UpdatedAt: r.CreatedAt}

			blocked, err := s.options.Files.Suppressed(ctx, m)
			if err != nil {
				return err
			}
			if blocked {
				r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: "discarded", Reason: "forgotten"})
				continue
			}
			duplicate := false
			previousText := ""
			for _, old := range existing {
				if old.DreamID == r.ID || prior.MemoryVersions[old.Path] != "" {
					continue
				}
				if old.OwnerID != p.OwnerID || (old.Scope == memory.Assistant && old.AssistantID != p.SubjectID) {
					continue
				}
				if c.Action == "create" && strings.EqualFold(strings.Join(strings.Fields(old.Text), ""), strings.Join(strings.Fields(m.Text), "")) {
					duplicate = true
				}

			}
			if duplicate {
				r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: "discarded", Reason: "duplicate"})
				continue
			}
			if c.Replaces != nil {
				old, err := s.options.Memories.Get(ctx, p, *c.Replaces)
				if err != nil {
					return err
				}
				if old.Version != c.ExpectedVersion {
					return core.NewError(core.Conflict, "comparison file changed")
				}
				if old.Ref == ref {
					return core.Invalid("path", "proposal requires a different file")
				}
				m.State = memory.Pending
				previousText = old.Text
			}
			if c.Action == "create" && prior.MemoryVersions[c.Path] == "" {
				if _, err := s.options.Memories.Get(ctx, p, ref); err == nil {
					return core.NewError(core.Conflict, "semantic filename already exists")
				} else if core.CodeOf(err) != core.NotFound {
					return err
				}
			}

			if c.Action == "update" && c.Replaces == nil {
				m.ReplacesVersion = ""
			}
			m, err = s.options.Files.Preview(ctx, m)
			if err != nil {
				return err
			}
			planned = append(planned, m)
			if c.Action == "update" {
				r.MemoryUpdated[m.Path] = true
				expected[m.Path] = c.ExpectedVersion
				m.ReplacesVersion = ""
			}
			r.MemoryVersions[m.Path] = m.Version

			existing = append(existing, m)
			r.MemoryFiles = append(r.MemoryFiles, m.Ref)
			r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: string(m.State), Reason: m.Evidence, Memory: &m.Ref, Replaces: m.Replaces, PreviousText: previousText})
		}
		r.Output = output
		r.Summary = result.Summary
		r.State = "committing"
		save := func() error {
			return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				current, err := s.run(ctx, p, e)
				if err != nil {
					return err
				}
				if current.State == "undone" {
					return core.NewError(core.Canceled, "dream undone")
				}
				if err = s.ensureOpen(ctx, current); err != nil {
					return err
				}
				return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
			})
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
		if err = save(); err != nil {
			return err
		}
		for _, m := range planned {
			if r.MemoryStarted[m.Path] {
				current, err := s.options.Memories.Get(ctx, p, m.Ref)
				if err != nil {
					return core.NewError(core.Conflict, "partially committed dream file is missing; review retained files")
				}
				if current.Version != r.MemoryVersions[m.Path] {
					return core.NewError(core.Conflict, "partially committed dream file was edited")
				}
				continue
			}
			if err = check(ctx); err != nil {
				return err
			}
			r.MemoryStarted[m.Path] = true
			if err = save(); err != nil {
				return err
			}
			if _, err = s.options.Files.Put(ctx, m, func() string {
				if r.MemoryUpdated[m.Path] {
					return expected[m.Path]
				}
				return ""
			}(), func() error { return check(ctx) }); err != nil {
				return err
			}
		}
		r.ReviewPath = fmt.Sprintf(".agents/reviews/%s/第%d批回顾.md", r.Day, r.Batch)
		refs := []core.Object{}
		for _, source := range r.Sources {
			refs = append(refs, source.Source)
		}
		r.ReviewVersion, err = s.options.Files.WriteReview(ctx, p.OwnerID, p.SubjectID, r.ID, r.ReviewPath, "# "+r.Day+" 回顾\n\n"+result.Summary, refs, func() error { return check(ctx) })
		if err != nil {
			return err
		}
		r.State = "completed"
		r.FailureCode = ""
		r.UpdatedAt = s.options.Now().UTC()
		return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := check(ctx); err != nil {
				return err
			}
			if err := s.advanceSession(ctx, r); err != nil {
				return err
			}
			return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
		})
	}()
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
