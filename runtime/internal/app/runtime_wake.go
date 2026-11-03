package app

import (
	"context"
	"time"

	"molr.ai/runtime/internal/persistence"
)

var runtimeBuckets = []string{
	"business.watches", "business.schedules", "business.todos", "business.plan-links", "business.tasks",
	"business.assistants", "security.grants", "auth.accounts", "auth.groups", "agent.jobs",
	"business.dream_configs", "business.dream_timers", "business.dream_sessions_index",
	"business.settings", "sandbox.remote_waits", "files.gc",
}

func runtimeSignals() []persistence.SignalKey {
	keys := []persistence.SignalKey{{Namespace: "runtime.watch", Key: "completed"}}
	for _, bucket := range runtimeBuckets {
		keys = append(keys, persistence.BucketSignal(bucket))
	}
	return keys
}

// Time-driven transitions sleep until the nearest persisted boundary. External
// node availability and failed file deletion are retried only while work exists.
func nextRuntimeTick(ctx context.Context, store persistence.Store, now time.Time) (time.Time, error) {
	var next time.Time
	consider := func(at *time.Time) {
		if at != nil && at.After(now) && (next.IsZero() || at.Before(next)) {
			next = *at
		}
	}
	type boundary struct {
		StartAt, EndAt, RetryAt, Deadline *time.Time
		Cursor                            struct{ NextAt *time.Time }
		State                             string
		Paused, Completed                 bool
	}
	for _, bucket := range []string{"business.todos", "business.watches", "business.dream_timers", "business.dream_sessions_index"} {
		where := []persistence.Filter{persistence.Eq("State", "active")}
		switch bucket {
		case "business.todos":
			where = []persistence.Filter{persistence.Eq("Completed", false)}
		case "business.watches":
			where = []persistence.Filter{persistence.Eq("Paused", false), {Field: "State", Op: "!=", Value: "completed"}}
		case "business.dream_timers":
			where = []persistence.Filter{{Field: "Cursor.NextAt", Op: "!=", Value: nil}}
		}
		rows, err := persistence.Select[boundary](ctx, store, bucket, persistence.Query{Where: where, Fields: []string{"StartAt", "EndAt", "RetryAt", "Deadline", "Cursor", "State", "Paused", "Completed"}, Unordered: true})
		if err != nil {
			return next, err
		}
		for _, row := range rows {
			if row.Paused || row.Completed || row.State == "completed" {
				continue
			}
			consider(row.StartAt)
			consider(row.EndAt)
			consider(row.RetryAt)
			consider(row.Cursor.NextAt)
			if row.State == "active" {
				consider(row.Deadline)
			}
		}
	}
	rows, err := persistence.Select[struct {
		EmployerID string
		ExpiresAt  *time.Time
		Hiring     *struct {
			Enabled   bool
			ExpiresAt *time.Time
		}
	}](ctx, store, "business.assistants", persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "active")}, Fields: []string{"EmployerID", "ExpiresAt", "Hiring"}})
	if err != nil {
		return next, err
	}
	for _, row := range rows {
		if row.EmployerID != "" {
			consider(row.ExpiresAt)
		}
		if row.Hiring != nil && row.Hiring.Enabled {
			consider(row.Hiring.ExpiresAt)
		}
	}
	for _, pending := range []struct {
		bucket string
		retry  time.Duration
	}{{"sandbox.remote_waits", 15 * time.Second}, {"files.gc", 30 * time.Second}} {
		var n int64
		err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var err error
			n, err = tx.Count(ctx, pending.bucket, persistence.Query{})
			return err
		})
		if err != nil {
			return next, err
		}
		if n > 0 {
			at := now.Add(pending.retry)
			consider(&at)
		}
	}
	return next, nil
}
