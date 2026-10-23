package debugtrace

import (
	"context"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// A count policy includes every trigger, including dreams. Active runs remain
// pinned until completion; age and payload bytes never reject a new detail.
func (s *Service) prune(ctx context.Context, tx persistence.Tx, owner, keep core.ID) error {
	policy, err := s.retention(ctx, tx.Store())
	if err != nil {
		return err
	}
	count, err := tx.Count(ctx, "debug.summaries", visibleQuery(owner))
	if err != nil {
		return err
	}
	if count <= int64(policy.MaxRuns) {
		return nil
	}
	q := visibleQuery(owner)
	q.Where = append(q.Where, persistence.Filter{Field: "State", Op: "!=", Value: "running"})
	q.OrderBy = "StartedAtKey"
	q.ThenBy = "FirstRevision"
	q.Limit = 200
	q.Fields = []string{"ID", "AssistantID", "StartedAt", "FirstRevision"}
	resetMarked := false
	for count > int64(policy.MaxRuns) {
		rows, err := tx.Query(ctx, "debug.summaries", q)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			summary, err := persistence.Decode[Run](row)
			if err != nil {
				return err
			}
			q.After = []any{persistence.TimeKey(summary.StartedAt), summary.FirstRevision, row.Key}
			if summary.ID == keep {
				continue
			}
			run, err := persistence.Load[Run](ctx, tx.Store(), "debug.runs", row.Key)
			if err != nil {
				return err
			}
			for _, step := range run.Steps {
				if err = contextstore.Release(ctx, tx, payloadReference(owner, run.AssistantID, run.ID, step.Sequence)); err != nil {
					return err
				}
				if err = tx.Delete(ctx, "debug.payloads", payloadKey(owner, run.ID, step.Sequence)); err != nil {
					return err
				}
			}
			for _, bucket := range []string{"debug.runs", "debug.summaries"} {
				if err = tx.Delete(ctx, bucket, row.Key); err != nil {
					return err
				}
			}
			if err = tx.Put(ctx, "debug.cleanup", persistence.Key(owner, run.AssistantID), CleanupEvent{At: s.options.Now().UTC(), Policy: policy}); err != nil {
				return err
			}
			// A deletion invalidates summary snapshots without retaining tombstones.
			if !resetMarked {
				var revision uint64
				if err := tx.Get(ctx, "debug.revisions", string(owner), &revision); err != nil && !persistence.Missing(err) {
					return err
				}
				if err := tx.Put(ctx, "debug.summary_resets", string(owner), revision+1); err != nil {
					return err
				}
				resetMarked = true
			}
			count--
			if count <= int64(policy.MaxRuns) {
				break
			}
		}
		if len(rows) < q.Limit {
			break
		}
	}
	return nil
}
