package contextstore

import (
	"context"
	"log"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
)

// RunGC performs bounded startup/periodic collection. No node still needed by
// execution, recovery or retained history is eligible, regardless of its age.
func RunGC(ctx context.Context, store persistence.Store) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		batch, cancel := context.WithTimeout(ctx, 2*time.Second)
		n, err := Collect(batch, store, 200)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("context collection unavailable: %s", core.CodeOf(err))
		}
		delay := time.Minute
		if err == nil && n == 200 {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}
