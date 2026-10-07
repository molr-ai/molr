package trigger

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestRuleValidationAndFirst(t *testing.T) {
	start := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	r := Rule{StartAt: start, Timezone: "Asia/Shanghai", Recurrence: "FREQ=DAILY;BYHOUR=13;BYMINUTE=30;BYSECOND=0", MissedPolicy: Coalesce}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := First(r)
	if err != nil || first == nil || first.In(time.FixedZone("CST", 8*3600)).Hour() != 13 {
		t.Fatal(first, err)
	}
	for _, change := range []func(*Rule){func(r *Rule) { r.Timezone = "invalid" }, func(r *Rule) { r.MissedPolicy = CatchUp; r.MaxCatchUp = 0 }, func(r *Rule) { r.MaxCatchUp = 1 }, func(r *Rule) { r.StartAt = time.Time{} }} {
		bad := r
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid rule accepted", bad)
		}
	}
	for _, text := range []string{"bad", "FREQ=DAILY;COUNT=0", "FREQ=DAILY;COUNT=2;UNTIL=20271001T000000Z", "FREQ=DAILY\nDTSTART:20260929T000000Z"} {
		bad := r
		bad.Recurrence = text
		if _, err := First(bad); err == nil {
			t.Fatal("invalid recurrence accepted", text)
		}
	}
}
func TestDueMissedPoliciesAndCursor(t *testing.T) {
	start := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	now := start.Add(3*time.Hour + 2*time.Minute)
	for _, tc := range []struct {
		policy MissedPolicy
		limit  int
		hours  []int
	}{{Skip, 0, nil}, {Coalesce, 0, []int{3}}, {CatchUp, 2, []int{2, 3}}} {
		t.Run(string(tc.policy), func(t *testing.T) {
			r := Rule{StartAt: start, Timezone: "UTC", Recurrence: "FREQ=HOURLY;COUNT=5", MissedPolicy: tc.policy, MaxCatchUp: tc.limit}
			b, err := Due(t.Context(), r, Cursor{NextAt: &start}, now)
			if err != nil || len(b.Occurrences) != len(tc.hours) || b.Cursor.Consumed != 4 {
				t.Fatal(b, err)
			}
			for i, h := range tc.hours {
				if !b.Occurrences[i].Equal(start.Add(time.Duration(h) * time.Hour)) {
					t.Fatal(b)
				}
			}
			again, err := Due(t.Context(), r, b.Cursor, now)
			if err != nil || len(again.Occurrences) != 0 || again.Cursor.Consumed != 4 {
				t.Fatal("cursor replay", again, err)
			}
			last, err := Due(t.Context(), r, again.Cursor, start.Add(4*time.Hour))
			if err != nil || len(last.Occurrences) != 1 || last.Cursor.NextAt != nil || last.Cursor.Consumed != 5 {
				t.Fatal("count reset", last, err)
			}
		})
	}
	once := Rule{StartAt: start, Timezone: "UTC", MissedPolicy: Coalesce}
	b, err := Due(t.Context(), once, Cursor{NextAt: &start}, now)
	if err != nil || len(b.Occurrences) != 1 || b.Cursor.NextAt != nil {
		t.Fatal(b, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Due(ctx, once, Cursor{NextAt: &start}, now); core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}
func TestDueAndCalendarAgreeAcrossDSTAndRestart(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []time.Time{time.Date(2026, 3, 7, 9, 0, 0, 0, loc), time.Date(2026, 10, 31, 9, 0, 0, 0, loc)} {
		r := Rule{StartAt: start, Timezone: loc.String(), Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: Coalesce}
		first, err := First(r)
		if err != nil {
			t.Fatal(err)
		}
		calendar, err := Between(t.Context(), r, start.Add(-time.Hour), start.AddDate(0, 0, 4), 0)
		if err != nil || len(calendar) != 3 {
			t.Fatal(calendar, err)
		}
		cursor := Cursor{NextAt: first}
		for _, at := range calendar {
			b, err := Due(t.Context(), r, cursor, at)
			if err != nil || len(b.Occurrences) != 1 || b.Occurrences[0].In(loc).Hour() != 9 {
				t.Fatal(b, err)
			}
			cursor = b.Cursor
		}
		if cursor.NextAt != nil {
			t.Fatal("COUNT not exhausted")
		}
		duration := calendar[1].Sub(calendar[0])
		if duration != 23*time.Hour && duration != 25*time.Hour {
			t.Fatal("DST wall-time drift", duration)
		}
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	next := start.Add(200000 * time.Minute)
	r := Rule{StartAt: start, Timezone: "UTC", Recurrence: "FREQ=MINUTELY", MissedPolicy: Coalesce}
	b, err := Due(t.Context(), r, Cursor{NextAt: &next, Consumed: 200000, Resumed: true}, next)
	if err != nil || len(b.Occurrences) != 1 || b.Cursor.Consumed != 200001 {
		t.Fatal("replayed lifetime", b, err)
	}
}
func TestOccurrenceNamespacesAtomicRollbackAndConcurrentDedup(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "timer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)
	v := Occurrence{OwnerID: "owner", Kind: "dream", ID: "same", Revision: 1, At: at}
	db.FailNextCommit(errors.New("interrupted"))
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if _, err := RecordOccurrence(ctx, db, v); err != nil {
			return err
		}
		return tx.Put(ctx, "effects", "owner/same", true)
	})
	if err == nil {
		t.Fatal("failed commit passed")
	}
	rows, _ := persistence.Scan[Occurrence](t.Context(), db, "trigger.occurrences")
	if len(rows) != 0 {
		t.Fatal("partial occurrence")
	}
	if _, err := persistence.Load[bool](t.Context(), db, "effects", "owner/same"); !persistence.Missing(err) {
		t.Fatal("partial effect", err)
	}
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() { inserted, err := RecordOccurrence(t.Context(), db, v); results <- inserted; errs <- err }()
	}
	count := 0
	for range 2 {
		if <-results {
			count++
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("duplicate occurrence", count)
	}
	shifted := v
	shifted.At = at.In(time.FixedZone("CST", 8*3600))
	if ok, err := RecordOccurrence(t.Context(), db, shifted); err != nil || ok {
		t.Fatal("offset duplicated instant", err)
	}
	for _, change := range []func(*Occurrence){func(v *Occurrence) { v.Kind = "schedule" }, func(v *Occurrence) { v.Revision = 2 }, func(v *Occurrence) { v.OwnerID = "other" }} {
		copy := v
		change(&copy)
		if ok, err := RecordOccurrence(t.Context(), db, copy); err != nil || !ok {
			t.Fatal("namespace/revision collision", copy, err)
		}
	}
}

func TestVisitUpcomingPreservesCountAndLocalDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, loc)
	r := Rule{StartAt: at, Timezone: loc.String(), Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: Coalesce}
	var result []time.Time
	err = VisitUpcoming(t.Context(), r, at.Add(time.Hour), func(at time.Time) bool { result = append(result, at); return true })
	if err != nil || len(result) != 2 {
		t.Fatal(result, err)
	}
	for _, v := range result {
		if v.In(loc).Hour() != 9 {
			t.Fatal("DST wall clock drift", v)
		}
	}
	result = nil
	r.Recurrence = "FREQ=DAILY"
	if err := VisitUpcoming(t.Context(), r, at, func(at time.Time) bool { result = append(result, at); return false }); err != nil || len(result) != 1 {
		t.Fatal("visitor not bounded", err)
	}
}
