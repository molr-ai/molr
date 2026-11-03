package app

import (
	"context"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/quota"
	"strings"
	"time"
)

func temporaryLive(ctx context.Context, store persistence.Store, a assistant.Assistant) error {
	if a.EmployerID == "" {
		return nil
	}
	parent, err := assistant.NewRepository(store).Get(ctx, a.OwnerID, a.EmployerID)
	if err != nil {
		return err
	}
	if (a.ExpiresAt != nil && !a.ExpiresAt.After(time.Now())) || parent.State != assistant.Active || parent.Hiring == nil || !parent.Hiring.Enabled || parent.Hiring.ID != a.HiringID || (parent.Hiring.ExpiresAt != nil && !parent.Hiring.ExpiresAt.After(time.Now())) {
		return core.WithReason(core.NewError(core.Forbidden, "temporary employment expired or revoked"), "temporary_employment_ended")
	}
	return nil
}
func temporaryAccess(ctx context.Context, store persistence.Store, p core.Principal, q policy.CheckRequest) error {
	if p.Kind != core.ActorAssistant {
		return nil
	}
	a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return err
	}
	if a.EmployerID == "" {
		if q.Target.Kind == "tool" && q.Target.ID == "assistant_hire" && (a.Hiring == nil || !a.Hiring.Enabled || (a.Hiring.ExpiresAt != nil && !a.Hiring.ExpiresAt.After(time.Now()))) {
			return core.WithReason(core.NewError(core.Forbidden, "temporary hiring requires preauthorization"), "hiring_permission_required")
		}
		return nil
	}
	if q.Target.Kind == "tool" {
		name := string(q.Target.ID)
		if strings.HasPrefix(name, "permission_") || strings.HasPrefix(name, "plan_") || strings.HasPrefix(name, "plg_") || strings.HasPrefix(name, "mcp_") || name == "assistant_hire" || name == "assistant_dismiss" {
			return core.WithReason(core.NewError(core.Forbidden, "temporary assistants use preauthorized permissions only; report limitations to your employer"), "temporary_tool_restricted")
		}
	}
	return nil
}
func checkDirectMessage(ctx context.Context, store persistence.Store, p core.Principal, target conversation.Endpoint) error {
	a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return err
	}
	if a.State != assistant.Active {
		return denied()
	}
	if a.EmployerID != "" && (target.Type != conversation.Assistant || target.ID != a.EmployerID) {
		return core.NewError(core.Forbidden, "temporary assistants may communicate only with their employer")
	}
	if target.Type == conversation.User {
		if target.ID != p.OwnerID {
			return denied()
		}
		return nil
	}
	if target.Type != conversation.Assistant || target.ID == a.ID {
		return denied()
	}
	other, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, target.ID)
	if err != nil {
		return err
	}
	if !assistant.CanContact(a, other, time.Now()) {
		return core.WithReason(core.NewError(core.Forbidden, "assistant communication is not authorized by both participants"), "assistant_communication_not_allowed")
	}
	return nil
}
func checkTemporaryBudget(ctx context.Context, store persistence.Store, r policy.ReserveRequest, rates pricing.ExchangeSnapshot) error {
	a, err := assistant.NewRepository(store).Get(ctx, r.Execution.Principal.OwnerID, r.Execution.AssistantID)
	if err != nil {
		return err
	}
	if a.EmployerID == "" {
		return nil
	}
	parent, err := assistant.NewRepository(store).Get(ctx, a.OwnerID, a.EmployerID)
	if err != nil {
		return err
	}
	if err := temporaryLive(ctx, store, a); err != nil {
		return err
	}
	existing, err := quota.ModelByOperation(ctx, store, a.OwnerID, r.Execution.OperationID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	children, err := persistence.Select[assistant.Assistant](ctx, store, "business.assistants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", a.OwnerID), persistence.Eq("HiringID", a.HiringID)}, Fields: []string{"ID"}})
	if err != nil {
		return err
	}
	totals := quota.Totals{}
	for _, child := range children {
		rows, err := quota.ModelRecords(ctx, store, a.OwnerID, child.ID, "", time.Time{}, time.Time{})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Reservation.Settled != nil {
				totals.Amounts = append(totals.Amounts, *row.Reservation.Settled)
			} else if row.Reservation.State == "reserved" || row.Reservation.State == "unknown" {
				totals.Amounts = append(totals.Amounts, row.Reservation.Reserved)
			}
		}
	}
	if err := quota.CheckMoney(totals, r.Estimate, parent.Hiring.Budget, rates); err != nil {
		return core.WithReason(core.NewError(core.BudgetExceeded, "shared temporary assistant budget exhausted"), "temporary_budget_exhausted")
	}
	return nil
}

// Re-enabling communication admits new inputs, never old undelivered messages.
func checkAssistantInput(ctx context.Context, store persistence.Store, p core.Principal, m conversation.Message) error {
	if err := checkDirectMessage(ctx, store, p, conversation.Endpoint{Type: conversation.Assistant, ID: m.SenderID}); err != nil {
		return err
	}
	repo := assistant.NewRepository(store)
	receiver, err := repo.Get(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return err
	}
	sender, err := repo.Get(ctx, p.OwnerID, m.SenderID)
	if err != nil {
		return err
	}
	if receiver.EmployerID == "" && sender.EmployerID == "" && receiver.PeerCommunication != nil && receiver.PeerCommunication.EnabledAt != nil && m.CreatedAt.Before(*receiver.PeerCommunication.EnabledAt) {
		return core.WithReason(core.NewError(core.Forbidden, "message predates the current communication authorization"), "assistant_communication_not_allowed")
	}
	return nil
}
