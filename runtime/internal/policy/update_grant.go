package policy

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// UpdateGrant changes scope/actions/expiry atomically. The recipient and resource
// remain fixed, and revoked grants cannot be revived through editing.
func (m *Manager) UpdateGrant(ctx context.Context, p core.Principal, r UpdateGrantRequest) (Grant, error) {
	if err := r.Mutation.Validate(true); err != nil {
		return Grant{}, err
	}
	if err := r.GrantID.Validate(); err != nil {
		return Grant{}, err
	}
	var out Grant
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.owner(ctx, p, "manage"); err != nil {
			return err
		}
		var err error
		out, err = persistence.Load[Grant](ctx, m.store, "security.grants", persistence.Key(p.OwnerID, r.GrantID))
		if err != nil {
			return err
		}
		if out.OwnerID != p.OwnerID {
			return core.NewError(core.Forbidden, "foreign grant")
		}
		if err := m.own(ctx, p, core.Object{Kind: "assistant", ID: out.AssistantID}); err != nil {
			return err
		}
		if err := m.own(ctx, p, core.Object{Kind: "resource", ID: out.ResourceID}); err != nil {
			return err
		}
		key := string(p.OwnerID) + "/grant.update/" + string(r.GrantID) + "/" + r.Mutation.IdempotencyKey
		digest := payloadDigest(r)
		var saved mutationReceipt
		err = tx.Get(ctx, "security.idempotency", key, &saved)
		if err == nil {
			if saved.Digest != digest {
				return persistence.Conflict("grant update payload changed")
			}
			return json.Unmarshal(saved.Result, &out)
		}
		if !persistence.Missing(err) {
			return err
		}
		if out.Version != r.Mutation.ExpectedVersion {
			return core.WithReason(persistence.Conflict("grant version mismatch"), "grant_changed")
		}
		if out.RevokedAt != nil {
			return core.NewError(core.FailedPrecondition, "grant revoked")
		}
		request := GrantRequest{Constraints: r.Constraints, Temporary: r.Temporary, AssistantID: out.AssistantID, ResourceID: out.ResourceID, Actions: r.Actions, Scope: r.Scope, ExpiresAt: r.ExpiresAt}
		if err := m.validateGrant(request); err != nil {
			return err
		}
		if supported, err := m.supports(ctx, p, out.ResourceID, r.Actions); err != nil {
			return err
		} else if !supported {
			return core.Invalid("actions", "permission not supported by resource")
		}
		if m.options.ValidateGrantScope != nil {
			if err := m.options.ValidateGrantScope(ctx, p, out.ResourceID, r.Scope); err != nil {
				return err
			}
		} else if r.Scope.ModelID != "" || r.Scope.FullAccess {
			return core.NewError(core.Unavailable, "model scope validation not configured")
		}
		if out.Constraints != nil && r.Constraints == nil && !r.Scope.FullAccess {
			return core.Invalid("constraints", "resource quotas cannot be removed")
		}
		if out.Constraints != nil && out.Constraints.Network != nil && r.Constraints != nil && r.Constraints.Network != nil {
			old, next := out.Constraints.Network, r.Constraints.Network
			if old.Period != next.Period || old.Timezone != next.Timezone {
				return core.Invalid("constraints.network", "period and timezone are immutable; changing them would reset usage")
			}
		}
		if m.options.ValidateConstraints != nil {
			if err := m.options.ValidateConstraints(ctx, p, out.ResourceID, out.AssistantID, r.Scope, r.Constraints); err != nil {
				return err
			}
			if r.Temporary != nil {
				if err := m.options.ValidateConstraints(ctx, p, out.ResourceID, out.AssistantID, r.Scope, &r.Temporary.Limits); err != nil {
					return err
				}
			}
		}
		if out.Scope.FullAccess && !r.Scope.FullAccess && r.Constraints != nil {
			rows, err := ActiveGrants(ctx, m.store, out.OwnerID, Query{AssistantID: out.AssistantID, ResourceID: out.ResourceID}, m.options.Now())
			if err != nil {
				return err
			}
			for _, other := range rows {
				if other.ID != out.ID && other.OwnerID == out.OwnerID && other.AssistantID == out.AssistantID && other.ResourceID == out.ResourceID && !other.Scope.FullAccess && other.RevokedAt == nil && (other.ExpiresAt == nil || other.ExpiresAt.After(m.options.Now())) {
					return core.WithReason(core.NewError(core.Conflict, "update the existing isolated grant instead of stacking quotas"), "grant_exists")
				}
			}
		}
		out.Constraints, out.Temporary = r.Constraints, r.Temporary
		out.Actions = r.Actions
		out.Scope = r.Scope
		out.ExpiresAt = r.ExpiresAt
		out.Version++
		out.UpdatedAt = m.options.Now().UTC()
		if err := tx.Put(ctx, "security.grants", persistence.Key(p.OwnerID, out.ID), out); err != nil {
			return err
		}
		if err := m.audit(ctx, p, "grant.update", out.ID); err != nil {
			return err
		}
		data, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return tx.Put(ctx, "security.idempotency", key, mutationReceipt{digest, data})
	})
	return out, err
}
