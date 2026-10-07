package plan

import (
	"context"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"
)

func TestEventsMergeRecurringOccurrencesAndBindPagination(t *testing.T) {
	s, db := fixture(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	for _, spec := range []struct{ id, rule, owner string }{{"daily", "FREQ=DAILY;COUNT=3", "owner"}, {"yearly", "FREQ=YEARLY;COUNT=3", "owner"}, {"foreign", "FREQ=DAILY", "other"}} {
		err := schedule.NewRepository(db).Insert(ctx, schedule.Schedule{ID: core.ID(spec.id), OwnerID: core.ID(spec.owner), AssistantID: "assistant", Kind: schedule.AssistantPlan, Title: spec.id, Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: spec.rule, MissedPolicy: schedule.Coalesce}})
		if err != nil {
			t.Fatal(err)
		}
	}
	when := at.Add(time.Hour)
	if err := todo.NewRepository(db).Insert(ctx, todo.Todo{ID: "personal", OwnerID: "owner", Title: "personal", DueAt: &when, Version: 1}); err != nil {
		t.Fatal(err)
	}
	q := EventQuery{From: at, Scope: "all", Kind: "all", State: "all", Page: core.PageRequest{Limit: 2}}
	var items []Event
	seen := map[string]bool{}
	for {
		page, err := s.Events(ctx, owner(), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range page.Items {
			if seen[v.Key] {
				t.Fatal("duplicate page event", v.Key)
			}
			seen[v.Key] = true
			items = append(items, v)
		}
		if page.NextCursor == "" {
			break
		}
		q.Page.Cursor = page.NextCursor
	}
	if len(items) != 7 {
		t.Fatal("series collapsed or sparse future events lost", items)
	}
	for i, v := range items {
		if v.Plan.Title == "foreign" {
			t.Fatal("cross-owner event")
		}
		if i > 0 && v.At.Before(items[i-1].At) {
			t.Fatal("not chronological")
		}
	}
	if items[6].At.Year() != 2028 {
		t.Fatal("upcoming projection confined to calendar range")
	}
	first, _ := s.Events(ctx, owner(), EventQuery{From: at, Page: core.PageRequest{Limit: 1}})
	bad := EventQuery{From: at, Scope: "me", Page: core.PageRequest{Limit: 1, Cursor: first.NextCursor}}
	if _, err := s.Events(ctx, owner(), bad); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("cursor accepted changed filter", err)
	}
	other := owner()
	other.OwnerID = "other"
	other.SubjectID = "other"
	bad.Scope = ""
	if _, err := s.Events(ctx, other, bad); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("cross-owner cursor", err)
	}
	personal, err := s.Events(ctx, owner(), EventQuery{From: at, Scope: "me"})
	if err != nil || len(personal.Items) != 1 {
		t.Fatal(personal, err)
	}
	events, _ := persistence.Scan[map[string]any](ctx, db, "agent.events")
	if len(events) != 0 {
		t.Fatal("read projection started work")
	}
}
func TestEventsCompletionAppliesToOneOccurrence(t *testing.T) {
	s, db := fixture(t)
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	ref := core.Object{Kind: "schedule", ID: "recurring"}
	err := schedule.NewRepository(db).Insert(t.Context(), schedule.Schedule{ID: ref.ID, OwnerID: "owner", Kind: schedule.Personal, Title: "daily", Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: schedule.Coalesce}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation("complete-one", 0), Ref: ref, Action: "complete", OccurrenceAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	for state, count := range map[string]int{"all": 3, "open": 2, "done": 1} {
		page, err := s.Events(t.Context(), owner(), EventQuery{From: at, State: state})
		if err != nil || len(page.Items) != count {
			t.Fatal(state, page, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.Events(ctx, owner(), EventQuery{From: at}); err == nil {
		t.Fatal("canceled query succeeded")
	}
}
