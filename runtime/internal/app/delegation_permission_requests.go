package app

import (
	"context"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"slices"
	"time"
)

func permissionRequestTitle(input policy.PermissionRequestInput) string {
	switch input.PermissionType {
	case "temporary_hiring":
		return "Temporary hiring"
	case "assistant_communication":
		return "Assistant communication"
	default:
		return input.ResourceDescription
	}
}

func (s *permissionRequests) validateDelegationProposal(ctx context.Context, e core.Execution, input policy.PermissionRequestInput) error {
	a, err := assistant.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.AssistantID)
	if err != nil {
		return err
	}
	if a.State != assistant.Active || a.EmployerID != "" {
		return denied()
	}
	if input.Hiring != nil {
		grants, err := policy.ActiveGrants(ctx, s.store, e.Principal.OwnerID, policy.Query{AssistantID: e.AssistantID}, time.Now())
		if err != nil {
			return err
		}
		for _, id := range input.Hiring.GrantIDs {
			index := slices.IndexFunc(grants, func(g policy.Grant) bool { return g.ID == id })
			if index < 0 || grants[index].Scope.FullAccess || grants[index].Scope.Once {
				return core.Invalid("hiring.grant_ids", "select this assistant's current delegable grants from permission_list")
			}
			r, err := resource.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, grants[index].ResourceID)
			if err != nil {
				return err
			}
			if r.Kind != resource.Model && r.Kind != resource.Storage && r.Kind != resource.Compute {
				return core.Invalid("hiring.grant_ids", "only model, storage and isolated compute grants may be delegated")
			}
		}
	}
	// Proposed peer IDs are untrusted hints. Resolve them only during owner review,
	// so this tool cannot be used as an existence oracle for hidden assistants.
	if input.Communication != nil && slices.Contains(input.Communication.AssistantIDs, a.ID) {
		return core.Invalid("communication.assistant_ids", "choose another assistant")
	}
	return nil
}

func validityCovers(current, requested *time.Time, now time.Time) bool {
	return current == nil || current.After(now) && requested != nil && !current.Before(*requested)
}
func (s *permissionRequests) checkExistingDelegation(ctx context.Context, e core.Execution, input policy.PermissionRequestInput) error {
	a, err := assistant.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.AssistantID)
	if err != nil {
		return err
	}
	covered := false
	if h, p := a.Hiring, input.Hiring; h != nil && p != nil && h.Enabled && validityCovers(h.ExpiresAt, input.ExpiresAt, time.Now()) {
		covered = h.MaxAssistants >= p.MaxAssistants && h.Budget.Currency == p.Budget.Currency && h.Budget.Micros >= p.Budget.Micros && len(p.GrantIDs) > 0
		for _, id := range p.GrantIDs {
			covered = covered && slices.Contains(h.GrantIDs, id)
		}
	}
	if h, p := a.PeerCommunication, input.Communication; h != nil && p != nil && h.Enabled && validityCovers(h.ExpiresAt, input.ExpiresAt, time.Now()) {
		covered = h.AllAssistants
		if !p.AllAssistants && len(p.AssistantIDs) > 0 {
			covered = true
			for _, id := range p.AssistantIDs {
				covered = covered && (h.AllAssistants || slices.Contains(h.AssistantIDs, id))
			}
		}
	}
	if covered {
		return &core.Error{Code: core.InvalidArgument, Field: "permission_type", Reason: "permission_already_granted", Message: "current delegation authorization covers this proposal; use it and refresh permission_list before continuing"}
	}
	return nil
}

func (s *permissionRequests) approveDelegation(ctx context.Context, p core.Principal, a assistant.Assistant, request policy.PermissionRequest, q policy.PermissionResolution) (*policy.PermissionRequestInput, error) {
	if s.assistants == nil {
		return nil, core.NewError(core.Unavailable, "assistant authorization service unavailable")
	}
	if q.AssistantVersion < 1 || q.AssistantVersion != a.Version {
		return nil, core.NewError(core.Conflict, "assistant authorization changed; reopen the review")
	}
	if len(q.Additional) > 0 || q.ResourceID != "" || q.GrantID != "" || q.GrantVersion != 0 || len(q.Actions) > 0 || q.Constraints != nil || q.Temporary != nil || q.Scope.ModelID != "" || len(q.Scope.PathPrefixes) > 0 || q.Scope.ReadOnly || q.Scope.FullAccess || q.Scope.Once || q.Scope.FullAccessScope != nil {
		return nil, core.Invalid("approval", "delegation approval does not accept resource grant fields")
	}
	approved := policy.PermissionRequestInput{PermissionType: request.Input.PermissionType, Reason: request.Input.Reason, ExpiresAt: q.ExpiresAt, Hiring: q.Hiring, Communication: q.Communication}
	if err := approved.Normalize(time.Now()); err != nil {
		return nil, err
	}
	mutation := core.Mutation{IdempotencyKey: "permission-request/" + string(request.ID), ExpectedVersion: q.AssistantVersion}
	if h := approved.Hiring; h != nil {
		updated, err := s.assistants.SaveHiring(ctx, p, assistant.HiringRequest{AssistantID: a.ID, Mutation: mutation, Authorization: assistant.HiringAuthorization{Enabled: true, MaxAssistants: h.MaxAssistants, Budget: h.Budget, GrantIDs: h.GrantIDs, ExpiresAt: q.ExpiresAt}})
		if err != nil {
			return nil, err
		}
		approved.Hiring = &policy.HiringPermissionProposal{MaxAssistants: updated.Hiring.MaxAssistants, Budget: updated.Hiring.Budget, GrantIDs: updated.Hiring.GrantIDs}
	} else {
		c := approved.Communication
		updated, err := s.assistants.SetPeerCommunication(ctx, p, assistant.PeerCommunicationRequest{AssistantID: a.ID, Mutation: mutation, Authorization: assistant.PeerCommunicationAuthorization{Enabled: true, AllAssistants: c.AllAssistants, AssistantIDs: c.AssistantIDs, ExpiresAt: q.ExpiresAt}})
		if err != nil {
			return nil, err
		}
		approved.Communication = &policy.CommunicationPermissionProposal{AllAssistants: updated.PeerCommunication.AllAssistants, AssistantIDs: updated.PeerCommunication.AssistantIDs}
	}
	return &approved, nil
}

type delegationPermissionState struct {
	Enabled       bool
	Active        bool
	Configuration *policy.PermissionRequestInput `json:",omitempty"`
}
type delegationPermissionSnapshot struct {
	AssistantVersion core.Version
	Hiring           delegationPermissionState
	Communication    delegationPermissionState
}

func currentDelegationPermissions(ctx context.Context, store persistence.Store, p core.Principal, now time.Time) (*delegationPermissionSnapshot, error) {
	a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, p.SubjectID)
	if persistence.Missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.EmployerID != "" {
		return nil, nil
	}
	out := &delegationPermissionSnapshot{AssistantVersion: a.Version}
	if h := a.Hiring; h != nil {
		out.Hiring = delegationPermissionState{Enabled: h.Enabled, Active: h.Enabled && (h.ExpiresAt == nil || h.ExpiresAt.After(now)), Configuration: &policy.PermissionRequestInput{PermissionType: "temporary_hiring", ExpiresAt: h.ExpiresAt, Hiring: &policy.HiringPermissionProposal{MaxAssistants: h.MaxAssistants, Budget: h.Budget, GrantIDs: h.GrantIDs}}}
	}
	if c := a.PeerCommunication; c != nil {
		out.Communication = delegationPermissionState{Enabled: c.Enabled, Active: c.Enabled && (c.ExpiresAt == nil || c.ExpiresAt.After(now)), Configuration: &policy.PermissionRequestInput{PermissionType: "assistant_communication", ExpiresAt: c.ExpiresAt, Communication: &policy.CommunicationPermissionProposal{AllAssistants: c.AllAssistants, AssistantIDs: c.AssistantIDs}}}
	}
	return out, nil
}
