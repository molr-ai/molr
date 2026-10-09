package dream

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func (s *Coordinator) memories(ctx context.Context, p core.Principal) ([]memory.Memory, error) {
	all := []memory.Memory{}
	for _, scope := range []memory.Scope{memory.Assistant, memory.Global} {
		page := core.PageRequest{Limit: 200}
		total := 0
		for {
			result, err := s.options.Memories.List(ctx, p, memory.Query{Scope: scope, AssistantID: func() core.ID {
				if scope == memory.Assistant {
					return p.SubjectID
				}
				return ""
			}(), Page: page})
			if err != nil {
				return nil, err
			}
			for _, m := range result.Items {
				total += len(m.Text)
				if total > 8<<20 {
					return nil, core.NewError(core.FailedPrecondition, "memory content scan limit exceeded")
				}
			}
			all = append(all, result.Items...)
			if result.NextCursor == "" {
				break
			}
			page.Cursor = result.NextCursor
		}
	}
	return all, nil
}

// Read all pages before ranking. Core dependencies stay visible; a rotating
// tie-break allows quiet topics to be reconsidered on later days as well.
func comparison(r Run, all []memory.Memory) ([]memory.Memory, *memory.Memory) {
	query := ""
	for _, v := range r.Sources {
		query += " " + v.Text
	}
	var current *memory.Memory
	required := map[memory.Ref]bool{}
	for _, m := range all {
		if m.Scope == memory.Assistant && m.Summary {
			v := m
			current = &v
			for _, d := range m.Dependencies {
				required[d.Ref] = true
			}
		}
	}
	score := func(m memory.Memory) int {
		n := memory.Score(query, m.Text+" "+m.Context+" "+m.Path)
		if required[m.Ref] {
			n += 1 << 20
		}
		if m.Summary {
			n += 1 << 21
		}
		return n
	}
	rotation := func(m memory.Memory) uint64 {
		h := fnv.New64a()
		h.Write([]byte(r.Day + "/" + m.LogicalPath()))
		return h.Sum64()
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := score(all[i]), score(all[j])
		if a != b {
			return a > b
		}
		return rotation(all[i]) < rotation(all[j])
	})
	selected := []memory.Memory{}
	size := 0
	for _, m := range all {
		if m.Scope == memory.Assistant && m.Summary {
			continue
		}
		raw, _ := json.Marshal(m)
		if len(selected) >= 40 {
			break
		}
		if size+len(raw) > 32<<10 {
			continue
		}
		size += len(raw)
		selected = append(selected, m)
	}
	return selected, current
}
func compared(r Run, ref memory.Ref, version string) bool {
	if r.Compared == nil {
		return true
	} // Direct business callers still undergo live version checks.
	for _, d := range r.Compared {
		if d.Ref == ref && d.Version == version {
			return true
		}
	}
	return false
}
func (s *Coordinator) coreMemory(ctx context.Context, p core.Principal, r Run, c CoreUpdate, existing, planned []memory.Memory) (memory.Memory, error) {
	ref := memory.Ref{Scope: memory.Assistant, AssistantID: p.SubjectID, Path: "MEMORY.md"}
	if len(c.Text) > 6<<10 || len(c.Topics) > 40 || len(c.Reason) > 1024 || sensitive(c.Text+" "+c.Reason) {
		return memory.Memory{}, core.Invalid("core", "invalid core cognition")
	}
	current, err := s.options.Memories.Get(ctx, p, ref)
	if err != nil && !persistenceMissing(err) {
		return memory.Memory{}, err
	}
	if err == nil && current.Confirmed {
		return memory.Memory{}, core.NewError(core.Conflict, "user maintained core memory is protected")
	}
	if r.MemoryVersions[ref.Path] == "" {
		if err == nil && (current.Version != c.ExpectedVersion || !compared(r, ref, current.Version)) {
			return memory.Memory{}, core.NewError(core.Conflict, "core memory changed")
		}
		if err != nil && c.ExpectedVersion != "" {
			return memory.Memory{}, core.NewError(core.Conflict, "core memory missing")
		}
	}
	byPath := map[string]memory.Memory{}
	for _, m := range existing {
		if m.Scope == memory.Assistant && !m.Summary && compared(r, m.Ref, m.Version) {
			byPath[m.Path] = m
		}
	}
	for _, m := range planned {
		byPath[m.Path] = m
	}
	m := memory.Memory{Ref: ref, OwnerID: p.OwnerID, State: memory.Active, Evidence: "consolidated", DreamID: r.ID, Text: strings.TrimSpace(c.Text), CreatedAt: r.CreatedAt, UpdatedAt: r.CreatedAt}
	seen := map[string]bool{}
	sources := map[core.Object]bool{}
	for _, path := range c.Topics {
		topic, ok := byPath[path]
		if !ok || topic.State != memory.Active || seen[path] {
			return m, core.Invalid("core", "use distinct active topic files from the supplied context")
		}
		if !topic.Confirmed && topic.Evidence != "explicit" && topic.Evidence != "verified" {
			return m, core.Invalid("core", "topic lacks durable evidence")
		}
		blocked, e := s.options.Files.Suppressed(ctx, topic)
		if e != nil {
			return m, e
		}
		if blocked {
			return m, core.NewError(core.Conflict, "core topic was forgotten")
		}
		seen[path] = true
		m.Dependencies = append(m.Dependencies, memory.Dependency{Ref: topic.Ref, Version: topic.Version})
		for _, source := range topic.Sources {
			if !sources[source] {
				m.Sources = append(m.Sources, source)
				sources[source] = true
			}
		}
	}
	if len(m.Sources) > 256 {
		return m, core.Invalid("core", "too many sources")
	}
	if m.Text == "" {
		if len(c.Topics) != 0 {
			return m, core.Invalid("core", "empty core cannot cite topics")
		}
		m.Text = "No stable core cognition retained."
		m.State = memory.Superseded
	} else if len(m.Dependencies) == 0 {
		return m, core.Invalid("core", "core cognition requires topic evidence")
	}
	return s.options.Files.Preview(ctx, m)
}
func persistenceMissing(err error) bool { return core.CodeOf(err) == core.NotFound }

// A generated file is writable only while it still matches its generating run.
// This protects edits made directly with an editor as well as UI confirmations.
func (s *Coordinator) userMaintained(ctx context.Context, m memory.Memory) (bool, error) {
	if m.Confirmed || m.DreamID == "" {
		return true, nil
	}
	origin, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(m.OwnerID, m.DreamID))
	if err != nil {
		if persistence.Missing(err) {
			return true, nil
		}
		return false, err
	}
	return origin.MemoryVersions[m.Path] != m.Version, nil
}
