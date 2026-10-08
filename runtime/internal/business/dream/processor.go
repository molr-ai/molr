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

// SavedOutput resumes a durable file plan without asking the provider to regenerate it.
func (s *Coordinator) SavedOutput(ctx context.Context, p core.Principal, e event.AssistantEvent) (string, error) {
	r, err := s.run(ctx, p, e)
	if err != nil {
		return "", err
	}
	if r.State == "completed" && r.Output == "" {
		return `{"Summary":"Previously completed; retained output cleared.","Memories":[]}`, nil
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
	existing, err := s.memories(ctx, p)
	if err != nil {
		return "", err
	}
	relevant, currentCore := comparison(r, existing)
	if r.Compared != nil && r.Output == "" {
		relevant = nil
		currentCore = nil
		for _, d := range r.Compared {
			m, err := s.options.Memories.Get(ctx, p, d.Ref)
			if err != nil || m.Version != d.Version {
				return "", core.NewError(core.Conflict, "comparison changed after preparation")
			}
			if m.Scope == memory.Assistant && m.Summary {
				currentCore = &m
			} else {
				relevant = append(relevant, m)
			}
		}
	}
	protected := []memory.Ref{}
	inspect := append([]memory.Memory{}, relevant...)
	if currentCore != nil {
		inspect = append(inspect, *currentCore)
	}
	for _, m := range inspect {
		held, err := s.userMaintained(ctx, m)
		if err != nil {
			return "", err
		}
		if held {
			protected = append(protected, m.Ref)
		}
	}
	payload, _ := json.Marshal(struct {
		Day         string
		Maintenance bool
		Sources     []Evidence
		Existing    []memory.Memory
		Core        *memory.Memory
		Protected   []memory.Ref
	}{r.Day, r.Maintenance, r.Sources, relevant, currentCore, protected})
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
		if current.Output == "" && current.Compared == nil {
			current.Compared = []memory.Dependency{}
			for _, m := range relevant {
				current.Compared = append(current.Compared, memory.Dependency{Ref: m.Ref, Version: m.Version})
			}
			if currentCore != nil {
				current.Compared = append(current.Compared, memory.Dependency{Ref: currentCore.Ref, Version: currentCore.Version})
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
	if len(output) > 64<<10 || decoder.Decode(&result) != nil || !json.Valid([]byte(output)) || strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 8000 || len(result.Memories) > 8 || sensitive(result.Summary) {
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
		existing, err := s.memories(ctx, p)
		if err != nil {
			return err
		}
		for _, m := range existing {
			if m.State != memory.Active || !compared(r, m.Ref, m.Version) {
				continue
			}
			for _, source := range m.Sources {
				if _, ok := allowed[source]; !ok {
					allowed[source] = Evidence{Source: source, Kind: "memory_" + m.Evidence}
				}
			}
		}
		if s.options.Files == nil {
			return core.NewError(core.Unavailable, "memory file storage unavailable")
		}
		planned := []memory.Memory{}
		expected := map[string]string{}
		prior := r
		r.MemoryFiles = nil
		r.MemorySources = nil
		provenance := map[core.Object]bool{}
		retainSource := func(ref core.Object) {
			if !provenance[ref] {
				r.MemorySources = append(r.MemorySources, ref)
				provenance[ref] = true
			}
		}
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
			if sensitive(c.Text+" "+c.Context+" "+c.Reason) || strings.TrimSpace(c.Text) == "" || len(c.Text) > 4800 || len(c.Context) > 1024 || len(c.Sources) == 0 || len(c.Sources) > 256 || len(c.Reason) > 1024 {
				return core.Invalid("memory", "invalid dream memory")
			}
			explicit, verified := false, false
			for _, ref := range c.Sources {
				src, ok := allowed[ref]
				if !ok && prior.Output == output && prior.MemoryVersions[c.Path] != "" {
					src = Evidence{Kind: "memory_" + c.Evidence}
					ok = true
				}
				if !ok {
					return core.Invalid("sources", "source outside evidence snapshot")
				}
				retainSource(ref)
				explicit = explicit || src.Kind == "user" || src.Kind == "memory_explicit"
				verified = verified || src.Kind == "task_completed" || src.Kind == "memory_verified"
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
			if state == memory.Pending || c.Replaces != nil {
				r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: "deferred", Reason: "insufficient_or_conflicting_evidence"})
				continue
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
			if c.Action != "create" && c.Action != "update" && c.Action != "retire" {
				return core.Invalid("action", "create, update or retire required")
			}
			if c.Action != "create" && len(c.ExpectedVersion) != 64 {
				return core.Invalid("expected_version", "existing file version required")
			}
			if c.Action == "create" && c.Replaces == nil && c.ExpectedVersion != "" {
				return core.Invalid("expected_version", "creation has no previous version")
			}
			if c.Action != "create" && prior.MemoryVersions[c.Path] == "" {
				old, err := s.options.Memories.Get(ctx, p, ref)
				if err != nil {
					return err
				}
				if old.Version != c.ExpectedVersion || !compared(r, ref, old.Version) {
					return core.NewError(core.Conflict, "comparison file changed")
				}
				protected, err := s.userMaintained(ctx, old)
				if err != nil {
					return err
				}
				if protected {
					r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: "deferred", Reason: "user_maintained", Memory: &ref})
					continue
				}
			}
			m := memory.Memory{Ref: ref, OwnerID: p.OwnerID, Text: c.Text, Context: c.Context, Evidence: c.Evidence, State: state, Sources: c.Sources, DreamID: r.ID, Replaces: c.Replaces, ReplacesVersion: c.ExpectedVersion, CreatedAt: r.CreatedAt, UpdatedAt: r.CreatedAt}
			if c.Action == "retire" {
				m.State = memory.Superseded
			}

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
			if c.Action != "create" {
				for _, old := range existing {
					if old.Ref == ref {
						previousText = old.Text
						break
					}
				}
			}
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
			if c.Action == "create" && prior.MemoryVersions[c.Path] == "" {
				if _, err := s.options.Memories.Get(ctx, p, ref); err == nil {
					return core.NewError(core.Conflict, "semantic filename already exists")
				} else if core.CodeOf(err) != core.NotFound {
					return err
				}
			}

			if c.Action != "create" && c.Replaces == nil {
				m.ReplacesVersion = ""
			}
			m, err = s.options.Files.Preview(ctx, m)
			if err != nil {
				return err
			}
			planned = append(planned, m)
			if c.Action != "create" {
				r.MemoryUpdated[m.Path] = true
				expected[m.Path] = c.ExpectedVersion
				m.ReplacesVersion = ""
			}
			r.MemoryVersions[m.Path] = m.Version

			existing = append(existing, m)
			r.MemoryFiles = append(r.MemoryFiles, m.Ref)
			r.Decisions = append(r.Decisions, Decision{Text: c.Text, Action: string(m.State), Reason: func() string {
				if c.Reason != "" {
					return c.Reason
				}
				return m.Evidence
			}(), Memory: &m.Ref, Replaces: m.Replaces, PreviousText: previousText})
		}
		if result.Core != nil {
			coreRef := memory.Ref{Scope: memory.Assistant, AssistantID: p.SubjectID, Path: "MEMORY.md"}
			old, readErr := s.options.Memories.Get(ctx, p, coreRef)
			if readErr != nil && !persistence.Missing(readErr) {
				return readErr
			}
			protected := false
			if readErr == nil {
				protected, err = s.userMaintained(ctx, old)
				if err != nil {
					return err
				}
			}
			if protected && prior.MemoryVersions[coreRef.Path] != "" {
				return core.NewError(core.Conflict, "core memory was edited after partial commit")
			}
			if protected {
				r.Decisions = append(r.Decisions, Decision{Text: result.Core.Text, Action: "deferred", Reason: "user_maintained", Memory: &coreRef})
			} else {
				m, err := s.coreMemory(ctx, p, prior, *result.Core, existing, planned)
				if err != nil {
					return err
				}
				for _, source := range m.Sources {
					retainSource(source)
				}
				planned = append(planned, m)
				if result.Core.ExpectedVersion != "" {
					r.MemoryUpdated[m.Path] = true
					expected[m.Path] = result.Core.ExpectedVersion
				}
				r.MemoryVersions[m.Path] = m.Version
				r.MemoryFiles = append(r.MemoryFiles, m.Ref)
				r.Decisions = append(r.Decisions, Decision{Text: m.Text, Action: "core", Reason: result.Core.Reason, Memory: &m.Ref, PreviousText: old.Text})
			}
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
			for _, d := range r.Compared {
				actual, err := s.options.Memories.Get(ctx, p, d.Ref)
				if err != nil {
					return core.NewError(core.Conflict, "comparison file disappeared")
				}
				if actual.Version != d.Version && !(r.MemoryStarted[d.Ref.Path] && d.Ref.Scope == memory.Assistant && actual.Version == r.MemoryVersions[d.Ref.Path]) {
					return core.NewError(core.Conflict, "comparison file changed during commit")
				}
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
			if r.MemoryUpdated[m.Path] {
				if err = s.options.Files.BackupDream(ctx, r.ID, m, expected[m.Path], func() error { return check(ctx) }); err != nil {
					return err
				}
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
		refs := append([]core.Object{}, r.MemorySources...)
		for _, source := range r.Sources {
			refs = append(refs, source.Source)
		}
		reviewText := "# " + r.Day + " 回顾\n\n" + result.Summary
		for _, decision := range r.Decisions {
			if decision.Action == "deferred" {
				reviewText += "\n\n## 未沉淀的观察 / Unconsolidated observation\n\n" + decision.Text + "\n\n" + decision.Reason
			}
		}
		r.ReviewVersion, err = s.options.Files.WriteReview(ctx, p.OwnerID, p.SubjectID, r.ID, r.ReviewPath, reviewText, refs, func() error { return check(ctx) })
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
