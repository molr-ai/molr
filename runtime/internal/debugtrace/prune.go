package debugtrace

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func retainedScope(owner core.ID) persistence.Query {
	return persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "Trigger", Op: "!=", Value: "dream_due"}}}
}

func retentionUsage(ctx context.Context, tx persistence.Tx, owner core.ID) (int64, int64, error) {
	scope := retainedScope(owner)
	count, err := tx.Count(ctx, "debug.summaries", scope)
	if err != nil {
		return 0, 0, err
	}
	total, err := tx.Sum(ctx, "debug.summaries", scope, "Bytes")
	return count, total, err
}

// Retention runs inside the caller's transaction. Count/bytes stay in SQLite;
// only protected IDs and bounded batches of possible deletions enter Go memory.
func (s *Service) prune(ctx context.Context, tx persistence.Tx, owner, keep core.ID, extra int) error {
	policy, err := s.retention(ctx, tx.Store())
	if err != nil {
		return err
	}
	count, total, err := retentionUsage(ctx, tx, owner)
	if err != nil {
		return err
	}
	total += int64(extra)
	protected, err := s.protected(ctx, tx.Store(), owner, policy)
	if err != nil {
		return err
	}
	cutoff := s.options.Now().AddDate(0, 0, -policy.RetentionDays)
	query := retainedScope(owner)
	query.Where = append(query.Where, persistence.Filter{Field: "State", Op: "!=", Value: "running"})
	if count <= int64(policy.MaxRuns) && total <= int64(policy.MaxBytes) {
		query.Where = append(query.Where, persistence.Filter{Field: "StartedAtKey", Op: "<", Value: persistence.TimeKey(cutoff)})
	}
	query.OrderBy, query.ThenBy = "StartedAtKey", "FirstRevision"
	query.Limit = 200
	query.Fields = []string{"ID", "AssistantID", "Bytes", "StartedAt", "FirstRevision"}
	for {
		runs, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", query)
		if err != nil {
			return err
		}
		for _, run := range runs {
			query.After = []any{persistence.TimeKey(run.StartedAt), run.FirstRevision, persistence.Key(owner, run.ID)}
			if run.ID == keep || protected[run.ID] {
				continue
			}
			if !run.StartedAt.Before(cutoff) && count <= int64(policy.MaxRuns) && total <= int64(policy.MaxBytes) {
				return nil
			}
			full, err := persistence.Load[Run](ctx, tx.Store(), "debug.runs", persistence.Key(owner, run.ID))
			if err != nil {
				return err
			}
			for _, step := range full.Steps {
				if err := tx.Delete(ctx, "debug.payloads", payloadKey(owner, run.ID, step.Sequence)); err != nil {
					return err
				}
			}
			for _, bucket := range []string{"debug.runs", "debug.summaries"} {
				if err := tx.Delete(ctx, bucket, persistence.Key(owner, run.ID)); err != nil {
					return err
				}
			}
			if err := tx.Put(ctx, "debug.cleanup", persistence.Key(owner, run.AssistantID), CleanupEvent{At: s.options.Now().UTC(), Policy: policy}); err != nil {
				return err
			}
			count--
			total -= int64(run.Bytes)
		}
		if len(runs) < query.Limit {
			break
		}
	}
	// Protected data wins over capacity; callers may still save metadata without payloads.
	if extra > 0 && total > int64(policy.MaxBytes) {
		return core.NewError(core.FailedPrecondition, "trace capacity reached")
	}
	return nil
}
