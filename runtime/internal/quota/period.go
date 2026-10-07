package quota

import "time"

// Period uses calendar boundaries in the authorization's fixed timezone.
func Period(period, timezone string, now time.Time) (time.Time, time.Time) {
	if period == "lifetime" || period == "" {
		return time.Unix(0, 0).UTC(), time.Time{}
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		zone = time.UTC
	}
	local := now.In(zone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	switch period {
	case "week", "weekly":
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		return start.UTC(), start.AddDate(0, 0, 7).UTC()
	case "month":
		start = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, zone)
		return start.UTC(), start.AddDate(0, 1, 0).UTC()
	default:
		return start.UTC(), start.AddDate(0, 0, 1).UTC()
	}
}
