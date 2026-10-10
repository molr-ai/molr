package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type RateLimitOptions struct {
	Now            func() time.Time
	Limit          int
	Window         time.Duration
	CategoryLimits map[string]int
}
type Limiter struct {
	store   persistence.Store
	options RateLimitOptions
}
type limitRecord struct {
	Window   time.Time
	Attempts int
}

func NewRateLimiter(store persistence.Store, o RateLimitOptions) (*Limiter, error) {
	if store == nil {
		return nil, core.Invalid("limiter", "store required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Limit <= 0 {
		o.Limit = 120
	}
	if o.Window <= 0 {
		o.Window = time.Minute
	}
	copy := make(map[string]int, len(o.CategoryLimits))
	for k, v := range o.CategoryLimits {
		if v <= 0 {
			return nil, core.Invalid("limit", "positive limit required")
		}
		copy[k] = v
	}
	o.CategoryLimits = copy
	return &Limiter{store, o}, nil
}
func (l *Limiter) Allow(ctx context.Context, key RateKey) (Allowance, error) {
	if key.Category == "" || key.Address == "" {
		return Allowance{}, core.Invalid("rate_key", "category and trusted address required")
	}
	encoded, _ := json.Marshal(key)
	sum := sha256.Sum256(encoded)
	id := hex.EncodeToString(sum[:])
	out := Allowance{}
	err := l.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var record limitRecord
		err := tx.Get(ctx, "api.limits", id, &record)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		now := l.options.Now()
		if !now.Before(record.Window.Add(l.options.Window)) {
			record = limitRecord{Window: now}
		}
		limit := l.options.Limit
		if category, ok := l.options.CategoryLimits[key.Category]; ok {
			limit = category
		}
		out.Allowed = record.Attempts < limit
		if out.Allowed {
			record.Attempts++
		} else {
			out.RetryAfter = record.Window.Add(l.options.Window).Sub(now)
			return nil
		}
		return tx.Put(ctx, "api.limits", id, record)
	})
	if err != nil {
		return Allowance{}, err
	}
	return out, nil
}

var _ RateLimiter = (*Limiter)(nil)
