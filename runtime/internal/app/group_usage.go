package app

import (
	"context"
	"molr.ai/runtime/internal/accountview"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/quota"
)

func groupPeriod(q auth.ResourceQuota, now time.Time) (time.Time, time.Time) {
	return quota.Period(q.Period, q.Timezone, now)
}
func inGroupPeriod(at, start, end time.Time) bool { return quota.InPeriod(at, start, end) }

// The caller holds the same transaction as the assistant reservation.
func checkGroupModelBudget(ctx context.Context, store persistence.Store, req policy.ReserveRequest, rates pricing.ExchangeSnapshot) error {
	g, err := groupPolicy(ctx, store, req.Execution.Principal.OwnerID)
	if err != nil || g.Administrator {
		return err
	}
	ref, err := groupReference(ctx, store, req.Execution.Principal.OwnerID, "model", req.ModelID)
	if err != nil {
		return err
	}
	old, err := quota.ModelByOperation(ctx, store, req.Execution.Principal.OwnerID, req.Execution.OperationID)
	if err != nil {
		return err
	}
	if old != nil {
		if old.ModelID != req.ModelID {
			return persistence.Conflict("reservation model changed")
		}
		return nil
	}
	start, end := groupPeriod(ref.Quota, time.Now())
	used, err := quota.ModelTotals(ctx, store, req.Execution.Principal.OwnerID, req.ModelID, start, end)
	if err != nil {
		return err
	}
	if err := quota.CheckCalls(used, ref.Quota.Calls, ref.Quota.Concurrent); err != nil {
		return err
	}
	if ref.Quota.Budget != nil {
		return quota.CheckMoney(used, req.Estimate, *ref.Quota.Budget, rates)
	}
	return nil
}
func checkGroupToolQuota(ctx context.Context, store persistence.Store, call tools.Call) error {
	ref, err := groupReference(ctx, store, call.Execution.Principal.OwnerID, "tool", core.ID(call.Name))
	if err != nil || ref == nil {
		return err
	}
	if ref.Quota.Calls == 0 && ref.Quota.Concurrent == 0 {
		return nil
	}
	start, end := groupPeriod(ref.Quota, time.Now())
	used, err := quota.CallTotals(ctx, store, "tool", call.Execution.Principal.OwnerID, core.ID(call.Name), call.Execution.OperationID, start, end)
	if err != nil {
		return err
	}
	return quota.CheckCalls(used, ref.Quota.Calls, ref.Quota.Concurrent)
}

func userUsage(ctx context.Context, store persistence.Store, u auth.UserInfo) (accountview.UsageSummary, error) {
	out := accountview.UsageSummary{UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName, GroupID: u.GroupID, Spent: []core.Money{}, Reserved: []core.Money{}}
	models, err := quota.ModelTotals(ctx, store, u.ID, "", time.Time{}, time.Time{})
	if err != nil {
		return out, err
	}
	tools, err := quota.CallTotals(ctx, store, "tool", u.ID, "", "", time.Time{}, time.Time{})
	if err != nil {
		return out, err
	}
	connections, err := quota.CallTotals(ctx, store, "connection", u.ID, "", "", time.Time{}, time.Time{})
	if err != nil {
		return out, err
	}
	out.ModelCalls, out.ToolCalls, out.ConnectionCalls = models.Calls, tools.Calls, connections.Calls
	// HTTP tools already appear in tool operations; don't double-count them here.
	out.Active, out.Unknown = models.Active+tools.Active, models.Unknown+tools.Unknown
	out.Spent, out.Reserved = models.Spent, models.Reserved
	out.AttachmentBytes, err = quota.AttachmentBytes(ctx, store, u.ID)
	if err != nil {
		return out, err
	}
	err = store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		// Send receipts survive chat cleanup and exclude copied history and retried sends.
		out.UserMessages, err = tx.Count(ctx, "im.receipts", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", u.ID), persistence.Eq("Message.Sender", conversation.User)}})
		return err
	})
	return out, err
}

func adminUsage(ctx context.Context, store persistence.Store, identity *auth.Manager, p core.Principal) (accountview.AdminUsage, error) {
	if err := identity.RequireAdministrator(ctx, p); err != nil {
		return accountview.AdminUsage{}, err
	}
	out := accountview.AdminUsage{Users: []accountview.UsageSummary{}}
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		page := core.PageRequest{Limit: 200}
		for {
			users, err := identity.Users(ctx, p, page)
			if err != nil {
				return err
			}
			for _, u := range users.Items {
				v, err := userUsage(ctx, tx.Store(), u)
				if err != nil {
					return err
				}
				out.Users = append(out.Users, v)
			}
			if users.NextCursor == "" {
				break
			}
			page.Cursor = users.NextCursor
		}
		var err error
		out.ContextStorage, err = contextstore.GlobalUsage(ctx, tx)
		return err
	})
	return out, err
}
