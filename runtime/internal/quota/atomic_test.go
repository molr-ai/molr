package quota

import (
	"context"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func TestNetworkAtomicFailureRestartAndNestedRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	l := &Ledger{Store: db, Now: func() time.Time { return now }}
	spec := core.NetworkConstraints{Bytes: 100, Connections: 2, Period: "day", Timezone: "UTC"}
	accounts := []AccountLimit{{"owner/assistant/source", spec}, {"owner/group/local", spec}}
	db.FailNextCommit(errors.New("disk full"))
	if n, err := l.ReserveMany(t.Context(), accounts, "failed", 80); err == nil || n != 0 {
		t.Fatal(n, err)
	}
	for _, a := range accounts {
		u, err := l.Usage(t.Context(), a.Key, spec)
		if err != nil || u.ReservedBytes != 0 {
			t.Fatal(u, err)
		}
	}
	if _, err := l.ReserveMany(t.Context(), accounts, "held", 80); err != nil {
		t.Fatal(err)
	}
	keys := []string{accounts[0].Key, accounts[1].Key}
	if err := l.SettleMany(t.Context(), []string{keys[0], "missing"}, "held", 30); err == nil {
		t.Fatal("partial settlement accepted")
	}
	db.FailNextCommit(errors.New("disk full"))
	if err := l.SettleMany(t.Context(), keys, "held", 30); err == nil {
		t.Fatal("commit failure ignored")
	}
	for _, a := range accounts {
		u, err := l.Usage(t.Context(), a.Key, spec)
		if err != nil || u.ReservedBytes != 80 || u.UsedBytes != 0 {
			t.Fatal(u, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l.Store = db
	now = now.Add(24 * time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		u, err := l.Usage(ctx, keys[0], spec)
		if err == nil && (u.ReservedBytes != 80 || u.UsedBytes != 0) {
			t.Errorf("lost carry: %+v", u)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := l.SettleMany(t.Context(), keys, "held", 30); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		u, err := l.Usage(t.Context(), key, spec)
		if err != nil || u.ReservedBytes != 0 {
			t.Fatal(u, err)
		}
	}
}
func TestCallUsagePendingCarryAndProjection(t *testing.T) {
	store := openLedgerStore(t, t.TempDir())
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		ID, OwnerID, Action, State string
		CreatedAt                  time.Time
		Arguments                  string
	}{
		{"old", "owner", "read", "unknown", now.Add(-24 * time.Hour), "private"},
		{"old-running", "owner", "read", "running", now.Add(-24 * time.Hour), "private"},
		{"uncertain", "owner", "read", "unknown", now.Add(time.Hour), "private"},
		{"current", "owner", "read", "succeeded", now.Add(time.Hour), "private"},
		{"live", "owner", "read", "running", now.Add(time.Hour), "private"},
		{"prepared", "owner", "read", "prepared", now.Add(time.Hour), "private"},
		{"other", "other", "read", "running", now.Add(time.Hour), "private"},
		{"finished", "owner", "read", "succeeded", now.Add(-24 * time.Hour), "private"},
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range rows {
			if err := tx.Put(ctx, "agent.operations", r.OwnerID+"/"+r.ID, r); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	used, err := CallTotals(t.Context(), store, "tool", "owner", "read", "", now, now.Add(24*time.Hour))
	if err != nil || used.Calls != 4 || used.Active != 2 || used.Unknown != 1 || used.Carried != 1 {
		t.Fatal(used, err)
	}
	if CheckCalls(used, 4, 0) == nil {
		t.Fatal("report and allowance disagree")
	}
	all, err := CallTotals(t.Context(), store, "tool", "owner", "", "", time.Time{}, time.Time{})
	if err != nil || all.Calls != 6 || all.Unknown != 2 || all.Active != 2 {
		t.Fatal(all, err)
	}
}

func TestPeriodBoundaryKeepsExactTime(t *testing.T) {
	store := openLedgerStore(t, t.TempDir())
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	for i, at := range []time.Time{start.Add(-time.Nanosecond), start, end.Add(-time.Nanosecond), end} {
		err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "agent.operations", fmt.Sprint("owner/", i), map[string]any{"ID": fmt.Sprint(i), "OwnerID": "owner", "Action": "read", "State": "succeeded", "CreatedAt": at})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	used, err := CallTotals(t.Context(), store, "tool", "owner", "read", "", start, end)
	if err != nil || used.Calls != 2 {
		t.Fatal(used, err)
	}
}
