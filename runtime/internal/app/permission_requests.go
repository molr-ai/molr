package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/notification"
	"slices"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

const permissionRequestsBucket = "security.permission_requests"

type permissionBatchKey struct{}
type permissionRequests struct {
	router        *sandboxRouter
	afterApproval func(context.Context, core.Principal, []policy.Grant) error
	store         persistence.Store
	authorizer    policy.Authorizer
	grants        policy.Grants
	chat          conversation.Publisher
}

func permissionDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *permissionRequests) create(ctx context.Context, e core.Execution, input policy.PermissionRequestInput) (json.RawMessage, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if e.Principal.Kind != core.ActorAssistant {
		return nil, denied()
	}
	if err := input.Normalize(time.Now()); err != nil {
		return nil, err
	}
	var out policy.PermissionRequest
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorizer.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "permission_request"}, Action: policy.Execute}); err != nil {
			return err
		}
		if err := (access{s.store}).execution(ctx, e); err != nil {
			return err
		}
		for i, item := range append([]policy.PermissionRequestInput{input}, input.Additional...) {
			if err := s.validateProposal(ctx, e, item); err != nil {
				return policy.PermissionRequestItemError(i, err)
			}
		}
		key := persistence.Key(e.Principal.OwnerID, e.OperationID)
		var saved core.ID
		if err := tx.Get(ctx, "security.permission_operations", key, &saved); err == nil {
			return tx.Get(ctx, permissionRequestsBucket, persistence.Key(e.Principal.OwnerID, saved), &out)
		} else if !persistence.Missing(err) {
			return err
		}
		for i, item := range append([]policy.PermissionRequestInput{input}, input.Additional...) {
			if err := s.checkExistingPermission(ctx, e, item); err != nil {
				return policy.PermissionRequestItemError(i, err)
			}
		}
		rows, err := s.requestRows(ctx, e.Principal.OwnerID, e.AssistantID, true, 0)
		if err != nil {
			return err
		}
		need := input
		need.Reason = ""
		pending := 0
		for _, row := range rows {
			if row.State != "pending" {
				continue
			}
			pending++
			prior := row.Input
			prior.Reason = ""
			if row.TaskID == e.TaskID && permissionDigest(prior) == permissionDigest(need) {
				out = row
				return tx.Put(ctx, "security.permission_operations", key, row.ID)
			}
		}
		if pending >= 20 {
			return core.NewError(core.FailedPrecondition, "resolve existing permission requests first")
		}
		out = policy.PermissionRequest{ID: randomOperation(), OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, TaskID: e.TaskID, Input: input, State: "pending", Version: 1, CreatedAt: time.Now().UTC()}
		if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key(out.OwnerID, out.ID), out); err != nil {
			return err
		}
		if err := tx.Put(ctx, "security.permission_operations", key, out.ID); err != nil {
			return err
		}
		a, err := assistant.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.AssistantID)
		if err != nil {
			return err
		}
		if s.chat == nil {
			return core.NewError(core.FailedPrecondition, "conversation publisher required")
		}
		title := input.ResourceDescription
		if len(title) > 256 {
			runes := []rune(title)
			title = string(runes[:min(64, len(runes))])
		}
		message, err := s.chat.Publish(ctx, e, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: "permission-request/" + string(out.ID)}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Card, Card: &conversation.CardContent{Kind: conversation.PermissionRequestCard, TargetID: out.ID, Title: title, Summary: input.Reason}}})
		if err != nil {
			return err
		}
		out.MessageID = message.ID
		if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key(out.OwnerID, out.ID), out); err != nil {
			return err
		}
		return s.notify(ctx, out)
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (s *permissionRequests) requestRows(ctx context.Context, owner, assistantID core.ID, pending bool, limit int) ([]policy.PermissionRequest, error) {
	op := "="
	if !pending {
		op = "!="
	}
	return persistence.Select[policy.PermissionRequest](ctx, s.store, permissionRequestsBucket, persistence.Query{
		Where:   []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistantID), {Field: "State", Op: op, Value: "pending"}},
		OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", Limit: limit,
	})
}

func (s *permissionRequests) rows(ctx context.Context, owner, assistantID core.ID, limit int) ([]policy.PermissionRequest, error) {
	rows := []policy.PermissionRequest{}
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		pending, err := s.requestRows(ctx, owner, assistantID, true, limit)
		if err != nil {
			return err
		}
		rows = append(rows, pending...)
		if len(rows) == limit {
			return nil
		}
		decided, err := s.requestRows(ctx, owner, assistantID, false, limit-len(rows))
		if err != nil {
			return err
		}
		rows = append(rows, decided...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *permissionRequests) List(ctx context.Context, p core.Principal, id core.ID) ([]policy.PermissionRequest, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if id.Validate() != nil {
		return nil, core.Invalid("assistant_id", "required")
	}
	if err := s.authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: id}, Action: policy.Read}); err != nil {
		return nil, err
	}
	return s.rows(ctx, p.OwnerID, id, 200)
}

func (s *permissionRequests) Get(ctx context.Context, p core.Principal, id core.ID) (policy.PermissionRequest, error) {
	var out policy.PermissionRequest
	if err := p.RequireOwner(); err != nil {
		return out, err
	}
	if id.Validate() != nil {
		return out, core.Invalid("request_id", "required")
	}
	if err := s.authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "permission_request", ID: id}, Action: policy.Read}); err != nil {
		return out, err
	}
	return persistence.Load[policy.PermissionRequest](ctx, s.store, permissionRequestsBucket, persistence.Key(p.OwnerID, id))
}

func (s *permissionRequests) notify(ctx context.Context, row policy.PermissionRequest) error {
	if err := notification.Sync(ctx, s.store, row.OwnerID, notification.Input{Kind: "permission.request", Code: "pending", Source: core.Object{Kind: "permission_request", ID: row.ID}, AssistantID: row.AssistantID, Label: row.Input.ResourceDescription}, row.State == "pending"); err != nil {
		return err
	}
	return realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: randomOperation(), OwnerID: row.OwnerID, Topic: "permission.request", Object: core.Object{Kind: "assistant", ID: row.AssistantID}, Version: row.Version, CreatedAt: time.Now().UTC()})
}

func (s *permissionRequests) Resolve(ctx context.Context, p core.Principal, q policy.PermissionResolution) (policy.PermissionRequest, error) {
	var out policy.PermissionRequest
	if err := p.RequireOwner(); err != nil {
		return out, err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return out, err
	}
	if q.RequestID.Validate() != nil || (q.Decision != "approve" && q.Decision != "reject") {
		return out, core.Invalid("decision", "approve or reject required")
	}
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "permission_request", ID: q.RequestID}, Action: policy.Manage}); err != nil {
			return err
		}
		key := persistence.Key(p.OwnerID, q.RequestID)
		if err := tx.Get(ctx, permissionRequestsBucket, key, &out); err != nil {
			return err
		}
		if err := s.authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: out.AssistantID}, Action: policy.Read}); err != nil {
			return err
		}
		receiptKey := persistence.Key(p.OwnerID, core.ID(q.Mutation.IdempotencyKey))
		var digest string
		if err := tx.Get(ctx, "security.permission_decisions", receiptKey, &digest); err == nil {
			if digest != permissionDigest(q) {
				return persistence.Conflict("decision retry changed payload")
			}
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		if out.State != "pending" || out.Version != q.Mutation.ExpectedVersion {
			return core.WithReason(persistence.Conflict("permission request changed"), "permission_request_changed")
		}
		a, err := assistant.NewRepository(s.store).Get(ctx, p.OwnerID, out.AssistantID)
		if err != nil {
			return err
		}
		if a.State != assistant.Active {
			return core.NewError(core.FailedPrecondition, "assistant archived")
		}
		if q.Decision == "approve" {
			if len(q.Additional) != len(out.Input.Additional) {
				return core.Invalid("additional", "review every item in the permission group")
			}
			if len(q.Additional) > 7 {
				return core.Invalid("additional", "at most eight grants")
			}
			approved := []policy.Grant{}
			batchCtx := context.WithValue(ctx, permissionBatchKey{}, true)
			for i, item := range append([]policy.PermissionResolution{q}, q.Additional...) {
				if i > 0 && len(item.Additional) > 0 {
					return core.Invalid("additional", "nested approvals are not supported")
				}
				// One owner-confirmed deadline applies to every grant, including no expiry.
				item.ExpiresAt = q.ExpiresAt
				if err := s.validateApproval(batchCtx, p, item); err != nil {
					return err
				}
				mutationID := fmt.Sprintf("permission-request/%s/%d", out.ID, i)
				var grant policy.Grant
				var err error
				if item.GrantID != "" {
					current, loadErr := persistence.Load[policy.Grant](batchCtx, s.store, "security.grants", persistence.Key(p.OwnerID, item.GrantID))
					if loadErr != nil {
						return loadErr
					}
					if current.AssistantID != out.AssistantID || current.ResourceID != item.ResourceID {
						return denied()
					}
					grant, err = s.grants.UpdateGrant(batchCtx, p, policy.UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: mutationID, ExpectedVersion: item.GrantVersion}, GrantID: item.GrantID, Actions: item.Actions, Scope: item.Scope, ExpiresAt: item.ExpiresAt, Constraints: item.Constraints, Temporary: item.Temporary})
				} else {
					grant, err = s.grants.Grant(batchCtx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: mutationID}, AssistantID: out.AssistantID, ResourceID: item.ResourceID, Actions: item.Actions, Scope: item.Scope, ExpiresAt: item.ExpiresAt, Constraints: item.Constraints, Temporary: item.Temporary})
				}
				if err != nil {
					return err
				}
				approved = append(approved, grant)
			}
			out.GrantedAll = approved
			out.State, out.GrantID, out.Granted = "approved", approved[0].ID, &approved[0]
		} else {
			if len(q.Additional) > 0 || q.Constraints != nil || q.Temporary != nil || q.GrantID != "" || q.GrantVersion != 0 || q.ResourceID != "" || len(q.Actions) > 0 || q.Scope.ModelID != "" || len(q.Scope.PathPrefixes) > 0 || q.Scope.ReadOnly || q.ExpiresAt != nil {
				return core.Invalid("decision", "reject takes no grant fields")
			}
			out.State = "rejected"
		}
		now := time.Now().UTC()
		out.ResolvedAt = &now
		out.Version++
		if err := tx.Put(ctx, permissionRequestsBucket, key, out); err != nil {
			return err
		}
		if err := tx.Put(ctx, "security.permission_decisions", receiptKey, permissionDigest(q)); err != nil {
			return err
		}
		if err := s.notify(ctx, out); err != nil {
			return err
		}
		// A decision wakes the assistant, but never overrides a manual stop or a terminal task.
		status, err := persistence.Load[agent.Status](ctx, s.store, "agent.status", persistence.Key(p.OwnerID, out.AssistantID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, s.store, "agent.interrupt_boundaries", persistence.Key(p.OwnerID, out.AssistantID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if !boundary.At.IsZero() && !out.CreatedAt.After(boundary.At) {
			return nil
		}
		if status.State == agent.Stopped || status.State == agent.Paused {
			return nil
		}
		if out.TaskID != "" {
			t, err := persistence.Load[task.Task](ctx, s.store, "business.tasks", persistence.Key(p.OwnerID, out.TaskID))
			if err != nil {
				return err
			}
			if t.State != task.Running && t.State != task.Waiting && t.State != task.Pending {
				return nil
			}
		}
		return event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: core.ID("permission-" + string(out.ID)), OwnerID: p.OwnerID, AssistantID: out.AssistantID, TaskID: out.TaskID, Kind: event.ResumeRequested, DeliveryMode: event.WhenIdle, CreatedAt: now})
	})
	if err == nil && out.State == "approved" && s.afterApproval != nil {
		err = s.afterApproval(ctx, p, out.GrantedAll)
	}
	return out, err
}

func (s *permissionRequests) validateApproval(ctx context.Context, p core.Principal, q policy.PermissionResolution) error {
	// The owner's submitted fields are authoritative. The assistant's proposal
	// pre-fills the form but does not constrain the owner's own resource choices.
	r, err := resource.NewRepository(s.store).Get(ctx, p.OwnerID, q.ResourceID)
	if err != nil {
		return err
	}
	if len(q.Actions) == 0 || len(q.Actions) > 4 || len(q.Scope.PathPrefixes) > 32 {
		return core.Invalid("grant", "bounded actions and paths required")
	}
	if r.Kind != resource.Storage && len(q.Scope.PathPrefixes) > 0 {
		return core.Invalid("scope", "paths require storage")
	}
	raw, err := json.Marshal(q)
	if err != nil || len(raw) > 8192 {
		return core.Invalid("grant", "grant too large")
	}
	// Grants.Grant validates ownership, supported actions, exact model and path
	// scopes, expiration and deployment restrictions inside this transaction.
	return nil
}

func (s *permissionRequests) context(ctx context.Context, e core.Execution) ([]agent.ContextDocument, error) {
	if err := s.authorizer.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "permission_list"}, Action: policy.Execute}); err != nil {
		if core.ReasonOf(err) == "group_resource_denied" {
			return []agent.ContextDocument{}, nil
		}
		return nil, err
	}
	rows, err := s.rows(ctx, e.Principal.OwnerID, e.AssistantID, 40)
	if err != nil {
		return nil, err
	}
	// Include all pending requests and the most recent decisions. Their state is
	// evidence only: execution and permission_list still recheck actual grants.
	for i := range rows {
		rows[i].Granted = nil
	} // query current grants via permission_list
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	permissions, err := permissionsContext(ctx, s.store, e.Principal, time.Now())
	if err != nil {
		return nil, err
	}
	return []agent.ContextDocument{{Kind: "permission_requests", Source: "permission_requests", Scope: "assistant", Revision: permissionDigest(rows), Content: string(raw)}, permissions}, nil
}

// Validate every target before writing any request, receipt, card or notification.
func (s *permissionRequests) validateProposal(ctx context.Context, e core.Execution, input policy.PermissionRequestInput) error {
	r, err := resource.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, input.ResourceID)
	if persistence.Missing(err) {
		return core.Invalid("resource_id", "resource does not exist or is not requestable by this account; refresh permission_list")
	}
	if err != nil {
		return err
	}
	if input.FullAccess {
		if s.router == nil {
			return core.Invalid("full_access", "Full Access unavailable")
		}
		if err := s.router.checkFullAccessCapability(ctx, e.Principal, r); err != nil {
			return err
		}
	}
	if string(r.Kind) != input.ResourceKind {
		return core.Invalid("resource_kind", fmt.Sprintf("resource kind mismatch: selected resource has kind %s", r.Kind))
	}
	if r.State == resource.Disconnected {
		return core.Invalid("resource_id", "resource is disconnected; connect it before requesting permission")
	}
	for _, action := range input.Actions {
		if !slices.Contains(r.Actions, string(action)) {
			return core.Invalid("actions", fmt.Sprintf("selected resource does not support action %s; refresh permission_list", action))
		}
	}
	if err := (access{s.store}).grantScope(ctx, e.Principal, input.ResourceID, policy.Scope{FullAccess: input.FullAccess, ModelID: input.ModelID, PathPrefixes: input.PathPrefixes}); err != nil {
		return err
	}
	if input.GrantID != "" {
		g, err := persistence.Load[policy.Grant](ctx, s.store, "security.grants", persistence.Key(e.Principal.OwnerID, input.GrantID))
		if persistence.Missing(err) {
			return core.Invalid("grant_id", "grant does not exist; refresh permission_list")
		}
		if err != nil {
			return err
		}
		if g.AssistantID != e.AssistantID || g.ResourceID != input.ResourceID || g.RevokedAt != nil {
			return core.Invalid("grant_id", "grant must be active and belong to this assistant and selected resource")
		}
		if g.Version != input.GrantVersion {
			return &core.Error{Code: core.Conflict, Field: "grant_version", Message: "grant changed; refresh permission_list", Reason: "grant_changed"}
		}
	}
	return nil
}
