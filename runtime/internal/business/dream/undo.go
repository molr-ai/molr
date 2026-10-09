package dream

import (
	"context"
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
		for i := len(r.FileOperations) - 1; i >= 0; i-- {
			retained, err := s.options.Files.UndoFileOperation(ctx, p.OwnerID, r.FileOperations[i], func() error { return s.authorize(ctx, p, r.AssistantID, true) })
			if err != nil {
				return err
			}
			r.UndoRetained = append(r.UndoRetained, retained...)
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
