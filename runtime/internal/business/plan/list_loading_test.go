package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type listReadCounter struct {
	persistence.Store
	gets, queries int
	rows          map[string]int
	failBucket    string
}
type listCountingTx struct {
	persistence.Tx
	counter *listReadCounter
}

func (s *listReadCounter) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, listCountingTx{tx, s}) })
}
func (tx listCountingTx) Get(ctx context.Context, bucket, key string, out any) error {
	tx.counter.gets++
	return tx.Tx.Get(ctx, bucket, key, out)
}
func (tx listCountingTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	tx.counter.queries++
	if bucket == tx.counter.failBucket {
		return nil, core.NewError(core.Unavailable, "fixture read failure")
	}
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if tx.counter.rows == nil {
		tx.counter.rows = map[string]int{}
	}
	tx.counter.rows[bucket] += len(rows)
	return rows, err
}
func seedListSources(ctx context.Context, store persistence.Store, count int) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < count; i++ {
			id := core.ID(fmt.Sprintf("plan-%04d", i))
			var bucket string
			var item any
			if i%2 == 0 {
				bucket = "business.todos"
				item = todo.Todo{ID: id, OwnerID: "owner", Title: string(id), Version: 1}
			} else {
				bucket = "business.schedules"
				item = schedule.Schedule{ID: id, OwnerID: "owner", Kind: schedule.Personal, Title: string(id), Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), Timezone: "UTC", Recurrence: "FREQ=DAILY", MissedPolicy: schedule.Coalesce}}
			}
			if err := tx.Put(ctx, bucket, persistence.Key("owner", id), item); err != nil {
				return err
			}
		}
		return nil
	})
}
func TestListLoadsPlanSourcesWithoutPerPlanReads(t *testing.T) {
	_, db := fixture(t)
	if err := seedListSources(t.Context(), db, 60); err != nil {
		t.Fatal(err)
	}
	counted := &listReadCounter{Store: db}
	coordinator := New(counted, allowOwner{}, nil)
	plans, err := coordinator.List(t.Context(), owner(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 60 {
		t.Fatalf("plans = %d", len(plans))
	}
	if counted.gets+counted.queries > 8 {
		t.Fatalf("60 plans issued %d point reads and %d queries; expected bounded batch reads", counted.gets, counted.queries)
	}
}
func BenchmarkListManyPlans(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("plans-%d", count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "plans.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if err := seedListSources(b.Context(), db, count); err != nil {
				b.Fatal(err)
			}
			counted := &listReadCounter{Store: db}
			coordinator := New(counted, allowOwner{}, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				plans, err := coordinator.List(b.Context(), owner(), time.Time{}, time.Time{})
				if err != nil || len(plans) != count {
					b.Fatalf("plans=%d err=%v", len(plans), err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(counted.gets+counted.queries)/float64(b.N), "reads/op")
		})
	}
}

func TestBatchListMatchesIndividualReadsWithHiddenDependencies(t *testing.T) {
	coordinator, db := fixture(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	dep := core.Object{Kind: "todo", ID: "dependency"}
	mixed := core.Object{Kind: "todo", ID: "shared"}
	recurring := core.Object{Kind: "schedule", ID: "shared"}
	missing := core.Object{Kind: "todo", ID: "deleted"}
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, v := range []todo.Todo{
			{ID: dep.ID, OwnerID: "owner", AssistantID: "other-assistant", Title: "Hidden dependency", Version: 1},
			{ID: mixed.ID, OwnerID: "owner", AssistantID: "assistant", Title: "Visible work", Version: 1},
			{ID: "missing", OwnerID: "owner", AssistantID: "assistant", Title: "Missing dependency", Version: 1},
			{ID: "done", OwnerID: "owner", Completed: true, Title: "Personal done", Version: 1},
			{ID: "paused", OwnerID: "owner", AssistantID: "assistant", Title: "Paused work", Version: 1},
			{ID: "unrelated", OwnerID: "owner", AssistantID: "other-assistant", Title: "Unrelated", Version: 1},
			{ID: mixed.ID, OwnerID: "foreign", Title: "Foreign", Version: 1},
		} {
			if err := tx.Put(ctx, "business.todos", persistence.Key(v.OwnerID, v.ID), v); err != nil {
				return err
			}
		}
		schedule := schedule.Schedule{ID: recurring.ID, OwnerID: "owner", AssistantID: "assistant", Kind: schedule.Personal, Title: "Recurring", Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: schedule.Coalesce}}
		if err := tx.Put(ctx, "business.schedules", persistence.Key("owner", schedule.ID), schedule); err != nil {
			return err
		}
		for _, links := range []Links{
			{Ref: dep, OwnerID: "owner", Handoff: "notify", Runs: []Execution{{ID: "local-complete", State: "completed", StartedAt: at.Add(time.Hour)}}},
			{Ref: mixed, OwnerID: "owner", Handoff: "automatic", Dependencies: []core.Object{dep}, Runs: []Execution{{ID: "inline", State: "completed", StartedAt: at}}},
			{Ref: core.Object{Kind: "todo", ID: "missing"}, OwnerID: "owner", Dependencies: []core.Object{missing}},
			{Ref: core.Object{Kind: "todo", ID: "paused"}, OwnerID: "owner", Paused: true},
		} {
			if err := tx.Put(ctx, bucket, key(links.OwnerID, links.Ref), links); err != nil {
				return err
			}
		}
		for _, run := range []task.Task{
			{ID: "a-old", OwnerID: "owner", Plan: dep, State: task.Failed, CreatedAt: at},
			{ID: "b-new", OwnerID: "owner", Plan: mixed, State: task.Completed, CreatedAt: at.Add(time.Hour), UpdatedAt: at.Add(2 * time.Hour)},
			{ID: "c-tie", OwnerID: "owner", Plan: mixed, State: task.Failed, CreatedAt: at.Add(time.Hour), UpdatedAt: at.Add(2 * time.Hour)},
			{ID: "d-recurring", OwnerID: "owner", Plan: recurring, State: task.Completed, CreatedAt: at, OccurrenceAt: &at},
			{ID: "ignored-deleted", OwnerID: "owner", Plan: missing, State: task.Completed, CreatedAt: at},
			{ID: "ignored-unrelated", OwnerID: "owner", Plan: core.Object{Kind: "todo", ID: "unrelated"}, State: task.Completed, CreatedAt: at},
			{ID: "ignored-foreign", OwnerID: "foreign", Plan: mixed, State: task.Completed, CreatedAt: at.Add(24 * time.Hour)},
		} {
			if err := tx.Put(ctx, "business.tasks", persistence.Key(run.OwnerID, run.ID), run); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	principals := []core.Principal{owner(), {OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}}
	for _, principal := range principals {
		for _, calendar := range []bool{false, true} {
			from, to := time.Time{}, time.Time{}
			if calendar {
				from = at
				to = at.AddDate(0, 0, 4)
			}
			observed := &listReadCounter{Store: db}
			coordinator.store = observed
			plans, err := coordinator.List(t.Context(), principal, from, to)
			if err != nil {
				t.Fatal(err)
			}
			rows := observed.rows["business.tasks"]
			if principal.Kind == core.ActorAssistant && rows != 4 {
				t.Fatalf("assistant loaded %d task rows; want only 4 visible/dependency rows", rows)
			}
			if principal.Kind == core.ActorAssistant && len(plans) != 4 {
				t.Fatalf("assistant saw %d plans", len(plans))
			}
			if principal.Kind == core.ActorOwner && len(plans) != 7 {
				t.Fatalf("owner saw %d plans", len(plans))
			}
			for _, plan := range plans {
				expected, err := coordinator.Read(t.Context(), owner(), plan.Ref)
				if err != nil {
					t.Fatal(err)
				}
				if calendar && expected.Schedule != nil {
					expected.Occurrences, err = schedule.CalendarTimes(t.Context(), *expected.Schedule, from, to)
					if err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(plan, expected) {
					t.Fatalf("batch differs from individual read for %v:\n%+v\n%+v", plan.Ref, plan, expected)
				}
				if plan.Ref == mixed && !plan.Ready {
					t.Fatal("hidden dependency completion was lost")
				}
				if plan.Ref.ID == "missing" && (plan.Ready || !reflect.DeepEqual(plan.BlockedBy, []core.Object{missing})) {
					t.Fatal("missing dependency became ready")
				}
				if plan.Ref.ID == "paused" && plan.Ready {
					t.Fatal("paused plan became ready")
				}
			}
		}
	}
}

func TestBatchListCancellationAndReadErrors(t *testing.T) {
	coordinator, db := fixture(t)
	if err := seedListSources(t.Context(), db, 600); err != nil {
		t.Fatal(err)
	}
	counted := &listReadCounter{Store: db}
	coordinator.store = counted
	rows, err := coordinator.List(t.Context(), owner(), time.Time{}, time.Time{})
	if err != nil || len(rows) != 600 {
		t.Fatalf("batch boundary: %d %v", len(rows), err)
	}
	if counted.gets != 0 || counted.queries > 8 {
		t.Fatalf("unbounded hydration: gets=%d queries=%d", counted.gets, counted.queries)
	}
	for _, failed := range []string{bucket, "business.tasks"} {
		counted.failBucket = failed
		rows, err := coordinator.List(t.Context(), owner(), time.Time{}, time.Time{})
		if core.CodeOf(err) != core.Unavailable || len(rows) != 0 {
			t.Fatalf("read failure not propagated: %d %v", len(rows), err)
		}
	}
	counted.failBucket = ""
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := coordinator.List(ctx, owner(), time.Time{}, time.Time{}); core.CodeOf(err) != core.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	at := time.Now()
	if _, err := coordinator.List(t.Context(), owner(), at, at.Add(126*24*time.Hour)); core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("range validation: %v", err)
	}
}
