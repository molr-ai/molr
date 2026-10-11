package auth

import (
	"context"
	"log"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const credentialRetention = 7 * 24 * time.Hour

// Collect removes at most limit records in one short transaction. The predicates
// are re-evaluated under the write lock, including the latest session activity.
func (m *Manager) Collect(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, core.Invalid("limit", "cleanup batch must be between 1 and 500")
	}
	n := 0
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		now := m.options.Now()
		cutoff := now.Add(-credentialRetention)
		jobs := []struct {
			bucket string
			query  persistence.Query
		}{
			{"auth.challenges", persistence.Query{Where: []persistence.Filter{{Field: "ExpiresAtKey", Op: "<=", Value: persistence.TimeKey(now)}}}},
			{loginLinksBucket, persistence.Query{Where: []persistence.Filter{{Field: "ExpiresAtKey", Op: "<=", Value: persistence.TimeKey(cutoff)}}}},
			{"auth.sessions", persistence.Query{Any: [][]persistence.Filter{
				{persistence.Eq("RevokedAt", nil), {Field: "ExpiresAtKey", Op: "<=", Value: persistence.TimeKey(cutoff)}},
				{persistence.Eq("RevokedAt", nil), {Field: "LastSeenAtKey", Op: "<=", Value: persistence.TimeKey(cutoff.Add(-m.options.SessionIdle))}},
				{{Field: "RevokedAt", Op: "!=", Value: nil}, {Field: "RevokedAtKey", Op: "<=", Value: persistence.TimeKey(cutoff)}},
			}}},
		}
		for _, job := range jobs {
			if n == limit {
				break
			}
			job.query.Limit, job.query.Unordered = limit-n, true
			deleted, err := tx.DeleteMatching(ctx, job.bucket, job.query)
			if err != nil {
				return err
			}
			n += int(deleted)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (m *Manager) RunGC(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		batch, cancel := context.WithTimeout(ctx, 5*time.Second)
		n, err := m.Collect(batch, 500)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("auth collection unavailable: %s", core.CodeOf(err))
		}
		delay := time.Hour
		if err == nil && n == 500 {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}
