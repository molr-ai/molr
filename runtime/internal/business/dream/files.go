package dream

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"strings"
)

func FileRef(p core.Principal, value string, directory bool) (memory.Ref, error) {
	ref := memory.Ref{Scope: memory.Assistant, AssistantID: p.SubjectID}
	if strings.HasPrefix(value, "/global/") {
		ref.Scope = memory.Global
		ref.AssistantID = ""
		value = strings.TrimPrefix(value, "/global/")
	} else {
		value = strings.TrimPrefix(value, "/work/")
	}
	ref.Path = value
	return ref, ref.Validate(directory)
}
func (s *Coordinator) CheckFileAccess(ctx context.Context, p core.Principal, e event.AssistantEvent, write bool) error {
	r, err := s.run(ctx, p, e)
	if err != nil {
		return err
	}
	if err = s.ensureOpen(ctx, r); err != nil {
		return err
	}
	if r.State == "completed" {
		return core.NewError(core.FailedPrecondition, "dream already completed")
	}
	action := policy.Read
	if write {
		action = policy.Write
		if err = s.checkSources(ctx, p, r); err != nil {
			return err
		}
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "memory"}, Action: action})
}

// RememberRead observes a normal file read; it does not define a tool or format.
func (s *Coordinator) RememberRead(ctx context.Context, p core.Principal, e event.AssistantEvent, ref memory.Ref) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	doc, err := s.options.Files.ReadFile(ctx, p.OwnerID, ref)
	if persistence.Missing(err) || core.CodeOf(err) == core.InvalidArgument {
		return nil
	}
	if err != nil {
		return err
	}
	r, err := s.run(ctx, p, e)
	if err != nil {
		return err
	}
	r.MemorySources = uniqueSources(r.MemorySources, doc.Sources)
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
	})
}

// BeforeFileChange records recovery data for the common file executor. All
// actual write/edit/move/delete operations are performed by that executor.
func (s *Coordinator) BeforeFileChange(ctx context.Context, p core.Principal, e event.AssistantEvent, operation core.ID, kind string, ref memory.Ref, to *memory.Ref, version string, after []byte) (func() error, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	check := func() error { return s.CheckFileAccess(ctx, p, e, true) }
	if err := check(); err != nil {
		return nil, err
	}
	r, err := s.run(ctx, p, e)
	if err != nil {
		return nil, err
	}
	old, err := s.options.Files.ReadFile(ctx, p.OwnerID, ref)
	exists := err == nil
	if err != nil && !persistence.Missing(err) {
		return nil, err
	}
	if exists && (version == "" || old.FileVersion != version) {
		return nil, core.NewError(core.Conflict, "read the file and supply its current expected_version")
	}
	if !exists && kind != "file_write" {
		return nil, core.NewError(core.NotFound, "memory file not found")
	}
	changes := []memory.FileChange{}
	change := memory.FileChange{Ref: ref, Before: []byte(old.Text), BeforeExists: exists, After: after, AfterExists: kind == "file_write" || kind == "file_edit"}
	if kind == "file_move" {
		if to == nil || to.Scope != memory.Assistant || ref.Path == "MEMORY.md" || to.Path == "MEMORY.md" {
			return nil, core.Invalid("to_path", "move between private topic files")
		}
		changes = append(changes, memory.FileChange{Ref: *to, After: []byte(old.Text), AfterExists: true})
	}
	if change.AfterExists && sensitive(string(after)) {
		return nil, core.Invalid("text", "credentials must not be retained in memory")
	}
	changes = append(changes, change)
	r.MemorySources = uniqueSources(r.MemorySources, old.Sources)
	sources := append([]core.Object{}, r.MemorySources...)
	for _, v := range r.Sources {
		sources = uniqueSources(sources, []core.Object{v.Source})
	}
	if len(sources) > 1024 {
		return nil, core.NewError(core.FailedPrecondition, "file provenance limit exceeded")
	}
	found := false
	for _, v := range r.FileOperations {
		found = found || v == operation
	}
	if !found {
		r.FileOperations = append(r.FileOperations, operation)
	}
	if err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
	}); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(struct {
		Kind    string
		Changes []memory.FileChange
	}{kind, changes})
	if err = s.options.Files.PrepareFileOperation(ctx, p.OwnerID, operation, string(id(string(raw))), changes, check, sources...); err != nil {
		return nil, err
	}
	return func() error {
		s.commitMu.Lock()
		defer s.commitMu.Unlock()
		if err := s.options.Files.CompleteFileOperation(ctx, p.OwnerID, operation, check); err != nil {
			return err
		}
		return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			current, err := s.run(ctx, p, e)
			if err != nil {
				return err
			}
			for _, v := range current.FileChanges {
				if v.OperationID == operation {
					return nil
				}
			}
			entry := FileOperation{OperationID: operation, Tool: kind, Path: ref.LogicalPath()}
			if to != nil {
				entry.ToPath = to.LogicalPath()
			}
			current.FileChanges = append(current.FileChanges, entry)
			for _, c := range changes {
				found := false
				for _, v := range current.MemoryFiles {
					found = found || v == c.Ref
				}
				if !found {
					current.MemoryFiles = append(current.MemoryFiles, c.Ref)
				}
			}
			current.UpdatedAt = s.options.Now().UTC()
			return tx.Put(ctx, runs, persistence.Key(p.OwnerID, current.ID), current)
		})
	}, nil
}
func uniqueSources(existing, more []core.Object) []core.Object {
	seen := map[core.Object]bool{}
	out := []core.Object{}
	for _, list := range [][]core.Object{existing, more} {
		for _, v := range list {
			if !seen[v] {
				out = append(out, v)
				seen[v] = true
			}
		}
	}
	return out
}
