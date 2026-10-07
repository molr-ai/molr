package policy

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/quota"
	"time"
)

type Constraints = core.ResourceConstraints
type ComputeConstraints = core.ComputeConstraints
type NetworkConstraints = core.NetworkConstraints
type StorageConstraints = core.StorageConstraints

// TemporaryConstraints replace the base limits until Until; they do not reset usage.
type TemporaryConstraints struct {
	Limits Constraints `json:"limits"`
	Until  time.Time   `json:"until"`
}

func (g Grant) EffectiveConstraints(now time.Time) *Constraints {
	if g.Temporary != nil && now.Before(g.Temporary.Until) {
		return &g.Temporary.Limits
	}
	return g.Constraints
}

func NetworkPeriod(v NetworkConstraints, now time.Time) (time.Time, time.Time) {
	return quota.Period(v.Period, v.Timezone, now)
}
