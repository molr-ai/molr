package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
	"time"
)

func checkPluginConnectionQuota(ctx context.Context, store persistence.Store, p core.Principal, id core.ID) error {
	ref, err := groupReference(ctx, store, p.OwnerID, "connection", id)
	if err != nil || ref == nil {
		return err
	}
	if ref.Quota.Calls == 0 && ref.Quota.Concurrent == 0 {
		return nil
	}
	start, end := groupPeriod(ref.Quota, time.Now())
	used, err := quota.CallTotals(ctx, store, "connection", p.OwnerID, id, "", start, end)
	if err != nil {
		return err
	}
	return quota.CheckCalls(used, ref.Quota.Calls, ref.Quota.Concurrent)
}
