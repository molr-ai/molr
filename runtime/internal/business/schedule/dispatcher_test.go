package schedule

import (
	"errors"
	"sort"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func dispatchFixture(t *testing.T, trigger Trigger, kind Kind) (*sqlitestore.Store, Schedule) {
	t.Helper()
	db := fixtureDB(t)
	fixturePut(t, db, "business.assistants", "owner/assistant", struct {
		OwnerID core.ID
		State   string
	}{"owner", "active"})
	first, err := firstOccurrence(trigger)
	testkit.RequireNoError(t, err)
	plan := Schedule{ID: "plan", OwnerID: "owner", Kind: kind, AssistantID: "assistant", Title: "Daily brief", Trigger: trigger, Enabled: true, NextAt: first, Version: 1}
	if kind == Personal {
		plan.AssistantID = ""
	}
	testkit.RequireNoError(t, NewRepository(db).Insert(testkit.Context(t), plan))
	return db, plan
}
func dispatchEvents(t *testing.T, db persistence.Store) []event.AssistantEvent {
	t.Helper()
	events, err := persistence.Scan[event.AssistantEvent](testkit.Context(t), db, "agent.events")
	testkit.RequireNoError(t, err)
	sort.Slice(events, func(i, j int) bool { return events[i].OccurrenceAt.Before(events[j].OccurrenceAt) })
	return events
}
func TestTDDDispatcherMissedPoliciesAreBoundedAndAdvance(t *testing.T) {
	start := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		policy MissedPolicy
		limit  int
		hours  []int
	}{{Skip, 0, nil}, {Coalesce, 0, []int{3}}, {CatchUp, 2, []int{2, 3}}} {
		t.Run(string(tc.policy), func(t *testing.T) {
			db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", Recurrence: "FREQ=HOURLY", MissedPolicy: tc.policy, MaxCatchUp: tc.limit}, AssistantPlan)
			ctx := testkit.Context(t)
			dispatcher := NewDispatcher(db)
			now := start.Add(3*time.Hour + 2*time.Minute)
			testkit.RequireNoError(t, dispatcher.Tick(ctx, now))
			events := dispatchEvents(t, db)
			if len(events) != len(tc.hours) || fixtureCount(t, db, "trigger.occurrences") != len(tc.hours) {
				t.Fatalf("unexpected catch-up events: %+v", events)
			}
			for i, hour := range tc.hours {
				e := events[i]
				if !e.OccurrenceAt.Equal(start.Add(time.Duration(hour)*time.Hour)) || e.Kind != event.ScheduleDue || e.ScheduleID != plan.ID || e.ScheduleVersion != 1 || e.MessageID != "" {
					t.Fatalf("source lost: %+v", e)
				}
			}
			current, err := NewRepository(db).Get(ctx, "owner", plan.ID)
			testkit.RequireNoError(t, err)
			if current.NextAt == nil || !current.NextAt.Equal(start.Add(4*time.Hour)) || current.Version != 2 {
				t.Fatalf("next time/version: %+v", current)
			}
			testkit.RequireNoError(t, dispatcher.Tick(ctx, now))
			if len(dispatchEvents(t, db)) != len(events) {
				t.Fatal("replayed tick duplicated occurrence")
			}
		})
	}
}
func TestTDDDispatcherPersonalAndDismissedPlansNeverWakeAgent(t *testing.T) {
	for _, kind := range []Kind{Personal, AssistantPlan} {
		t.Run(string(kind), func(t *testing.T) {
			start := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
			db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: Skip}, kind)
			if kind == AssistantPlan {
				fixturePut(t, db, "business.assistants", "owner/assistant", struct {
					OwnerID core.ID
					State   string
				}{"owner", "dismissed"})
			}
			testkit.RequireNoError(t, NewDispatcher(db).Tick(testkit.Context(t), start.Add(30*time.Second)))
			if len(dispatchEvents(t, db)) != 0 {
				t.Fatal("personal/dismissed plan woke assistant")
			}
			wanted := 0
			if kind == Personal {
				wanted = 1
			}
			if n := fixtureCount(t, db, "trigger.occurrences"); n != wanted {
				t.Fatalf("occurrences=%d want %d", n, wanted)
			}
			current, err := NewRepository(db).Get(testkit.Context(t), "owner", plan.ID)
			testkit.RequireNoError(t, err)
			if current.Enabled || current.NextAt != nil {
				t.Fatal("finished or dismissed plan remains runnable")
			}
		})
	}
}
func TestTDDDispatcherOccurrenceEventAndProgressRollBackTogether(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: Skip}, AssistantPlan)
	ctx := testkit.Context(t)
	dispatcher := NewDispatcher(db)
	db.FailNextCommit(errors.New("commit interrupted"))
	if err := dispatcher.Tick(ctx, start); err == nil {
		t.Fatal("failed commit succeeded")
	}
	if fixtureCount(t, db, "trigger.occurrences") != 0 || len(dispatchEvents(t, db)) != 0 || fixtureCount(t, db, "business.schedule-progress") != 0 {
		t.Fatal("partial occurrence/event/progress transaction")
	}
	current, err := NewRepository(db).Get(ctx, "owner", plan.ID)
	testkit.RequireNoError(t, err)
	if !current.Enabled || current.Version != 1 || !current.NextAt.Equal(start) {
		t.Fatal("failed commit advanced schedule")
	}
	testkit.RequireNoError(t, dispatcher.Tick(ctx, start))
	if len(dispatchEvents(t, db)) != 1 {
		t.Fatal("retry did not deliver exactly once")
	}
}
func TestTDDDispatcherTimezoneAndCountSurviveRepeatedTicks(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	testkit.RequireNoError(t, err)
	start := time.Date(2026, 3, 7, 9, 0, 0, 0, location)
	db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: location.String(), Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: Skip}, AssistantPlan)
	dispatcher := NewDispatcher(db)
	ctx := testkit.Context(t)
	for day := 0; day < 3; day++ {
		at := start.AddDate(0, 0, day)
		testkit.RequireNoError(t, dispatcher.Tick(ctx, at))
		if len(dispatchEvents(t, db)) != day+1 {
			t.Fatal("COUNT was reset on subsequent tick")
		}
	}
	current, err := NewRepository(db).Get(ctx, "owner", plan.ID)
	testkit.RequireNoError(t, err)
	if current.Enabled || current.NextAt != nil {
		t.Fatal("exhausted COUNT remains scheduled")
	}
	events := dispatchEvents(t, db)
	if events[1].OccurrenceAt.Sub(events[0].OccurrenceAt) != 23*time.Hour || events[1].OccurrenceAt.In(location).Hour() != 9 {
		t.Fatalf("local-time recurrence drifted at DST: %+v", events)
	}
}
func TestTDDDispatcherConcurrentTicksAndInvalidRule(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	db, _ := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: Coalesce}, AssistantPlan)
	ctx := testkit.Context(t)
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- NewDispatcher(db).Tick(ctx, start) }()
	}
	for range 2 {
		select {
		case err := <-results:
			testkit.RequireNoError(t, err)
		case <-ctx.Done():
			t.Fatal("concurrent dispatch did not finish")
		}
	}
	if len(dispatchEvents(t, db)) != 1 || fixtureCount(t, db, "trigger.occurrences") != 1 {
		t.Fatal("concurrent ticks duplicated occurrence")
	}
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, CheckAssistant: fixtureCheckAssistant(db)})
	_, err := service.Create(ctx, owner(), CreateRequest{Mutation: mutation("bad-rule", 0), Kind: AssistantPlan, AssistantID: "assistant", Title: "Invalid rule", Trigger: Trigger{StartAt: start, Timezone: "UTC", Recurrence: "not-a-rule", MissedPolicy: Skip}})
	wantCode(t, err, core.InvalidArgument)
}
func TestTDDDispatcherResumesPersistedCursorWithoutReplayingLifetime(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", Recurrence: "FREQ=MINUTELY", MissedPolicy: Coalesce}, AssistantPlan)
	ctx := testkit.Context(t)
	next := start.Add(200000 * time.Minute)
	plan.NextAt = &next
	plan.Version = 2
	testkit.RequireNoError(t, NewRepository(db).Save(ctx, plan, 1))
	fixturePut(t, db, "business.schedule-progress", "owner/plan", dispatchProgress{Version: 2, Consumed: 200000})
	testkit.RequireNoError(t, NewDispatcher(db).Tick(ctx, next))
	if len(dispatchEvents(t, db)) != 1 {
		t.Fatal("persisted cursor was ignored")
	}
}

func TestDispatcherRecognizesLegacyOccurrenceLedger(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	db, plan := dispatchFixture(t, Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: Coalesce}, AssistantPlan)
	legacy := Occurrence{ScheduleID: plan.ID, ScheduleVersion: plan.Version, DueAt: start}
	fixturePut(t, db, "business.occurrences", "owner/plan/1/"+start.Format(time.RFC3339Nano), legacy)
	if err := NewDispatcher(db).Tick(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	if len(dispatchEvents(t, db)) != 0 || fixtureCount(t, db, "trigger.occurrences") != 0 {
		t.Fatal("upgrade replayed a historical occurrence")
	}
	current, err := NewRepository(db).Get(t.Context(), "owner", plan.ID)
	if err != nil || current.NextAt != nil || current.Enabled {
		t.Fatal(current, err)
	}
}
