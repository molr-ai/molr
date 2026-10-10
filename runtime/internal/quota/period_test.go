package quota

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func instant(t *testing.T, value string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return at
}
func TestPeriodCalendarBoundariesWithReusedTimezones(t *testing.T) {
	cases := []struct{ period, zone, now, start, end string }{
		{"daily", "America/New_York", "2026-03-08T12:00:00Z", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"day", "America/New_York", "2026-11-01T12:00:00Z", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"weekly", "America/New_York", "2026-03-08T12:00:00Z", "2026-03-02T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"week", "America/New_York", "2026-11-01T12:00:00Z", "2026-10-26T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"month", "Asia/Shanghai", "2026-03-01T01:00:00Z", "2026-02-28T16:00:00Z", "2026-03-31T16:00:00Z"},
		{"daily", "Asia/Shanghai", "2026-10-10T12:00:00Z", "2026-10-09T16:00:00Z", "2026-10-10T16:00:00Z"},
		{"daily", "Asia/Shanghai", "2026-10-11T12:00:00Z", "2026-10-10T16:00:00Z", "2026-10-11T16:00:00Z"},
		{"daily", "America/New_York", "2026-01-01T01:00:00Z", "2025-12-31T05:00:00Z", "2026-01-01T05:00:00Z"},
		{"daily", "Asia/Tokyo", "2026-01-01T01:00:00Z", "2025-12-31T15:00:00Z", "2026-01-01T15:00:00Z"},
		{"daily", "Invalid/Zone", "2026-01-01T01:00:00Z", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"},
	}
	for range 2 {
		for _, tc := range cases {
			start, end := Period(tc.period, tc.zone, instant(t, tc.now))
			if !start.Equal(instant(t, tc.start)) || !end.Equal(instant(t, tc.end)) {
				t.Fatalf("%s %s at %s: %v..%v", tc.period, tc.zone, tc.now, start, end)
			}
		}
	}
	for _, period := range []string{"", "lifetime"} {
		start, end := Period(period, "Invalid/Zone", time.Now())
		if !start.Equal(time.Unix(0, 0)) || !end.IsZero() {
			t.Fatal("lifetime boundary changed")
		}
	}
}

func TestTimezoneCacheConcurrentReuse(t *testing.T) {
	var cache timezoneCache
	const count = 32
	results := make(chan *time.Location, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() { loc, err := cache.load("America/New_York"); results <- loc; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *time.Location
	for location := range results {
		if first == nil {
			first = location
		}
		if first != location {
			t.Fatal("concurrent callers did not reuse location")
		}
	}
}

func TestTimezoneCacheIsBoundedAndDoesNotRetainErrors(t *testing.T) {
	var cache timezoneCache
	zones := strings.Fields("Africa/Abidjan Africa/Accra Africa/Addis_Ababa Africa/Algiers Africa/Cairo Africa/Casablanca Africa/Johannesburg America/Anchorage America/Argentina/Buenos_Aires America/Bogota America/Chicago America/Denver America/Detroit America/Halifax America/Los_Angeles America/Mexico_City America/New_York America/Phoenix America/Santiago America/Sao_Paulo America/Toronto America/Vancouver Asia/Bangkok Asia/Colombo Asia/Dhaka Asia/Dubai Asia/Hong_Kong Asia/Jakarta Asia/Jerusalem Asia/Karachi Asia/Kathmandu Asia/Kolkata Asia/Kuala_Lumpur Asia/Manila Asia/Seoul Asia/Shanghai Asia/Singapore Asia/Taipei Asia/Tehran Asia/Tokyo Australia/Adelaide Australia/Brisbane Australia/Darwin Australia/Hobart Australia/Melbourne Australia/Perth Australia/Sydney Europe/Amsterdam Europe/Athens Europe/Berlin Europe/Brussels Europe/Bucharest Europe/Budapest Europe/Copenhagen Europe/Dublin Europe/Helsinki Europe/Istanbul Europe/Lisbon Europe/London Europe/Madrid Europe/Moscow Europe/Oslo Europe/Paris Europe/Prague Europe/Rome Europe/Stockholm Europe/Vienna Europe/Warsaw Europe/Zurich Pacific/Auckland Pacific/Fiji Pacific/Honolulu")
	for _, zone := range zones {
		if _, err := cache.load(zone); err != nil {
			t.Fatal(zone, err)
		}
	}
	if len(cache.locations) != timezoneCacheLimit || len(cache.order) != timezoneCacheLimit {
		t.Fatal("unbounded cache", len(cache.locations), len(cache.order))
	}
	if cache.locations[zones[0]] != nil {
		t.Fatal("oldest entry not evicted")
	}
	for i := 0; i < 100; i++ {
		if _, err := cache.load(fmt.Sprintf("Invalid/Zone%d", i)); err == nil {
			t.Fatal("invalid zone accepted")
		}
	}
	if len(cache.locations) != timezoneCacheLimit || cache.locations[zones[len(zones)-1]] == nil {
		t.Fatal("errors changed cache contents")
	}
	if _, err := cache.load(zones[0]); err != nil {
		t.Fatal("evicted location failed to reload", err)
	}
}

func TestTimezoneLocalUsesCurrentHostLocation(t *testing.T) {
	previous := time.Local
	t.Cleanup(func() { time.Local = previous })
	var cache timezoneCache
	for _, offset := range []int{3600, -7200} {
		time.Local = time.FixedZone("test-local", offset)
		location, err := cache.load("Local")
		if err != nil || location != time.Local {
			t.Fatal("stale Local", err)
		}
	}
	if len(cache.locations) != 0 {
		t.Fatal("Local retained in named cache")
	}
}

func TestBudgetUsesUpdatedTimezoneAndRejectsInvalidZone(t *testing.T) {
	store := openLedgerStore(t, t.TempDir())
	now := instant(t, "2026-03-08T12:00:00Z")
	put := func(bucket, key string, value any) {
		t.Helper()
		if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }); err != nil {
			t.Fatal(err)
		}
	}
	put("business.assistants", "owner/assistant", struct {
		OwnerID core.ID
		Budget  core.AssistantBudget
	}{"owner", core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	budgets := Budgets{Store: store, Now: func() time.Time { return now }}
	for _, tc := range []struct{ zone, start string }{{"America/New_York", "2026-03-08T05:00:00Z"}, {"Asia/Shanghai", "2026-03-07T16:00:00Z"}, {"", "2026-03-08T00:00:00Z"}} {
		put("business.settings", "owner", struct{ Timezone string }{tc.zone})
		spec, err := budgets.budget(t.Context(), "owner", "assistant")
		if err != nil || !spec.Start.Equal(instant(t, tc.start)) {
			t.Fatal(tc.zone, spec.Start, err)
		}
	}
	put("business.settings", "owner", struct{ Timezone string }{"Invalid/Zone"})
	if _, err := budgets.budget(t.Context(), "owner", "assistant"); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("invalid timezone silently accepted", err)
	}
}
