package quota

import (
	"molr.ai/runtime/internal/core"
	"sync"
	"testing"
	"time"
)

func TestGroupTrafficBudgetSharedByAssistantsNotUsers(t *testing.T) {
	l := &Ledger{Store: openLedgerStore(t, t.TempDir()), Now: func() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }}
	spec := core.NetworkConstraints{Bytes: 100, Connections: 1, Period: "month", Timezone: "UTC"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var total int64
	for _, assistant := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			n, err := l.ReserveMany(t.Context(), []AccountLimit{{"user/" + id, spec}, {"user/group/local", spec}}, id, 70)
			if err != nil {
				t.Error(err)
				return
			}
			if n > 0 {
				if err := l.Settle(t.Context(), "user/"+id, id, n); err != nil {
					t.Error(err)
				}
				if err := l.Settle(t.Context(), "user/group/local", id, n); err != nil {
					t.Error(err)
				}
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}(assistant)
	}
	wg.Wait()
	if total != 100 {
		t.Fatalf("account total=%d", total)
	}
	n, err := l.ReserveMany(t.Context(), []AccountLimit{{"other/a", spec}, {"other/group/local", spec}}, "other", 70)
	if err != nil || n != 70 {
		t.Fatalf("other account affected: %d %v", n, err)
	}
	if !l.ClaimMany([]AccountLimit{{"user/a", spec}, {"user/group/local", spec}}, "one") {
		t.Fatal("first connection denied")
	}
	if l.ClaimMany([]AccountLimit{{"user/b", spec}, {"user/group/local", spec}}, "two") {
		t.Fatal("group connection limit bypassed")
	}
	v, err := l.Usage(t.Context(), "user/b", spec)
	if err != nil || v.Connections != 0 {
		t.Fatal("failed group admission leaked assistant connection")
	}
}

func TestGroupPeriodChangePreservesTrafficAndPendingReservations(t *testing.T) {
	l := &Ledger{Store: openLedgerStore(t, t.TempDir()), Now: func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }}
	spec := core.NetworkConstraints{Bytes: 100, Connections: 2, Period: "month", Timezone: "UTC"}
	key := "user/group/local"
	n, err := l.Reserve(t.Context(), key, "spent", spec, 60)
	if err != nil || n != 60 {
		t.Fatal(n, err)
	}
	if err := l.Settle(t.Context(), key, "spent", 60); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reserve(t.Context(), key, "pending", spec, 20); err != nil {
		t.Fatal(err)
	}
	spec.Period = "day"
	v, err := l.Usage(t.Context(), key, spec)
	if err != nil || v.UsedBytes != 60 || v.ReservedBytes != 20 {
		t.Fatal(v, err)
	}
	n, err = l.ReserveMany(t.Context(), []AccountLimit{{key, spec}}, "next", 70)
	if err != nil || n != 20 {
		t.Fatal(n, err)
	}
	if err := l.Settle(t.Context(), key, "pending", 20); err != nil {
		t.Fatal(err)
	}
	spec.Period = "month"
	v, err = l.Usage(t.Context(), key, spec)
	if err != nil || v.UsedBytes != 80 || v.ReservedBytes != 20 {
		t.Fatal(v, err)
	}
}
