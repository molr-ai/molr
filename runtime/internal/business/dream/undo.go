package dream

import (
	"context"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func (s *Coordinator) Undo(ctx context.Context, p core.Principal, req UndoRequest) (out Run, err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	if err = req.Mutation.Validate(false); err != nil {
		return
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	err = func() error {
		r, err := persistence.Load[Run](ctx, s.store, runs, persistence.Key(p.OwnerID, req.ID))
		if err != nil {
			return err
		}
		if err = s.authorize(ctx, p, r.AssistantID, true); err != nil {
			return err
		}
		if r.State == "undone" {
			out = r
			return nil
		}
		if r.State != "completed" {
			return core.NewError(core.FailedPrecondition, "only completed dreams can be reverted")
		}
		for _, ref := range r.MemoryFiles {
			if r.MemoryUpdated[ref.Path] {
				restored, err := s.options.Files.RestoreDream(ctx, p.OwnerID, r.ID, ref, r.MemoryVersions[ref.Path], func() error { return s.authorize(ctx, p, r.AssistantID, true) })
				if err != nil {
					return err
				}
				if !restored {
					r.UndoRetained = append(r.UndoRetained, ref)
				}
				continue
			}
			m, err := s.options.Memories.Get(ctx, p, ref)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			if m.DreamID != r.ID || m.Version != r.MemoryVersions[ref.Path] || m.Confirmed {
				r.UndoRetained = append(r.UndoRetained, ref)
				continue
			}
			if err = s.options.Memories.Delete(ctx, p, memory.DeleteRequest{Mutation: memory.Mutation{IdempotencyKey: string(id("dream-undo", string(r.ID), ref.Path)), ExpectedVersion: m.Version}, Ref: ref}); err != nil {
				return err
			}
		}
		if r.ReviewPath != "" {
			r.ReviewRetained, err = s.options.Files.RemoveReview(ctx, p.OwnerID, r.AssistantID, r.ReviewPath, r.ReviewVersion, func() error { return s.authorize(ctx, p, r.AssistantID, true) })
			if err != nil {
				return err
			}
		}
		r.State = "undone"
		r.Summary = ""
		r.Output = ""
		r.Decisions = nil
		r.UpdatedAt = s.options.Now().UTC()
		for i := range r.Sources {
			r.Sources[i].Text = ""
		}
		out = r
		return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, runs, persistence.Key(p.OwnerID, r.ID), r)
		})
	}()
	return
}
