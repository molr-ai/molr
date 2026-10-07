package dream

import (
	"context"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
	"testing"
	"time"
)

func TestSessionDeadlinePreservesCommittedMemoryAndBoundsBackfill(t *testing.T) {
	f := setup(t)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	first := f.request(t, "first")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Deadline(t.Context(), f.ap, ev(first)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(first), output(first, "explicit")); err != nil {
		t.Fatal(err)
	}
	second := f.request(t, "second")
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	if err != nil || len(events) != 2 {
		t.Fatal("next batch event was deduplicated", events, err)
	}
	if !second.Deadline.Equal(first.Deadline) || second.SessionID != first.SessionID || second.Batch != 2 {
		t.Fatal("batch reset deadline", second)
	}
	f.now = first.Deadline
	if err := f.s.expire(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(second), output(second, "explicit")); core.CodeOf(err) != core.DeadlineExceeded {
		t.Fatal("late memory committed", err)
	}
	page, _ := f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 1 {
		t.Fatal("partial committed batch lost", page)
	}
	retry := f.request(t, "retry-expired")
	if retry.State != "timed_out" || retry.EventID != second.EventID {
		t.Fatal("timeout reset by retry", retry)
	}
	old, _ := persistence.Load[backlog](t.Context(), f.db, backlogs, "owner/assistant")
	if len(old.Sources) != 3 {
		t.Fatal(old)
	}
	if public, err := f.s.GetRun(t.Context(), f.p, first.ID); err != nil || public.Output != "" || public.Sources[0].Text != "" {
		t.Fatal("public run exposed raw snapshots", public, err)
	}
	raw, _ := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key("owner", first.ID))
	if raw.Output == "" || raw.Sources[0].Text == "" {
		t.Fatal("debug evidence discarded")
	}
	f.now = f.now.AddDate(0, 0, 1)
	f.message(t, 100)
	fresh := f.request(t, "next-day")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(fresh)); err != nil {
		t.Fatal(err)
	}
	if fresh.Backfill || len(fresh.Sources) != 1 {
		t.Fatal("old work displaced new work", fresh)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(fresh), `{"Summary":"new day","Memories":[]}`); err != nil {
		t.Fatal(err)
	}
	carry := f.request(t, "carry")
	if !carry.Backfill || len(carry.Sources) != 3 {
		t.Fatal(carry)
	}
	f.now = carry.Deadline
	if err := f.s.expire(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	old, _ = persistence.Load[backlog](t.Context(), f.db, backlogs, "owner/assistant")
	if len(old.Sources) != 0 {
		t.Fatal("old backlog carried a second time", old)
	}
}
func TestSessionFrozenDeadlineAndCancel(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	c, _ := f.s.Get(t.Context(), f.p, "assistant")
	_, err := f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "duration", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := f.s.Deadline(t.Context(), f.ap, ev(r))
	if err != nil || !deadline.Equal(r.Deadline) {
		t.Fatal("configuration extended active dream", deadline, err)
	}
	f.s = New(f.db, f.s.options)
	deadline, err = f.s.Deadline(t.Context(), f.ap, ev(r))
	if err != nil || !deadline.Equal(r.Deadline) {
		t.Fatal("restart reset deadline")
	}
	if err := f.s.Cancel(t.Context(), f.p, "assistant"); err != nil {
		t.Fatal(err)
	}
	if active, err := f.s.Active(t.Context(), f.ap, ev(r)); err != nil || active {
		t.Fatal(active, err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}
func TestWindowsAndRecurringConflictsUseSharedRules(t *testing.T) {
	f := setup(t)
	c, _ := f.s.Get(t.Context(), f.p, "assistant")
	_, err := f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "window", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	ws, err := f.s.Windows(t.Context(), f.p, "assistant", from, to)
	if err != nil || len(ws) != 3 || ws[0].End.Sub(ws[0].Start) != 10*time.Minute {
		t.Fatal(ws, err)
	}
	rule := trigger.Rule{StartAt: from.Add(3*time.Hour + 5*time.Minute), Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: trigger.Coalesce}
	conflicts, err := f.s.Conflicts(t.Context(), f.p, "assistant", rule, from, to)
	if err != nil || len(conflicts) != 3 {
		t.Fatal(conflicts, err)
	}
	rule.StartAt = from.Add(3*time.Hour + 10*time.Minute)
	conflicts, err = f.s.Conflicts(t.Context(), f.p, "assistant", rule, from, to)
	if err != nil || len(conflicts) != 0 {
		t.Fatal("end boundary not exclusive", conflicts, err)
	}
	foreign := f.p
	foreign.OwnerID = "foreign"
	if _, err := f.s.Windows(context.Background(), foreign, "assistant", from, to); err == nil {
		t.Fatal("unauthorized window read")
	}
}

func TestMissedAutomaticWindowDoesNotPreventManualDream(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	c, err := f.s.Get(t.Context(), f.p, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	missed, err := f.s.startSession(t.Context(), c, false, f.now, f.now.Add(-time.Hour))
	if err != nil || missed.State != "missed_window" {
		t.Fatal(missed, err)
	}
	manual := f.request(t, "manual-after-missed")
	if manual.State != "queued" || manual.SessionID == missed.SessionID || !manual.Deadline.Equal(f.now.Add(10*time.Minute)) {
		t.Fatal(manual)
	}
	again := f.request(t, "repeat-manual")
	if again.ID != manual.ID {
		t.Fatal("duplicate manual", again)
	}
}
