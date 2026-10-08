package dream

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
	"testing"
	"time"
)

func TestExpiryUsesOnlyDueActiveSessionsAcrossBatches(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	now := f.now.Add(123456789 * time.Nanosecond)
	if err := f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1103; i++ {
			v := Session{ID: core.ID(fmt.Sprint("session-", i)), OwnerID: "owner", AssistantID: "assistant", State: "completed", Deadline: now.Add(-time.Hour), Timezone: "UTC", End: now}
			if i < 101 {
				v.State = "active"
				v.Deadline = now.In(time.FixedZone("offset", -7*3600))
				v.Started = i%2 == 0
			}
			if i == 101 {
				v.State = "active"
				v.Deadline = now.Add(time.Nanosecond)
			}
			if i == 102 {
				v.State = "active"
				v.Deadline = now.Add(time.Hour)
			}
			if err := saveSession(ctx, tx, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	observed := &testkit.ObservedStore{Store: f.db}
	f.s.store = observed
	// Expiry must still run inside the one-minute discovery throttle.
	f.s.lastTick = now.Add(-time.Second)
	if err := f.s.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	if observed.Scans != 0 || observed.Rows["business.dream_sessions_index"] != 101 {
		t.Fatal("history loaded", observed)
	}
	for i := 0; i < 103; i++ {
		v, err := persistence.Load[Session](ctx, f.db, sessions, persistence.Key("owner", core.ID(fmt.Sprint("session-", i))))
		if err != nil {
			t.Fatal(err)
		}
		want := "active"
		if i < 101 {
			want = "missed_window"
			if i%2 == 0 {
				want = "timed_out"
			}
		}
		if v.State != want {
			t.Fatal(i, v.State, want)
		}
	}
	observed.Rows = nil
	if err := f.s.expire(ctx, now); err != nil {
		t.Fatal(err)
	}
	if observed.Rows["business.dream_sessions_index"] != 0 {
		t.Fatal("closed sessions reread", observed)
	}
}
