package dream

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type historyCountingStore struct {
	persistence.Store
	runRows, sessionRows int
}
type historyCountingTx struct {
	persistence.Tx
	counts *historyCountingStore
}

func (s *historyCountingStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &historyCountingTx{tx, s}) })
}
func (tx *historyCountingTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == sessions {
		tx.counts.sessionRows++
	}
	return tx.Tx.Get(ctx, bucket, key, out)
}
func (tx *historyCountingTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == runs {
		tx.counts.runRows += len(rows)
	}
	if bucket == sessions {
		tx.counts.sessionRows += len(rows)
	}
	return rows, err
}
func TestRunHistoryLoadsOnlyVisibleRunsAndDistinctSessions(t *testing.T) {
	f := setup(t)
	err := f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, sessions, "owner/history-session", Session{ID: "history-session", OwnerID: "owner", AssistantID: "assistant", State: "completed"}); err != nil {
			return err
		}
		for i := 0; i < 1000; i++ {
			id := core.ID(fmt.Sprintf("history-%04d", i))
			r := Run{ID: id, OwnerID: "owner", AssistantID: "assistant", SessionID: "history-session", State: "completed", CreatedAt: f.now.Add(time.Duration(i) * time.Second), Summary: string(id), Output: strings.Repeat("x", 4096)}
			if err := tx.Put(ctx, runs, persistence.Key("owner", id), r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	counts := &historyCountingStore{Store: f.db}
	f.s.store = counts
	rows, err := f.s.Runs(t.Context(), f.p, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 60 {
		t.Fatalf("history count=%d, want 60", len(rows))
	}
	for i, row := range rows {
		if row.ID != core.ID(fmt.Sprintf("history-%04d", 999-i)) || row.Output != "" || row.SessionState != "completed" {
			t.Fatalf("unexpected history row %d: %s", i, row.ID)
		}
	}
	t.Logf("returned=%d loaded runs=%d loaded sessions=%d", len(rows), counts.runRows, counts.sessionRows)
	if counts.runRows > 60 {
		t.Errorf("loaded %d runs for a 60-row history", counts.runRows)
	}
	if counts.sessionRows > 1 {
		t.Errorf("loaded shared session %d times, want at most one", counts.sessionRows)
	}
}

func TestRunHistoryOrdersExactInstantsAndKeepsScopes(t *testing.T) {
	f := setup(t)
	err := f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 75; i++ {
			at := f.now.Add(time.Duration(i) * time.Nanosecond)
			if i%2 == 0 {
				at = at.In(time.FixedZone("offset", 8*3600))
			}
			id := core.ID(fmt.Sprintf("run-%03d", i))
			r := Run{ID: id, OwnerID: "owner", AssistantID: "assistant", CreatedAt: at, Output: "private output", Sources: []Evidence{{Text: "private evidence", Kind: "message", Version: "v1"}}, Manual: true, Backfill: true, ReviewRetained: true}
			if i == 0 {
				r.SessionID = "missing-old-session"
			}
			if err := tx.Put(ctx, runs, persistence.Key("owner", id), r); err != nil {
				return err
			}
		}
		for _, r := range []Run{
			{ID: "other-account", OwnerID: "other", AssistantID: "assistant", CreatedAt: f.now.Add(time.Hour)},
			{ID: "other-assistant", OwnerID: "owner", AssistantID: "other", CreatedAt: f.now.Add(time.Hour)},
		} {
			if err := tx.Put(ctx, runs, persistence.Key(r.OwnerID, r.ID), r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.Runs(t.Context(), f.p, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 60 {
		t.Fatal(len(rows))
	}
	for i, row := range rows {
		if row.ID != core.ID(fmt.Sprintf("run-%03d", 74-i)) {
			t.Fatalf("position %d: %s", i, row.ID)
		}
		if row.Output != "" || len(row.Sources) != 1 || row.Sources[0].Text != "" || row.Sources[0].Version != "v1" || !row.Manual || !row.Backfill || !row.ReviewRetained {
			t.Fatalf("response fields changed: %+v", row)
		}
	}
	stored, err := persistence.Load[Run](t.Context(), f.db, runs, "owner/run-074")
	if err != nil || stored.Output != "private output" || stored.Sources[0].Text != "private evidence" {
		t.Fatal("history modified stored evidence", err)
	}
	empty, err := f.s.Runs(t.Context(), f.p, "empty")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty history", empty, err)
	}
}

func TestRunHistorySessionStateIsFreshAndMissingSessionFails(t *testing.T) {
	f := setup(t)
	put(t, f.db, sessions, "owner/shared", Session{ID: "shared", State: "active"})
	for _, id := range []core.ID{"a", "b"} {
		put(t, f.db, runs, persistence.Key("owner", id), Run{ID: id, OwnerID: "owner", AssistantID: "assistant", SessionID: "shared", CreatedAt: f.now})
	}
	for _, state := range []string{"active", "completed", ""} {
		put(t, f.db, sessions, "owner/shared", Session{ID: "shared", State: state})
		counts := &historyCountingStore{Store: f.db}
		f.s.store = counts
		rows, err := f.s.Runs(t.Context(), f.p, "assistant")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].ID != "b" || rows[1].ID != "a" {
			t.Fatalf("unstable same-instant order: %+v", rows)
		}
		for _, row := range rows {
			if row.SessionState != state {
				t.Fatalf("stale session state: %q, want %q", row.SessionState, state)
			}
		}
		if counts.sessionRows != 1 {
			t.Fatalf("session loaded %d times", counts.sessionRows)
		}
	}
	put(t, f.db, runs, "owner/missing", Run{ID: "missing", OwnerID: "owner", AssistantID: "assistant", SessionID: "missing", CreatedAt: f.now.Add(time.Second)})
	if rows, err := f.s.Runs(t.Context(), f.p, "assistant"); core.CodeOf(err) != core.NotFound || rows != nil {
		t.Fatal("missing visible session should fail", rows, err)
	}
}

func TestRunHistoryAuthorizationPrecedesReads(t *testing.T) {
	f := setup(t)
	counts := &historyCountingStore{Store: f.db}
	f.s.store = counts
	denied := f.ap
	denied.SubjectID = "other-assistant"
	if rows, err := f.s.Runs(t.Context(), denied, "assistant"); core.CodeOf(err) != core.Forbidden || rows != nil {
		t.Fatal("authorization", rows, err)
	}
	if counts.runRows != 0 || counts.sessionRows != 0 {
		t.Fatal("read history before authorization")
	}
}
