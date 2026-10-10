package quota

import (
	"testing"
	"time"
)

func BenchmarkPeriodTimezone(b *testing.B) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York"} {
		b.Run(zone, func(b *testing.B) {
			Period("daily", zone, now)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				start, end := Period("daily", zone, now)
				if !end.After(start) {
					b.Fatal("invalid calendar period")
				}
			}
		})
	}
}
