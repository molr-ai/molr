package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"testing"
	"time"
)

func seedBrowse(t *testing.T, s *Coordinator, count int) (core.Object, time.Time) {
	t.Helper()
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	ref := core.Object{Kind: "schedule", ID: "series"}
	text := strings.Repeat("snapshot-only-", 500)
	err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		source := schedule.Schedule{ID: ref.ID, OwnerID: "owner", AssistantID: "assistant", Kind: schedule.AssistantSchedule, Title: "Recurring review", Instructions: text, Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY", MissedPolicy: schedule.Skip}}
		if err := tx.Put(ctx, "business.schedules", persistence.Key("owner", ref.ID), source); err != nil {
			return err
		}
		for i := 0; i < count; i++ {
			occurrence := at.AddDate(0, 0, i-count)
			run := task.Task{ID: core.ID(fmt.Sprintf("run-%04d", i)), OwnerID: "owner", AssistantID: "assistant", Plan: ref, State: task.Completed, PlanTitle: "Immutable title", PlanInstructions: text, Goal: text, CreatedAt: occurrence, UpdatedAt: occurrence.Add(time.Minute), OccurrenceAt: &occurrence}
			if err := tx.Put(ctx, "business.tasks", persistence.Key("owner", run.ID), run); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref, at
}
func TestPlanSummariesStaySmallWithLongExecutionHistory(t *testing.T) {
	s, _ := fixture(t)
	ref, at := seedBrowse(t, s, 160)
	legacy, err := s.List(t.Context(), owner(), at, at.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(legacy)
	overview, err := s.Overview(t.Context(), owner(), at, at.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(overview)
	if len(old) < 3_000_000 || len(raw) > 8000 || strings.Contains(string(raw), "snapshot-only") {
		t.Fatalf("payload old=%d overview=%d", len(old), len(raw))
	}
	if overview[0].ExecutionCount != 160 || len(overview[0].Executions) != 1 || len(overview[0].Occurrences) != 7 {
		t.Fatalf("summary lost state: %+v", overview[0])
	}
	events, err := s.Events(t.Context(), owner(), EventQuery{From: at, Page: core.PageRequest{Limit: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Items) != 50 || events.Items[0].Plan != events.Items[49].Plan {
		t.Fatal("events did not reuse one summary")
	}
	raw, _ = json.Marshal(events)
	if len(raw) > 100_000 || strings.Contains(string(raw), "snapshot-only") {
		t.Fatalf("event page still contains histories: %d", len(raw))
	}
	detail, err := s.Detail(t.Context(), owner(), ref, nil)
	if err != nil || !strings.Contains(detail.Schedule.Instructions, "snapshot-only") || detail.ExecutionCount != 160 || len(detail.Executions) != 1 {
		t.Fatal("editable detail lost source/state", err)
	}
	run, err := s.Execution(t.Context(), owner(), ref, "run-0000")
	if err != nil || run.Title != "Immutable title" || !strings.Contains(run.Instructions, "snapshot-only") || run.Task == nil {
		t.Fatal("execution detail lost snapshot", err)
	}
	t.Logf("160 histories: old calendar %d bytes, new calendar %d bytes, 50 events before HTTP dedup %d bytes", len(old), func() int { v, _ := json.Marshal(overview); return len(v) }(), len(raw))
}
func TestPlanHistoryPagesAndAccessBoundaries(t *testing.T) {
	s, db := fixture(t)
	ref, at := seedBrowse(t, s, 25)
	// Equal instants and mixed personal/task histories must paginate exactly once.
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bucket, key("owner", ref), Links{Ref: ref, OwnerID: "owner", Handoff: "notify", Runs: []Execution{{ID: "inline-a", State: "completed", StartedAt: at, Result: "private-result"}, {ID: "inline-b", State: "completed", StartedAt: at, Result: "private-result"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	q := core.PageRequest{Limit: 4}
	seen := map[core.ID]bool{}
	firstCursor := ""
	for {
		page, err := s.History(t.Context(), owner(), ref, q)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if strings.Contains(string(raw), "private-result") || strings.Contains(string(raw), "snapshot-only") {
			t.Fatal("history list exposed bodies")
		}
		for _, run := range page.Items {
			if seen[run.ID] {
				t.Fatal("duplicate history", run.ID)
			}
			seen[run.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if firstCursor == "" {
			firstCursor = page.NextCursor
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 27 {
		t.Fatal("missing history", len(seen))
	}
	run, err := s.Execution(t.Context(), owner(), ref, "inline-a")
	if err != nil || run.Result != "private-result" {
		t.Fatal("inline detail", err)
	}
	wrong := core.Object{Kind: "todo", ID: "wrong"}
	add(t, s, wrong.ID, "")
	if _, err := s.History(t.Context(), owner(), wrong, core.PageRequest{Limit: 4, Cursor: firstCursor}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("cross-plan cursor accepted", err)
	}
	if _, err := s.Execution(t.Context(), owner(), wrong, "run-0000"); core.CodeOf(err) != core.NotFound {
		t.Fatal("cross-plan execution exposed", err)
	}
	foreign := owner()
	foreign.OwnerID = "foreign"
	foreign.SubjectID = "foreign"
	if _, err := s.History(t.Context(), foreign, ref, core.PageRequest{Cursor: firstCursor}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("cross-owner cursor accepted", err)
	}
	assistant := core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "other-assistant", ExecutionID: "execution"}
	for name, read := range map[string]func() error{
		"detail":    func() error { _, e := s.Detail(t.Context(), assistant, ref, nil); return e },
		"history":   func() error { _, e := s.History(t.Context(), assistant, ref, core.PageRequest{}); return e },
		"execution": func() error { _, e := s.Execution(t.Context(), assistant, ref, "run-0000"); return e },
	} {
		if err := read(); core.CodeOf(err) != core.Forbidden {
			t.Fatal(name, "assistant isolation", err)
		}
	}
	if _, err := s.History(t.Context(), owner(), ref, core.PageRequest{Cursor: "broken"}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.History(ctx, owner(), ref, core.PageRequest{}); err == nil {
		t.Fatal("canceled history succeeded")
	}
}
func TestCalendarAndEventSummaryKeepOccurrenceStateAndDependencies(t *testing.T) {
	s, db := fixture(t)
	ref, at := seedBrowse(t, s, 4)
	dep := add(t, s, "dependency", "")
	next := add(t, s, "successor", "")
	second := at.AddDate(0, 0, 1)
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, link := range []Links{
			{Ref: dep, OwnerID: "owner", Runs: []Execution{{ID: "dep-done", State: "completed", StartedAt: at}}},
			{Ref: next, OwnerID: "owner", Dependencies: []core.Object{ref}},
			{Ref: ref, OwnerID: "owner", Dependencies: []core.Object{dep}, Runs: []Execution{
				{ID: "old-failed", State: "failed", StartedAt: at, OccurrenceAt: &second},
				{ID: "new-done", State: "completed", StartedAt: at.Add(time.Hour), OccurrenceAt: &second},
			}},
		} {
			if err := tx.Put(ctx, bucket, key(link.OwnerID, link.Ref), link); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	overview, err := s.Overview(t.Context(), owner(), at, at.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range overview {
		if item.Ref == ref {
			if !item.Ready || !item.HasSuccessors || len(item.Executions) != 1 || item.Executions[0].State != "completed" {
				t.Fatalf("lost readiness/state: %+v", item)
			}
		}
	}
	for state, n := range map[string]int{"all": 3, "open": 3, "done": 1} {
		page, err := s.Events(t.Context(), owner(), EventQuery{From: at, State: state, Page: core.PageRequest{Limit: 3}})
		if err != nil || len(page.Items) != n {
			t.Fatal(state, len(page.Items), err)
		}
		if state == "done" && !page.Items[0].At.Equal(second) {
			t.Fatal("wrong completed occurrence")
		}
	}
}
