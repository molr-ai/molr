package policy

import (
	"testing"
	"time"
)

func TestCalendarQuotaPeriodsHandleTimezoneAndDST(t *testing.T) {
	tests := []struct{ zone, period, at, start, end string }{
		{"Asia/Shanghai", "day", "2026-10-02T18:00:00Z", "2026-10-02T16:00:00Z", "2026-10-03T16:00:00Z"},
		{"America/New_York", "day", "2026-03-08T12:00:00Z", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"UTC", "month", "2026-02-28T12:00:00Z", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"},
		{"UTC", "week", "2026-10-04T12:00:00Z", "2026-09-28T00:00:00Z", "2026-10-05T00:00:00Z"},
	}
	for _, tc := range tests {
		at, _ := time.Parse(time.RFC3339, tc.at)
		start, end := NetworkPeriod(NetworkConstraints{Period: tc.period, Timezone: tc.zone}, at)
		if start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end {
			t.Fatal(tc, start, end)
		}
	}
}
func TestQuotaTypesAndExplicitUnlimited(t *testing.T) {
	c := &Constraints{Compute: &ComputeConstraints{CPUPercent: 200, MemoryBytes: 2 << 30, Processes: 128, TempBytes: 64 << 20, MaxDurationSeconds: 1800}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Compute.MaxDurationSeconds = 0
	if c.Validate() == nil {
		t.Fatal("zero duration became unlimited")
	}
	c.Compute.UnlimitedDuration = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	g := Grant{Constraints: c, Temporary: &TemporaryConstraints{Limits: Constraints{Compute: &ComputeConstraints{CPUPercent: 400}}, Until: now.Add(time.Minute)}}
	if g.EffectiveConstraints(now).Compute.CPUPercent != 400 || g.EffectiveConstraints(now.Add(time.Minute)).Compute.CPUPercent != 200 {
		t.Fatal("temporary limits did not expire")
	}
}
