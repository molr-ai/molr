package schedule

import (
	"context"
	"testing"
	"time"
)

func TestCalendarUsesRecurrenceTimezoneAndDoesNotAdvancePausedPlan(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 31, 9, 0, 0, 0, location)
	plan := Schedule{Kind: AssistantPlan, Enabled: false, Trigger: Trigger{StartAt: start, Timezone: "America/New_York", Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: Skip}, NextAt: &start}
	from := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * 24 * time.Hour)
	times, err := CalendarTimes(context.Background(), plan, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 3 {
		t.Fatalf("times: %v", times)
	}
	if times[0].UTC().Hour() != 13 || times[1].UTC().Hour() != 14 || times[2].UTC().Hour() != 14 {
		t.Fatalf("DST changed wall clock: %v", times)
	}
	if plan.Enabled || !plan.NextAt.Equal(start) {
		t.Fatal("calendar advanced plan")
	}
	times, err = CalendarTimes(context.Background(), plan, to, to.Add(24*time.Hour))
	if err != nil || len(times) != 0 {
		t.Fatalf("COUNT ignored: %v %v", times, err)
	}
	if _, err = CalendarTimes(context.Background(), plan, from, from.Add(126*24*time.Hour)); err == nil {
		t.Fatal("unbounded range")
	}
}
func TestPersonalEndValidation(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Hour)
	if validateEnd(Personal, start, &end) != nil || validateEnd(Personal, start, nil) != nil {
		t.Fatal("valid or legacy personal plan rejected")
	}
	if validateEnd(Personal, start, &start) == nil || validateEnd(AssistantPlan, start, &end) == nil {
		t.Fatal("invalid end accepted")
	}
}

func TestCalendarIncludesOverlappingTimeBlocksAndExcludesEndBoundary(t *testing.T) {
	start := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	plan := Schedule{Kind: Personal, EndAt: &end, Trigger: Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: Skip}}
	times, err := CalendarTimes(context.Background(), plan, from, from.Add(24*time.Hour))
	if err != nil || len(times) != 1 {
		t.Fatalf("overlap missing: %v %v", times, err)
	}
	times, err = CalendarTimes(context.Background(), plan, end, end.Add(time.Hour))
	if err != nil || len(times) != 0 {
		t.Fatalf("exclusive end included: %v %v", times, err)
	}
}
