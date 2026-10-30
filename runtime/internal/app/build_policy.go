package app

import (
	"context"
	"log"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/policy"
	"time"
)

func (b *Bundle) buildPolicy(ctx context.Context, cfg config.Config, identity *auth.Manager, wiring *buildWiring) (*policy.Manager, error) {
	store := b.store
	access := access{store}
	b.Trace = debugtrace.New(store, debugtrace.Options{ResetContext: func(ctx context.Context, p core.Principal, id core.ID, m core.Mutation, sequence uint64) error {
		if err := identity.CheckScope(ctx, p, "debug.write"); err != nil {
			return err
		}
		if wiring.resetContext == nil {
			return core.NewError(core.FailedPrecondition, "context reset unavailable")
		}
		return wiring.resetContext(ctx, p, id, m, sequence)
	}, Administrator: identity.RequireAdministrator, Enabled: cfg.Debug.Enabled, MaxRuns: cfg.Debug.MaxRuns, MaxPayloadBytes: cfg.Debug.MaxPayloadBytes, Authorize: func(ctx context.Context, p core.Principal) error { return identity.CheckScope(ctx, p, "debug.read") }})
	if traceErr := b.Trace.Recover(ctx); traceErr != nil {
		log.Printf("debug trace recovery unavailable: %s", core.CodeOf(traceErr))
	}
	permissions, err := policy.NewService(store, policy.Options{GrantUsed: func(ctx context.Context, g policy.Grant) error {
		return realtime.NewStore(store).Append(ctx, realtime.Visible{ID: randomOperation(), OwnerID: g.OwnerID, Topic: "grants.changed", Object: core.Object{Kind: "assistant", ID: g.AssistantID}, CreatedAt: time.Now().UTC()})
	}, CheckGroupBudget: func(ctx context.Context, r policy.ReserveRequest) error {
		return checkGroupModelBudget(ctx, store, r, b.Exchange.Snapshot())
	}, CheckAccess: func(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
		return groupAccess(ctx, store, p, q)
	}, ObserveBudget: func(ctx context.Context, id core.ID, data any, err error) {
		state := "succeeded"
		if err != nil {
			state = "failed"
		}
		b.Trace.Record(ctx, "budget.decision", state, id, map[string]any{"Decision": data, "Code": core.CodeOf(err), "Reason": core.ReasonOf(err)})
	}, Exchange: b.Exchange.Snapshot, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, Owns: access.owns, ResourceActions: access.resourceActions, ValidateGrantScope: func(ctx context.Context, p core.Principal, id core.ID, scope policy.Scope) error {
		if err := access.grantScope(ctx, p, id, scope); err != nil {
			return err
		}
		if scope.FullAccess {
			r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, id)
			if err != nil {
				return err
			}
			if wiring.router == nil {
				return core.NewError(core.Unavailable, "sandbox authorization not ready")
			}
			if err := wiring.router.checkFullAccessCapability(ctx, p, r); err != nil {
				return err
			}
		}
		return nil
	}, ValidateConstraints: func(ctx context.Context, p core.Principal, id, assistantID core.ID, scope policy.Scope, c *policy.Constraints) error {
		if scope.FullAccess {
			return nil
		}
		if err := validateGroupConstraints(ctx, store, p, id, assistantID, c); err != nil {
			return err
		}
		return validateResourceConstraints(ctx, store, p, id, c, cfg.Sandbox.ComputeCeilings(), cfg.Sandbox.WorkspaceProjectQuotas)
	}, ValidateExecution: access.execution, ValidateSettlement: access.historicalExecution, VerifyReceipt: access.verifyReceipt, NoCharge: access.noCharge})
	if err != nil {
		return nil, err
	}
	return permissions, nil
}
