package policy

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

type Options struct {
	CheckAccess       func(context.Context, core.Principal, CheckRequest) error
	CheckGroupBudget  func(context.Context, ReserveRequest) error
	ObserveBudget     func(context.Context, core.ID, any, error)
	Now               func() time.Time
	Exchange          func() pricing.ExchangeSnapshot
	ValidatePrincipal func(context.Context, core.Principal) error
	CheckScope        func(context.Context, core.Principal, string) error
	// Owns must enforce exact assistant/peer binding, not only OwnerID.
	Owns              func(context.Context, core.Principal, core.Object) error
	ValidateExecution func(context.Context, core.Execution) error
	// ResourceActions resolves the resource's current provider capabilities.
	// Missing resolution fails closed for grants and assistant resource access.
	ResourceActions     func(context.Context, core.Principal, core.ID) ([]string, error)
	ValidateGrantScope  func(context.Context, core.Principal, core.ID, Scope) error
	ValidateConstraints func(context.Context, core.Principal, core.ID, core.ID, Scope, *Constraints) error
	// ValidateSettlement binds historical persisted jobs and operations even when
	// new execution is stopped. It never authorizes a new reservation or effect.
	ValidateSettlement func(context.Context, core.Execution) error
	VerifyReceipt      func(context.Context, core.Execution, core.ID, core.Money) error
	NoCharge           func(context.Context, core.Execution) (bool, error)
}
type Manager struct {
	store   persistence.Store
	repo    *repository
	options Options
}

func NewService(store persistence.Store, o Options) (*Manager, error) {
	if store == nil || o.ValidatePrincipal == nil {
		return nil, core.Invalid("policy", "store and live identity validation required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Manager{store, &repository{store: store, now: o.Now, exchange: o.Exchange}, o}, nil
}
func (m *Manager) live(ctx context.Context, p core.Principal) error {
	if e := p.Validate(); e != nil {
		return e
	}
	return m.options.ValidatePrincipal(ctx, p)
}
func (m *Manager) owner(ctx context.Context, p core.Principal, scope string) error {
	if e := p.RequireOwner(); e != nil {
		return e
	}
	if e := m.live(ctx, p); e != nil {
		return e
	}
	if p.TokenID != "" {
		if m.options.CheckScope == nil {
			return core.NewError(core.Forbidden, "scope checker unavailable")
		}
		return m.options.CheckScope(ctx, p, scope)
	}
	return nil
}
func (m *Manager) own(ctx context.Context, p core.Principal, target core.Object) error {
	if target.Kind == "" || (target.ID != "" && target.ID.Validate() != nil) {
		return core.Invalid("target", "valid target required")
	}
	if target.ID == "" {
		switch target.Kind {
		case "assistant", "task", "todo", "schedule", "resource", "memory", "settings", "conversation", "file", "credential", "grant", "model", "tool", "operation", "job", "search", "realtime", "audit":
		default:
			return core.Invalid("target", "unknown collection")
		}
	}
	if m.options.Owns == nil {
		return core.NewError(core.Forbidden, "object ownership unavailable")
	}
	return m.options.Owns(ctx, p, target)
}
func (m *Manager) Require(ctx context.Context, p core.Principal, q CheckRequest) error {
	return m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.live(ctx, p); e != nil {
			return e
		}
		if q.FullAccess && (q.Action != Execute || q.ResourceID == "") {
			return core.Invalid("full_access", "compute resource execution required")
		}
		if q.Action != Read && q.Action != Write && q.Action != Execute && q.Action != Manage {
			return core.Invalid("action", "unknown permission")
		}
		if e := m.own(ctx, p, q.Target); e != nil {
			return e
		}
		if m.options.CheckAccess != nil {
			if err := m.options.CheckAccess(ctx, p, q); err != nil {
				return err
			}
		}
		if p.Kind == core.ActorOwner {
			return m.owner(ctx, p, string(q.Action))
		}
		if q.Action == Read && q.ResourceID == "" && q.Target.Kind == "resource" && q.Target.ID == "" {
			return nil
		}
		if q.ResourceID == "" && q.Target.Kind == "resource" {
			q.ResourceID = q.Target.ID
		}
		if q.ResourceID == "" {
			if q.Action == Read && (q.Target.Kind == "search" || q.Target.Kind == "message") {
				return nil
			}
			if q.Action == Read && q.Target.ID == "catalog" && (q.Target.Kind == "model" || q.Target.Kind == "tool") {
				return nil
			}
			if q.Action == Execute && q.Target.ID != "" && (q.Target.Kind == "tool" || q.Target.Kind == "assistant") {
				return nil
			}
			// Built-in objects are authorized by the composition's exact assistant
			// binding check in Owns, not merely by matching the owner account.
			if q.Action == Read || q.Action == Write {
				switch q.Target.Kind {
				case "conversation", "task", "memory", "assistant", "file", "todo", "schedule", "operation", "permission_request":
					return nil
				}
			}
			return core.NewError(core.Forbidden, "assistant resource grant required")
		}
		if q.Target.Kind == "resource" && q.Target.ID != q.ResourceID {
			return core.NewError(core.Forbidden, "resource target mismatch")
		}
		if e := m.own(ctx, p, core.Object{Kind: "resource", ID: q.ResourceID}); e != nil {
			return e
		}
		if supported, e := m.supports(ctx, p, q.ResourceID, []Action{q.Action}); e != nil {
			return e
		} else if !supported {
			return core.NewError(core.Forbidden, "resource action unavailable")
		}
		grants, e := persistence.Scan[Grant](ctx, tx.Store(), "security.grants")
		if e != nil {
			return e
		}
		for _, g := range grants {
			if g.OwnerID != p.OwnerID || g.AssistantID != p.SubjectID || g.ResourceID != q.ResourceID || g.RevokedAt != nil || (g.ExpiresAt != nil && !m.options.Now().Before(*g.ExpiresAt)) {
				continue
			}
			if g.Scope.FullAccess != q.FullAccess {
				continue
			}
			if q.ModelID != "" && g.Scope.ModelID != q.ModelID {
				continue
			}
			allowed := false
			for _, a := range g.Actions {
				if a == q.Action {
					allowed = true
				}
			}
			if !allowed || (g.Scope.ReadOnly && q.Action != Read) {
				continue
			}
			if len(g.Scope.PathPrefixes) > 0 {
				destination := q.Destination
				if u, e := url.Parse(destination); e == nil && u.Scheme != "" {
					destination = u.Path
				}
				if !strings.HasPrefix(destination, "/") {
					continue
				}
				clean := path.Clean(destination)
				match := false
				for _, prefix := range g.Scope.PathPrefixes {
					prefix = path.Clean(prefix)
					if clean == prefix || strings.HasPrefix(clean, strings.TrimSuffix(prefix, "/")+"/") {
						match = true
					}
				}
				if !match {
					continue
				}
			}
			return nil
		}
		return core.NewError(core.Forbidden, "assistant grant denied")
	})
}
func (m *Manager) supports(ctx context.Context, p core.Principal, id core.ID, actions []Action) (bool, error) {
	if m.options.ResourceActions == nil {
		return false, core.NewError(core.Unavailable, "resource capabilities not configured")
	}
	allowed, err := m.options.ResourceActions(ctx, p, id)
	if err != nil {
		return false, err
	}
	for _, action := range actions {
		if !slices.Contains(allowed, string(action)) {
			return false, nil
		}
	}
	return true, nil
}
func (m *Manager) validateGrant(r GrantRequest) error {
	if r.Scope.FullAccess && (r.Scope.ReadOnly || r.Scope.ModelID != "" || len(r.Scope.PathPrefixes) > 0 || len(r.Actions) != 1 || r.Actions[0] != Execute || r.Constraints != nil || r.Temporary != nil) {
		return core.Invalid("full_access", "Full Access is a compute execute permission without quotas or path restrictions")
	}

	if err := r.Constraints.Validate(); err != nil {
		return err
	}
	if r.Temporary != nil {
		if r.Constraints == nil || r.Temporary.Limits.Kind() != r.Constraints.Kind() || !r.Temporary.Until.After(m.options.Now()) || (r.ExpiresAt != nil && r.Temporary.Until.After(*r.ExpiresAt)) {
			return core.Invalid("temporary", "temporary limits require matching base limits and a bounded future expiry")
		}
		if r.Constraints.Network != nil {
			base, next := r.Constraints.Network, r.Temporary.Limits.Network
			if base.Period != next.Period || base.Timezone != next.Timezone {
				return core.Invalid("temporary", "temporary traffic quota must use the base period and timezone")
			}
		}
		if err := r.Temporary.Limits.Validate(); err != nil {
			return err
		}
	}

	if r.AssistantID.Validate() != nil || r.ResourceID.Validate() != nil || len(r.Actions) == 0 {
		return core.Invalid("grant", "assistant, resource and actions required")
	}
	for _, a := range r.Actions {
		if a != Read && a != Write && a != Execute && a != Manage {
			return core.Invalid("action", "unknown permission")
		}
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(m.options.Now()) {
		return core.Invalid("expires_at", "future expiration required")
	}
	for _, prefix := range r.Scope.PathPrefixes {
		if !strings.HasPrefix(prefix, "/") || path.Clean(prefix) != prefix {
			return core.Invalid("path_prefix", "clean absolute prefix required")
		}
	}
	return nil
}
func (m *Manager) Grant(ctx context.Context, p core.Principal, r GrantRequest) (Grant, error) {
	if e := r.Mutation.Validate(false); e != nil {
		return Grant{}, e
	}
	if err := m.validateGrant(r); err != nil {
		return Grant{}, err
	}
	var out Grant
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.owner(ctx, p, "manage"); e != nil {
			return e
		}
		if e := m.own(ctx, p, core.Object{Kind: "assistant", ID: r.AssistantID}); e != nil {
			return e
		}
		if e := m.own(ctx, p, core.Object{Kind: "resource", ID: r.ResourceID}); e != nil {
			return e
		}
		key := string(p.OwnerID) + "/grant/" + r.Mutation.IdempotencyKey
		digest := payloadDigest(r)
		var saved mutationReceipt
		e := tx.Get(ctx, "security.idempotency", key, &saved)
		if e == nil {
			if saved.Digest != digest {
				return persistence.Conflict("grant retry changed payload")
			}
			return json.Unmarshal(saved.Result, &out)
		}
		if !persistence.Missing(e) {
			return e
		}
		if supported, e := m.supports(ctx, p, r.ResourceID, r.Actions); e != nil {
			return e
		} else if !supported {
			return core.Invalid("actions", "permission not supported by resource")
		}
		if m.options.ValidateGrantScope != nil {
			if e := m.options.ValidateGrantScope(ctx, p, r.ResourceID, r.Scope); e != nil {
				return e
			}
		} else if r.Scope.ModelID != "" || r.Scope.FullAccess {
			return core.NewError(core.Unavailable, "model scope validation not configured")
		}
		if m.options.ValidateConstraints != nil {
			if err := m.options.ValidateConstraints(ctx, p, r.ResourceID, r.AssistantID, r.Scope, r.Constraints); err != nil {
				return err
			}
			if r.Temporary != nil {
				if err := m.options.ValidateConstraints(ctx, p, r.ResourceID, r.AssistantID, r.Scope, &r.Temporary.Limits); err != nil {
					return err
				}
			}
		}
		if r.Constraints != nil {
			rows, err := persistence.Scan[Grant](ctx, m.store, "security.grants")
			if err != nil {
				return err
			}
			for _, g := range rows {
				if g.OwnerID == p.OwnerID && g.AssistantID == r.AssistantID && g.ResourceID == r.ResourceID && g.Scope.FullAccess == r.Scope.FullAccess && g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(m.options.Now())) {
					return core.NewError(core.Conflict, "update the existing resource grant instead of stacking quotas")
				}
			}
		}
		out = Grant{CreatedAt: m.options.Now().UTC(), UpdatedAt: m.options.Now().UTC(), Constraints: r.Constraints, Temporary: r.Temporary, ID: newID(), OwnerID: p.OwnerID, AssistantID: r.AssistantID, ResourceID: r.ResourceID, Actions: r.Actions, Scope: r.Scope, ExpiresAt: r.ExpiresAt, Version: 1}
		if e = m.repo.InsertGrant(ctx, out); e != nil {
			return e
		}
		if e = m.audit(ctx, p, "grant.create", out.ID); e != nil {
			return e
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return tx.Put(ctx, "security.idempotency", key, mutationReceipt{digest, b})
	})
	return out, err
}
func (m *Manager) List(ctx context.Context, p core.Principal, q Query) (core.Page[Grant], error) {
	var out core.Page[Grant]
	err := m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.owner(ctx, p, "read"); e != nil {
			return e
		}
		var err error
		out, err = m.repo.Grants(ctx, p.OwnerID, q)
		return err
	})
	return out, err
}
func (m *Manager) Revoke(ctx context.Context, p core.Principal, r RevokeRequest) error {
	if e := r.Mutation.Validate(true); e != nil {
		return e
	}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.owner(ctx, p, "manage"); e != nil {
			return e
		}
		key := string(p.OwnerID) + "/revoke/" + r.Mutation.IdempotencyKey
		want := payloadDigest(r)
		var saved mutationReceipt
		e := tx.Get(ctx, "security.idempotency", key, &saved)
		if e == nil {
			if saved.Digest != want {
				return persistence.Conflict("revoke retry changed payload")
			}
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		if e = m.repo.RevokeGrant(ctx, p.OwnerID, r.GrantID, r.Mutation.ExpectedVersion, m.options.Now()); e != nil {
			return e
		}
		if e = m.audit(ctx, p, "grant.revoke", r.GrantID); e != nil {
			return e
		}
		return tx.Put(ctx, "security.idempotency", key, mutationReceipt{Digest: want})
	})
}
func (m *Manager) execution(ctx context.Context, e core.Execution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := m.live(ctx, e.Principal); err != nil {
		return err
	}
	if m.options.ValidateExecution == nil {
		return core.NewError(core.Forbidden, "execution validation unavailable")
	}
	return m.options.ValidateExecution(ctx, e)
}
func (m *Manager) Reserve(ctx context.Context, r ReserveRequest) (Reservation, error) {
	var out Reservation
	var decision any
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := m.execution(ctx, r.Execution); e != nil {
			return e
		}
		if m.options.CheckGroupBudget != nil {
			if err := m.options.CheckGroupBudget(ctx, r); err != nil {
				return err
			}
		}
		var e error
		out, e = m.repo.reserve(ctx, r, func(data any) { decision = data })
		return e
	})
	if m.options.ObserveBudget != nil {
		m.options.ObserveBudget(ctx, r.Execution.OperationID, decision, err)
	}
	return out, err
}
func (m *Manager) bound(ctx context.Context, e core.Execution, id core.ID) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if m.options.ValidateSettlement != nil {
		if err := m.options.ValidateSettlement(ctx, e); err != nil {
			return err
		}
	} else if err := m.execution(ctx, e); err != nil {
		return err
	}
	r, err := persistence.Load[reservationRecord](ctx, m.store, "security.reservations", persistence.Key(e.Principal.OwnerID, id))
	if err != nil {
		return err
	}
	if r.ExecutionID != e.Principal.ExecutionID || r.AssistantID != e.AssistantID || r.Reservation.OperationID != e.OperationID || r.Reservation.TaskID != e.TaskID {
		return core.NewError(core.Forbidden, "reservation execution mismatch")
	}
	return nil
}
func (m *Manager) Settle(ctx context.Context, e core.Execution, r SettleRequest) (Reservation, error) {
	var out Reservation
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.bound(ctx, e, r.ReservationID); err != nil {
			return err
		}
		if r.OperationID != e.OperationID {
			return core.NewError(core.Forbidden, "operation mismatch")
		}
		if m.options.VerifyReceipt == nil {
			return core.NewError(core.Forbidden, "receipt verification unavailable")
		}
		if err := m.options.VerifyReceipt(ctx, e, r.ReceiptID, r.Actual); err != nil {
			return err
		}
		var err error
		out, err = m.repo.Settle(ctx, e.Principal.OwnerID, r)
		return err
	})
	return out, err
}
func (m *Manager) MarkUnknown(ctx context.Context, e core.Execution, id core.ID) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.bound(ctx, e, id); err != nil {
			return err
		}
		return m.repo.SetReservationState(ctx, e.Principal.OwnerID, id, "unknown")
	})
}
func (m *Manager) Release(ctx context.Context, e core.Execution, id core.ID) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.bound(ctx, e, id); err != nil {
			return err
		}
		if m.options.NoCharge == nil {
			return core.NewError(core.FailedPrecondition, "charge outcome unknown")
		}
		ok, err := m.options.NoCharge(ctx, e)
		if err != nil {
			return err
		}
		if !ok {
			return core.NewError(core.FailedPrecondition, "charge outcome unknown")
		}
		return m.repo.SetReservationState(ctx, e.Principal.OwnerID, id, "released")
	})
}
func (m *Manager) Usage(ctx context.Context, p core.Principal, q UsageQuery) (Usage, error) {
	var out Usage
	err := m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.owner(ctx, p, "read"); err != nil {
			return err
		}
		var err error
		out, err = m.repo.Usage(ctx, p.OwnerID, q)
		return err
	})
	return out, err
}

var _ Service = (*Manager)(nil)

func (m *Manager) audit(ctx context.Context, p core.Principal, action string, target core.ID) error {
	id := newID()
	record := struct {
		ID, OwnerID, SubjectID core.ID
		ActorKind              core.ActorKind
		Action                 string
		Target                 core.Object
		Outcome                string
		At                     time.Time
	}{id, p.OwnerID, p.SubjectID, p.Kind, action, core.Object{Kind: "grant", ID: target}, "succeeded", m.options.Now()}
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.audit", persistence.Key(p.OwnerID, id), record)
	})
}
