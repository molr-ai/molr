package dream

import (
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
)

func TestTimerSkipsExpiredWindowAndDoesNotCreatePlans(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	if err := f.s.Tick(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.s.Runs(t.Context(), f.p, "assistant")
	if len(rows) != 1 || rows[0].State != "missed_window" {
		t.Fatal(rows)
	}
	events, _ := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	if len(events) != 0 {
		t.Fatal("expired dream was queued")
	}
	for _, bucket := range []string{"business.schedules", "business.tasks", "business.plan-links"} {
		records, _ := persistence.Scan[any](t.Context(), f.db, bucket)
		if len(records) != 0 {
			t.Fatal("dream created plan", bucket)
		}
	}
	f.s = New(f.db, f.s.options)
	if err := f.s.Tick(t.Context(), f.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, _ := persistence.Scan[trigger.Occurrence](t.Context(), f.db, "trigger.occurrences")
	if len(ledger) != 1 {
		t.Fatal("duplicate occurrence", ledger)
	}
}
func TestTimerOccurrenceRunAndReplayCursorRollBackTogether(t *testing.T) {
	f := setup(t)
	f.now = f.now.Add(-55 * time.Minute)
	f.message(t, 1)
	f.db.FailNextCommit(errors.New("failed dream timer commit"))
	if err := f.s.Tick(t.Context(), f.now); err == nil {
		t.Fatal("commit should fail")
	}
	for _, bucket := range []string{configs, timers, runs, "trigger.occurrences", "agent.events"} {
		rows, _ := persistence.Scan[any](t.Context(), f.db, bucket)
		if len(rows) != 0 {
			t.Fatal("partial timer transaction", bucket)
		}
	}
	first, second := New(f.db, f.s.options), New(f.db, f.s.options)
	results := make(chan error, 2)
	go func() { results <- first.Tick(t.Context(), f.now) }()
	go func() { results <- second.Tick(t.Context(), f.now) }()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	events, _ := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	ledger, _ := persistence.Scan[trigger.Occurrence](t.Context(), f.db, "trigger.occurrences")
	if len(events) != 1 || len(ledger) != 1 {
		t.Fatal("concurrent ticks duplicated wake", events, ledger)
	}
}
func TestTimerSettingChangePreservesReplayProgressAndVersionedDedup(t *testing.T) {
	f := setup(t)
	f.now = f.now.Add(-55 * time.Minute)
	f.message(t, 1)
	if err := f.s.Tick(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	before, _ := f.s.Get(t.Context(), f.p, "assistant")
	timer, _ := persistence.Load[timerState](t.Context(), f.db, timers, "owner/assistant")
	req := ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "edit-clock", ExpectedVersion: before.Version}, AssistantID: "assistant", Enabled: true, Hour: 13, Timezone: "Asia/Shanghai"}
	after, err := f.s.Configure(t.Context(), f.p, req)
	if err != nil {
		t.Fatal(err)
	}
	if !after.NextDay.Equal(before.NextDay) {
		t.Fatal("time edit lost replay progress")
	}
	again, err := f.s.Configure(t.Context(), f.p, req)
	if err != nil || again.Version != after.Version {
		t.Fatal(again, err)
	}
	next, _ := persistence.Load[timerState](t.Context(), f.db, timers, "owner/assistant")
	if next.Revision != timer.Revision+1 || next.Rule.Timezone != "Asia/Shanghai" || !next.Through.Equal(timer.Through) {
		t.Fatal("timer revision/backlog lost", next)
	}
	f.s = New(f.db, f.s.options)
	if err := f.s.Tick(t.Context(), f.now.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, _ := persistence.Scan[trigger.Occurrence](t.Context(), f.db, "trigger.occurrences")
	if len(ledger) != 1 {
		t.Fatal("triggered before edited wall-clock time", ledger)
	}
	if err := f.s.Tick(t.Context(), time.Date(2026, 9, 29, 5, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	ledger, _ = persistence.Scan[trigger.Occurrence](t.Context(), f.db, "trigger.occurrences")
	if len(ledger) != 2 {
		t.Fatal("new rule not used", ledger)
	}
	events, _ := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	if len(events) != 2 {
		t.Fatal("expired window prevented the newly configured window", events)
	}
	if err := f.s.Tick(t.Context(), time.Date(2026, 9, 29, 5, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	againEvents, _ := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	if len(againEvents) != 2 {
		t.Fatal("repeated tick duplicated window", againEvents)
	}
}

func TestTimezoneEditKeepsAnAdmittedBatchWindow(t *testing.T) {
	f := setup(t)
	f.now = f.now.Add(-55 * time.Minute)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	if err := f.s.Tick(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.s.Runs(t.Context(), f.p, "assistant")
	first := rows[0]
	if len(first.Sources) != 80 {
		t.Fatal(first)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(first), "batch one"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.s.Get(t.Context(), f.p, "assistant")
	_, err := f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "timezone-edit", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tick(t.Context(), f.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows, _ = f.s.Runs(t.Context(), f.p, "assistant")
	var second Run
	for _, r := range rows {
		if r.State == "queued" {
			second = r
		}
	}
	if len(second.Sources) != 3 || !second.Start.Equal(first.Start) || !second.End.Equal(first.End) || second.Timezone != first.Timezone {
		t.Fatal("timezone changed an active replay window", first, second)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(second), "batch two"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tick(t.Context(), f.now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after, _ := f.s.Get(t.Context(), f.p, "assistant")
	if !after.NextDay.Equal(first.End) {
		t.Fatal("replay cursor overlapped or skipped evidence", after)
	}
}
