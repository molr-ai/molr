package plan

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Resolve follows Overview's todo-first ordering and visibility without loading
// unrelated sources, dependencies or execution histories. Detail authorizes the
// resolved object separately, just as selection through Overview did.
func (s *Coordinator) Resolve(ctx context.Context, p core.Principal, id core.ID) (ref core.Object, err error) {
	if err = id.Validate(); err != nil {
		return
	}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{}, policy.Read); err != nil {
			return err
		}
		for _, kind := range []string{"todo", "schedule", "watch"} {
			var source struct{ ID, OwnerID, AssistantID core.ID }
			bucket := "business." + kind + "s"
			if kind == "watch" {
				bucket = "business.watches"
			}
			err := tx.Get(ctx, bucket, persistence.Key(p.OwnerID, id), &source)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			if source.ID == id && source.OwnerID == p.OwnerID && (p.Kind != core.ActorAssistant || source.AssistantID == p.SubjectID) {
				ref = core.Object{Kind: kind, ID: id}
				return nil
			}
		}
		return core.NewError(core.NotFound, "plan not found")
	})
	return
}
