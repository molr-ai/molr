package plan

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/core"
)

func TestEventSelectionMatchesFullSortAndStaysBounded(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	input := make([]eventCandidate, 500)
	for i := range input {
		input[i] = eventCandidate{at: at.Add(time.Duration(i%13) * time.Hour), ref: fmt.Sprintf("schedule/%04d", i)}
	}
	expected := append([]eventCandidate(nil), input...)
	sort.Slice(expected, func(i, j int) bool {
		a, b := expected[i], expected[j]
		if a.at.Equal(b.at) {
			return a.ref < b.ref
		}
		return a.at.Before(b.at)
	})
	for _, limit := range []int{1, 2, 51, 201} {
		for seed := int64(0); seed < 5; seed++ {
			shuffled := append([]eventCandidate(nil), input...)
			rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			selected := make(eventSelection, 0, limit)
			for _, candidate := range shuffled {
				selected.offer(candidate, limit)
				if len(selected) > limit {
					t.Fatal("selection exceeded page bound")
				}
			}
			sort.Slice(selected, func(i, j int) bool {
				a, b := selected[i], selected[j]
				if a.at.Equal(b.at) {
					return a.ref < b.ref
				}
				return a.at.Before(b.at)
			})
			if !reflect.DeepEqual([]eventCandidate(selected), expected[:limit]) {
				t.Fatalf("limit=%d seed=%d differs from full sort", limit, seed)
			}
		}
	}
}

func TestEventsSmallPagesMatchFullChronologicalProjection(t *testing.T) {
	coordinator, db := fixture(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	type expectedEvent struct {
		at  time.Time
		ref string
	}
	expected := []expectedEvent{}
	for i := 0; i < 12; i++ {
		id := core.ID(fmt.Sprintf("plan-%02d", i))
		start := at.Add(time.Duration((11-i)%3) * time.Hour)
		kind := schedule.Personal
		if i%2 != 0 {
			kind = schedule.AssistantPlan
		}
		item := schedule.Schedule{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: kind, Title: string(id), Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: start, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=7", MissedPolicy: schedule.Coalesce}}
		if err := schedule.NewRepository(db).Insert(t.Context(), item); err != nil {
			t.Fatal(err)
		}
		for day := 0; day < 7; day++ {
			expected = append(expected, expectedEvent{start.AddDate(0, 0, day), "schedule/" + string(id)})
		}
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].at.Equal(expected[j].at) {
			return expected[i].ref < expected[j].ref
		}
		return expected[i].at.Before(expected[j].at)
	})
	for _, limit := range []int{1, 7, 50} {
		q := EventQuery{From: at, Page: core.PageRequest{Limit: limit}}
		offset := 0
		for pageNumber := 0; pageNumber <= len(expected); pageNumber++ {
			page, err := coordinator.Events(t.Context(), owner(), q)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) > limit {
				t.Fatal("page exceeds requested size")
			}
			for _, event := range page.Items {
				if offset >= len(expected) {
					t.Fatal("extra occurrence")
				}
				want := expected[offset]
				if !event.At.Equal(want.at) || event.Key != want.ref+"@"+want.at.Format(time.RFC3339Nano) {
					t.Fatalf("limit=%d offset=%d: %+v, want %+v", limit, offset, event, want)
				}
				offset++
			}
			if page.NextCursor == "" {
				break
			}
			if len(page.Items) == 0 {
				t.Fatal("empty page with cursor")
			}
			q.Page.Cursor = page.NextCursor
		}
		if offset != len(expected) {
			t.Fatalf("limit=%d returned %d of %d occurrences", limit, offset, len(expected))
		}
	}
}

func TestEventsCompletedHistorySelectionIgnoresExecutionOrder(t *testing.T) {
	coordinator, db := fixture(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	ref := core.Object{Kind: "schedule", ID: "daily"}
	item := schedule.Schedule{ID: ref.ID, OwnerID: "owner", Kind: schedule.Personal, Title: "daily", Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=8", MissedPolicy: schedule.Coalesce}}
	if err := schedule.NewRepository(db).Insert(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 8; day++ {
		when := at.AddDate(0, 0, day)
		current, err := coordinator.Read(t.Context(), owner(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := coordinator.Execute(t.Context(), owner(), ExecuteRequest{Mutation: mutation(fmt.Sprintf("complete-%d", day), current.Links.Version), Ref: ref, Action: "complete", OccurrenceAt: &when}); err != nil {
			t.Fatal(err)
		}
	}
	q := EventQuery{From: at, State: "done", Page: core.PageRequest{Limit: 2}}
	offset := 0
	for {
		page, err := coordinator.Events(t.Context(), owner(), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Items {
			if !event.At.Equal(at.AddDate(0, 0, offset)) {
				t.Fatalf("completion order changed page: offset=%d at=%s", offset, event.At)
			}
			offset++
		}
		if page.NextCursor == "" {
			break
		}
		q.Page.Cursor = page.NextCursor
	}
	if offset != 8 {
		t.Fatalf("returned %d completed occurrences, want 8", offset)
	}
}
