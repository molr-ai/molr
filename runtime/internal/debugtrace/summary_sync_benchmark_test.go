package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func summaryHistory(t testing.TB, count int) (*Service, core.Principal) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	s := New(db, Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < count; i++ {
			id := core.ID(fmt.Sprintf("run-%06d", i))
			run := Run{ID: id, OwnerID: p.OwnerID, AssistantID: "assistant", ExecutionID: id, EventID: id, Trigger: "user_message", State: "succeeded", StartedAt: now.Add(time.Duration(i) * time.Second), Source: &SourceContent{Kind: "user_message", Status: "available", Text: strings.Repeat("synthetic trigger ", 32)}, Steps: []Step{{Sequence: 1, Revision: uint64(i + 1), Kind: "run.start", At: now.Add(time.Duration(i) * time.Second)}}}
			if err := saveRun(ctx, tx, run); err != nil {
				return err
			}
			if err := s.bump(ctx, tx, p.OwnerID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestTrajectoryIdleSyncDoesNotReadRetainedHistory(t *testing.T) {
	s, p := summaryHistory(t, 1000)
	for _, assistant := range []core.ID{"", "assistant"} {
		first, err := s.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: assistant}})
		if err != nil {
			t.Fatal(err)
		}
		reads := &retentionReads{}
		observed := New(retentionStore{s.store, reads}, s.options)
		page, err := observed.Trajectory(t.Context(), p, TrajectoryQuery{Query: Query{AssistantID: assistant}, After: first.HeadCursor, RunsCursor: first.RunsCursor})
		if err != nil || page.RunsReset || len(page.Items) != 0 || len(page.Runs) != 0 || reads.rows != 0 {
			t.Fatalf("idle loaded history: rows=%d page=%+v err=%v", reads.rows, page, err)
		}
		encoded, _ := json.Marshal(page)
		if len(encoded) > 1024 {
			t.Fatal("idle response carries history", len(encoded))
		}
	}
}

func BenchmarkTrajectoryIdleSummarySync(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, p := summaryHistory(b, count)
			first, err := s.Trajectory(b.Context(), p, TrajectoryQuery{Query: Query{AssistantID: "assistant"}})
			if err != nil {
				b.Fatal(err)
			}
			reads := &retentionReads{}
			s = New(retentionStore{s.store, reads}, s.options)
			query := TrajectoryQuery{Query: Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 100}}, After: first.HeadCursor, RunsCursor: first.RunsCursor}
			b.ReportAllocs()
			b.ResetTimer()
			var page TrajectoryPage
			for b.Loop() {
				page, err = s.Trajectory(b.Context(), p, query)
				if err != nil || len(page.Items) != 0 || len(page.Runs) != 0 {
					b.Fatal(page, err)
				}
			}
			b.StopTimer()
			encoded, _ := json.Marshal(page)
			b.ReportMetric(float64(reads.rows)/float64(b.N), "summary-rows/op")
			b.ReportMetric(float64(len(encoded)), "json-bytes/op")
		})
	}
}
