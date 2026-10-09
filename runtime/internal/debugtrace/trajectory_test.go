package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestTrajectoryPagingLiveBackfillAndScopedCursors(t *testing.T) {
	s, p, _ := fixture(t)
	s.options.MaxRuns = 10
	initial, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}})
	if err != nil || initial.HeadCursor == "" || len(initial.Items) != 0 {
		t.Fatalf("empty tail: %+v %v", initial, err)
	}
	first := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	// Deliberately identical wall times and operation IDs across attempts. The
	// capture revision must still deliver every new record once, in commit order.
	s.Record(first, "tool.start", "started", "operation", map[string]any{"Name": "search", "Arguments": map[string]string{"query": "hello", "api_key": "SECRET"}})
	s.Record(first, "tool.complete", "succeeded", "operation", map[string]any{"Name": "search", "Result": map[string]any{"Content": map[string]string{"text": "found"}}})
	s.Finish(first, "succeeded", "")
	second := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 2, nil)
	s.Record(second, "model.start", "started", "operation", map[string]string{"ModelID": "model"})
	s.Record(second, "model.complete", "succeeded", "operation", map[string]string{"Text": "answer"})
	s.Finish(second, "succeeded", "")
	s.Begin(t.Context(), p.OwnerID, "another", "other", "other", "schedule_due", 1, nil)
	after := initial.HeadCursor
	seen := map[string]bool{}
	var last uint64
	for {
		page, e := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 2}}, After: after})
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Runs) != 2 {
			t.Fatalf("wrong run scope: %+v", page.Runs)
		}
		for _, run := range page.Runs {
			if len(run.Steps) != 0 {
				t.Fatal("summary included steps")
			}
		}
		for _, entry := range page.Items {
			key := fmt.Sprintf("%s:%d", entry.RunID, entry.Step.Sequence)
			if seen[key] || entry.Step.Revision <= last {
				t.Fatal("duplicate or nonmonotonic append", key)
			}
			seen[key] = true
			last = entry.Step.Revision
			if strings.Contains(entry.Step.Preview, "SECRET") {
				t.Fatal("unsanitized preview")
			}
		}
		if page.NextCursor == "" {
			after = page.HeadCursor
			break
		}
		after = page.NextCursor
	}
	if len(seen) != 8 {
		t.Fatalf("missed backfill: %d", len(seen))
	}
	empty, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}, After: after})
	if err != nil || len(empty.Items) != 0 || empty.HeadCursor != after {
		t.Fatal("tail repeated entries")
	}
	cursor := ""
	count := 0
	for {
		page, e := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 3, Cursor: cursor}}})
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range page.Items {
			key := fmt.Sprintf("%s:%d", entry.RunID, entry.Step.Sequence)
			if !seen[key] {
				t.Fatal("unknown or duplicated historical entry")
			}
			delete(seen, key)
			count++
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if count != 8 || len(seen) != 0 {
		t.Fatal("historical window not complete")
	}
	for _, q := range []TrajectoryQuery{
		{Query: Query{AssistantID: "another"}, After: after},
		{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Cursor: after}}, After: after},
		{After: "invalid"}, {After: strings.Repeat("a", 2049)},
	} {
		if _, e := s.Trajectory(t.Context(), p, q); core.CodeOf(e) != core.InvalidArgument {
			t.Fatalf("invalid cursor accepted: %v", e)
		}
	}
	s.options.Enabled = false
	if _, e := s.Trajectory(t.Context(), p, TrajectoryQuery{}); core.CodeOf(e) != core.Forbidden {
		t.Fatal("disabled endpoint accessible")
	}
}

func TestTrajectoryRetentionLegacyPreviewAndIncompletePayload(t *testing.T) {
	s, p, now := fixture(t)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.complete", "succeeded", "op", map[string]string{"Text": "old answer"})
	s.Finish(ctx, "succeeded", "")
	list, _ := s.Runs(t.Context(), p, Query{})
	id := list.Items[0].ID
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		run, err := persistence.Load[Run](ctx, tx.Store(), "debug.runs", persistence.Key(p.OwnerID, id))
		if err != nil {
			return err
		}
		for i := range run.Steps {
			run.Steps[i].Revision = 0
			run.Steps[i].Preview = ""
		}
		return saveRun(ctx, tx, run)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 2}}})
	if err != nil || page.NextCursor == "" || page.Items[0].Step.Preview != "old answer" {
		t.Fatalf("legacy hydration: %+v %v", page, err)
	}
	*now = now.AddDate(0, 0, 8)
	s.options.MaxRuns = 1
	newer := s.Begin(t.Context(), p.OwnerID, "assistant", "new-event", "new-execution", "schedule_due", 1, nil)
	s.Record(newer, "model.complete", "succeeded", "op", map[string]string{"Text": strings.Repeat("x", 4096)})
	s.Finish(newer, "failed", "capture unavailable")
	older, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Cursor: page.NextCursor}}})
	if err != nil || len(older.Items) != 0 {
		t.Fatal("pruned cursor must remain usable", err)
	}
	fresh, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}, After: page.HeadCursor})
	if err != nil || len(fresh.Runs) != 1 || fresh.Runs[0].ID == id || !fresh.Runs[0].Incomplete {
		t.Fatal("retention not reflected", err)
	}
	if fresh.Items[1].Step.Preview != "" || fresh.Items[1].Step.DataStatus != "too_large" {
		t.Fatal("oversized content leaked into preview")
	}
	reset := now.Add(time.Second)
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.context_reset_at", persistence.Key(p.OwnerID, "assistant"), reset)
	}); err != nil {
		t.Fatal(err)
	}
	fresh, err = s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}, After: fresh.HeadCursor})
	if err != nil || fresh.ContextResetAt == nil || !fresh.ContextResetAt.Equal(reset) {
		t.Fatal("reset boundary unavailable")
	}
}

func TestTrajectoryOwnerIsolationAndPreviewBounds(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.complete", "succeeded", "op", map[string]string{"Text": strings.Repeat("中", 400)})
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len([]rune(page.Items[1].Step.Preview)) != 241 {
		t.Fatal("preview not bounded by characters", err)
	}
	foreign := p
	foreign.OwnerID = "foreign"
	foreign.SubjectID = "foreign"
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", "foreign", map[string]bool{"WorkbenchVisible": true})
	}); err != nil {
		t.Fatal(err)
	}
	other, err := s.Trajectory(t.Context(), foreign, TrajectoryQuery{})
	if err != nil || len(other.Items) != 0 || len(other.Runs) != 0 {
		t.Fatal("foreign trace leaked", err)
	}
	if _, err := s.Trajectory(t.Context(), foreign, TrajectoryQuery{After: page.HeadCursor}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("foreign cursor accepted")
	}
	agent := core.Principal{OwnerID: p.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	if _, err := s.Trajectory(t.Context(), agent, TrajectoryQuery{}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("agent accessed owner diagnostics")
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "Inputs") {
		t.Fatal("full context included in lightweight page")
	}
}
