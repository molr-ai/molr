package debugtrace

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Summary revisions also cover state changes without a new trajectory step.
type storedSummary struct {
	Run
	UpdatedRevision uint64
}
type runsCursor struct {
	Kind             string
	Owner, Assistant core.ID
	Revision         uint64
}

func (s *Service) syncRuns(ctx context.Context, tx persistence.Tx, owner, assistant core.ID, cursor string, query persistence.Query) ([]Run, bool, string, error) {
	var revision, resetRevision uint64
	if err := tx.Get(ctx, "debug.revisions", string(owner), &revision); err != nil && !persistence.Missing(err) {
		return nil, false, "", err
	}
	if err := tx.Get(ctx, "debug.summary_resets", string(owner), &resetRevision); err != nil && !persistence.Missing(err) {
		return nil, false, "", err
	}
	var boundary runsCursor
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &boundary) != nil || boundary.Kind != "runs" || boundary.Owner != owner || boundary.Assistant != assistant || boundary.Revision > revision {
			return nil, false, "", core.Invalid("runs_cursor", "invalid summary cursor")
		}
	}
	reset := cursor == "" || resetRevision > boundary.Revision
	if !reset {
		query.Where = append(query.Where, persistence.Filter{Field: "UpdatedRevision", Op: ">", Value: boundary.Revision})
	}
	// Ordering happens after retrieval; a key sort must not favor a history scan.
	query.Unordered = true
	runs, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", query)
	if err != nil {
		return nil, false, "", err
	}
	for i := range runs {
		runs[i].Steps = nil
		if runs[i].Source == nil {
			runs[i].Source = sourceSummary(s.sourceForRun(ctx, tx, runs[i]))
		}
	}
	raw, _ := json.Marshal(runsCursor{Kind: "runs", Owner: owner, Assistant: assistant, Revision: revision})
	return runs, reset, base64.RawURLEncoding.EncodeToString(raw), nil
}
