package debugtrace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestTrajectorySummarySyncStateCleanupAndRollback(t *testing.T) {
	s, p, _ := fixture(t)
	active := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	query := TrajectoryQuery{Query: Query{AssistantID: "assistant"}}
	first, err := s.Trajectory(t.Context(), p, query)
	if err != nil || !first.RunsReset || len(first.Runs) != 1 || first.RunsCursor == "" {
		t.Fatalf("initial snapshot: %+v %v", first, err)
	}
	query.After, query.RunsCursor = first.HeadCursor, first.RunsCursor
	idle, err := s.Trajectory(t.Context(), p, query)
	if err != nil || idle.RunsReset || len(idle.Runs) != 0 || len(idle.Items) != 0 || idle.RunsCursor != first.RunsCursor {
		t.Fatalf("idle sync: %+v %v", idle, err)
	}
	// A failed summary update must advance neither the cursor nor the persisted state.
	s.store.(*sqlitestore.Store).FailNextCommit(errors.New("synthetic commit failure"))
	if err := s.Recover(t.Context()); err == nil {
		t.Fatal("recovery commit did not fail")
	}
	rolledBack, err := s.Trajectory(t.Context(), p, query)
	if err != nil || rolledBack.RunsCursor != first.RunsCursor || len(rolledBack.Runs) != 0 {
		t.Fatalf("rollback leaked: %+v %v", rolledBack, err)
	}
	// Recovery changes run status with no new step, even with a frozen clock.
	restarted := New(s.store, s.options)
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	changed, err := restarted.Trajectory(t.Context(), p, query)
	if err != nil || changed.RunsReset || len(changed.Items) != 0 || len(changed.Runs) != 1 || changed.Runs[0].State != "interrupted" || changed.Runs[0].FinishedAt == nil || !changed.Runs[0].Incomplete {
		t.Fatalf("status-only update: %+v %v", changed, err)
	}
	query.RunsCursor = changed.RunsCursor
	s.Finish(active, "failed", "synthetic failure")
	finished, err := s.Trajectory(t.Context(), p, query)
	if err != nil || len(finished.Runs) != 1 || finished.Runs[0].State != "failed" || len(finished.Items) != 1 {
		t.Fatalf("finish: %+v %v", finished, err)
	}
	query.After, query.RunsCursor = finished.HeadCursor, finished.RunsCursor
	s.options.MaxRuns = 1
	s.store.(*sqlitestore.Store).FailNextCommit(errors.New("synthetic cleanup rollback"))
	s.Begin(t.Context(), p.OwnerID, "assistant", "rolled-back", "rolled-back", "user_message", 1, nil)
	unchanged, err := s.Trajectory(t.Context(), p, query)
	if err != nil || unchanged.RunsReset || len(unchanged.Runs) != 0 || unchanged.RunsCursor != finished.RunsCursor {
		t.Fatalf("rolled-back cleanup invalidated summaries: %+v %v", unchanged, err)
	}
	s.Begin(t.Context(), p.OwnerID, "assistant", "new-event", "new-execution", "user_message", 1, nil)
	cleaned, err := s.Trajectory(t.Context(), p, query)
	if err != nil || !cleaned.RunsReset || len(cleaned.Runs) != 1 || cleaned.Runs[0].ID == first.Runs[0].ID || cleaned.Cleanup == nil {
		t.Fatalf("cleanup failed to replace retained snapshot: %+v %v", cleaned, err)
	}
	query.After, query.RunsCursor = cleaned.HeadCursor, cleaned.RunsCursor
	quiet, err := s.Trajectory(t.Context(), p, query)
	if err != nil || quiet.RunsReset || len(quiet.Runs) != 0 || len(quiet.Items) != 0 {
		t.Fatalf("cleanup repeated: %+v %v", quiet, err)
	}
}

func TestTrajectorySummaryCursorIndependentOfStepPaging(t *testing.T) {
	s, p, _ := fixture(t)
	query := TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 1}}}
	initial, err := s.Trajectory(t.Context(), p, query)
	if err != nil {
		t.Fatal(err)
	}
	query.After, query.RunsCursor = initial.HeadCursor, initial.RunsCursor
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.start", "started", "operation", nil)
	s.Record(ctx, "model.complete", "succeeded", "operation", map[string]string{"Text": "answer"})
	s.Finish(ctx, "succeeded", "")
	second := s.Begin(t.Context(), p.OwnerID, "assistant", "second", "second", "user_message", 1, nil)
	s.Record(second, "model.start", "started", "operation", nil)
	s.Record(second, "model.complete", "succeeded", "operation", nil)
	s.Finish(second, "succeeded", "")
	count := 0
	for {
		page, err := s.Trajectory(t.Context(), p, query)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 && len(page.Runs) != 2 {
			t.Fatal("first delta missed new run")
		}
		if count > 0 && len(page.Runs) != 0 {
			t.Fatal("later backfill page resent unchanged summaries")
		}
		if page.RunsReset || len(page.Items) != 1 {
			t.Fatalf("missing backfill: %+v", page)
		}
		count++
		query.RunsCursor = page.RunsCursor
		if page.NextCursor == "" {
			query.After = page.HeadCursor
			break
		}
		query.After = page.NextCursor
		if count > 10 {
			t.Fatal("backfill did not finish")
		}
	}
	if count != 8 {
		t.Fatalf("steps=%d want 8", count)
	}
	// Loading older steps must not move the live step cursor or erase summaries.
	history := TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 1}}, RunsCursor: query.RunsCursor}
	count = 0
	for {
		page, err := s.Trajectory(t.Context(), p, history)
		if err != nil {
			t.Fatal(err)
		}
		if page.RunsReset || len(page.Runs) != 0 || len(page.Items) != 1 {
			t.Fatalf("historical delta: %+v", page)
		}
		count++
		history.Page.Cursor = page.NextCursor
		if history.Page.Cursor == "" {
			break
		}
		if count > 10 {
			t.Fatal("history did not finish")
		}
	}
	if count != 8 {
		t.Fatal("historical page lost steps", count)
	}
	s.Record(ctx, "checkpoint.saved", "succeeded", "", nil)
	live, err := s.Trajectory(t.Context(), p, query)
	if err != nil || len(live.Items) != 1 || live.Items[0].Step.Kind != "checkpoint.saved" || len(live.Runs) != 1 {
		t.Fatalf("live update after history: %+v %v", live, err)
	}
}

func TestTrajectorySummarySyncScopeResetAndAuthorization(t *testing.T) {
	s, p, now := fixture(t)
	s.options.MaxRuns = 20
	first, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}})
	if err != nil {
		t.Fatal(err)
	}
	query := TrajectoryQuery{Query: Query{AssistantID: "assistant"}, After: first.HeadCursor, RunsCursor: first.RunsCursor}
	s.Begin(t.Context(), p.OwnerID, "other", "event", "execution", "user_message", 1, nil)
	s.Begin(t.Context(), "foreign", "assistant", "foreign", "foreign", "user_message", 1, nil)
	reset := now.AddDate(0, 0, 1)
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.context_reset_at", "owner/assistant", reset)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Trajectory(t.Context(), p, query)
	if err != nil || len(page.Items) != 0 || len(page.Runs) != 0 || page.RunsReset || page.ContextResetAt == nil || !page.ContextResetAt.Equal(reset) {
		t.Fatalf("scope/reset: %+v %v", page, err)
	}
	// An owner-wide client receives all its assistants, but no other owner's data.
	all, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len(all.Runs) != 1 {
		t.Fatalf("owner snapshot: %+v %v", all, err)
	}
	global := TrajectoryQuery{After: all.HeadCursor, RunsCursor: all.RunsCursor}
	s.Begin(t.Context(), p.OwnerID, "assistant", "next", "next", "user_message", 1, nil)
	delta, err := s.Trajectory(t.Context(), p, global)
	if err != nil || len(delta.Runs) != 1 || delta.Runs[0].AssistantID != "assistant" {
		t.Fatalf("owner delta: %+v %v", delta, err)
	}
	future, _ := json.Marshal(runsCursor{Kind: "runs", Owner: p.OwnerID, Assistant: "assistant", Revision: 1 << 40})
	for _, cursor := range []string{"invalid", strings.Repeat("a", 2049), first.HeadCursor, all.RunsCursor, base64.RawURLEncoding.EncodeToString(future)} {
		invalid := query
		invalid.RunsCursor = cursor
		if _, err := s.Trajectory(t.Context(), p, invalid); core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("invalid cursor accepted: %v", err)
		}
	}
	foreign := p
	foreign.OwnerID, foreign.SubjectID = "foreign", "foreign"
	if _, err := s.Trajectory(t.Context(), foreign, TrajectoryQuery{Query: query.Query, RunsCursor: first.RunsCursor}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("foreign summary cursor accepted", err)
	}
	s.options.Authorize = func(context.Context, core.Principal) error { return core.NewError(core.Forbidden, "revoked") }
	if _, err := s.Trajectory(t.Context(), p, query); core.CodeOf(err) != core.Forbidden {
		t.Fatal("incremental sync bypassed revoked authorization", err)
	}
}
