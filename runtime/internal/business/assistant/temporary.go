package assistant

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"slices"
	"time"
)

// HiringAuthorization is a preauthorized, shared allowance. Its ID survives
// edits and revocation so dismissing children never resets accumulated spend.
type HiringAuthorization struct {
	ID            core.ID
	Enabled       bool
	MaxAssistants int
	Budget        core.Money
	GrantIDs      []core.ID
	ExpiresAt     *time.Time
}
type GrantReference struct {
	ID      core.ID
	Version core.Version
}
type HiringRequest struct {
	Mutation      core.Mutation
	AssistantID   core.ID
	Authorization HiringAuthorization
}
type TemporaryRequest struct {
	Mutation     core.Mutation
	Name         string
	Instructions string
	ModelID      core.ID
	GrantIDs     []core.ID
}
type DirectoryEntry struct {
	Version           core.Version
	AssistantID       core.ID
	Name, Description string
	State             State
	EmployerID        core.ID
}

func (s *service) SaveHiring(ctx context.Context, p core.Principal, r HiringRequest) (Assistant, error) {
	return mutate(ctx, s, p, "hiring", r.AssistantID, r.Mutation, r, true, true, func(ctx context.Context) (Assistant, error) {
		a, err := s.repo.Get(ctx, p.OwnerID, r.AssistantID)
		if err != nil {
			return a, err
		}
		if a.Version != r.Mutation.ExpectedVersion {
			return a, conflict()
		}
		if a.EmployerID != "" || a.State != Active {
			return a, forbidden()
		}
		h := r.Authorization
		if h.MaxAssistants < 1 || h.MaxAssistants > 32 || h.Budget.Validate() != nil || h.Budget.Micros <= 0 || (h.Enabled && h.ExpiresAt != nil && !h.ExpiresAt.After(s.now())) || len(h.GrantIDs) > 64 {
			return a, invalid("authorization")
		}
		grants, err := policy.ActiveGrants(ctx, s.store, p.OwnerID, policy.Query{AssistantID: a.ID}, s.now())
		if err != nil {
			return a, err
		}
		for _, id := range h.GrantIDs {
			if !h.Enabled {
				break
			}
			i := slices.IndexFunc(grants, func(g policy.Grant) bool { return g.ID == id })
			if i < 0 || grants[i].Scope.FullAccess || grants[i].Scope.Once {
				return a, invalid("grant_ids")
			}
			// Temporary workers cannot contact external messaging services. Arbitrary
			// extensions and network grants are deliberately not delegated.
			res, err := resource.NewRepository(s.store).Get(ctx, p.OwnerID, grants[i].ResourceID)
			if err != nil {
				return a, err
			}
			if res.Kind != resource.Model && res.Kind != resource.Storage && res.Kind != resource.Compute {
				return a, core.Invalid("grant_ids", "temporary assistants support model, storage and isolated compute grants")
			}
		}
		h.ID = s.id()
		if a.Hiring != nil {
			h.ID = a.Hiring.ID
		}
		a.Hiring = &h
		old := a.Version
		a.Version++
		a.UpdatedAt = s.now()
		if err := s.repo.Save(ctx, a, old); err != nil {
			return a, err
		}
		if !h.Enabled {
			if err := s.endEmployees(ctx, a.OwnerID, a.ID); err != nil {
				return a, err
			}
		}
		return a, s.profileChanged(ctx, a)
	})
}

func (s *service) Directory(ctx context.Context, p core.Principal) ([]DirectoryEntry, error) {
	if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "assistant_list"}, Action: policy.Execute}); err != nil {
		return nil, err
	}
	current, err := s.repo.Get(ctx, p.OwnerID, p.SubjectID)
	if err != nil {
		return nil, err
	}
	filters := []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("State", Active)}
	var any [][]persistence.Filter
	if current.EmployerID != "" {
		filters = append(filters, persistence.Eq("ID", current.EmployerID))
	} else if current.PeerCommunication == nil || !current.PeerCommunication.Enabled || (current.PeerCommunication.ExpiresAt != nil && !current.PeerCommunication.ExpiresAt.After(s.now())) {
		filters = append(filters, persistence.Eq("EmployerID", current.ID))
	} else {
		any = [][]persistence.Filter{{persistence.Eq("EmployerID", current.ID)}, {persistence.Eq("PeerCommunication.Enabled", true)}}
	}
	rows, err := persistence.Select[Assistant](ctx, s.store, bucket, persistence.Query{Where: filters, Any: any, Limit: 200})
	out := []DirectoryEntry{}
	for _, a := range rows {
		if !CanContact(current, a, s.now()) {
			continue
		}
		out = append(out, DirectoryEntry{a.Version, a.ID, a.Name, a.Description, a.State, a.EmployerID})
	}
	return out, err
}

func (s *service) HireTemporary(ctx context.Context, e core.Execution, r TemporaryRequest) (Assistant, error) {
	if err := e.Validate(); err != nil {
		return Assistant{}, err
	}
	return mutate(ctx, s, e.Principal, "hire-temporary", e.AssistantID, r.Mutation, r, false, false, func(ctx context.Context) (Assistant, error) {
		parent, err := s.repo.Get(ctx, e.Principal.OwnerID, e.AssistantID)
		if err != nil {
			return Assistant{}, err
		}
		h := parent.Hiring
		if parent.EmployerID != "" || parent.State != Active || h == nil || !h.Enabled || (h.ExpiresAt != nil && !h.ExpiresAt.After(s.now())) {
			return Assistant{}, core.NewError(core.Forbidden, "temporary hiring is not authorized")
		}
		if !validText(r.Instructions, 65536) {
			return Assistant{}, invalid("instructions")
		}
		name, err := NormalizeName(r.Name)
		if err != nil {
			return Assistant{}, err
		}
		children, err := persistence.Select[Assistant](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", parent.OwnerID), persistence.Eq("EmployerID", parent.ID), persistence.Eq("State", Active)}})
		if err != nil {
			return Assistant{}, err
		}
		if len(children) >= h.MaxAssistants {
			return Assistant{}, core.NewError(core.FailedPrecondition, "temporary assistant limit reached; dismiss an existing worker first")
		}
		selected := r.GrantIDs
		if selected == nil {
			selected = h.GrantIDs
		}
		modelID := r.ModelID
		if modelID == "" {
			modelID = parent.ModelID
		}
		live, err := policy.ActiveGrants(ctx, s.store, parent.OwnerID, policy.Query{AssistantID: parent.ID}, s.now())
		if err != nil {
			return Assistant{}, err
		}
		refs := []GrantReference{}
		modelAllowed := false
		for _, id := range selected {
			i := slices.IndexFunc(live, func(g policy.Grant) bool { return g.ID == id })
			if !slices.Contains(h.GrantIDs, id) || i < 0 {
				return Assistant{}, core.NewError(core.Forbidden, "selected grant is outside hiring authorization")
			}
			g := live[i]
			if g.Scope.FullAccess || g.Scope.Once {
				return Assistant{}, forbidden()
			}
			if g.Scope.ModelID == modelID && slices.Contains(g.Actions, policy.Execute) {
				modelAllowed = true
			}
			refs = append(refs, GrantReference{g.ID, g.Version})
		}
		if !modelAllowed {
			return Assistant{}, core.NewError(core.Forbidden, "selected model is not preauthorized")
		}
		a := Assistant{ID: s.id(), OwnerID: parent.OwnerID, Name: name, Description: r.Instructions, EmployerID: parent.ID, HiringID: h.ID, AttachedGrants: refs, ExpiresAt: h.ExpiresAt, State: Active, ModelID: modelID, Budget: core.AssistantBudget{Limit: h.Budget, Period: "daily"}, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		a.ConversationID = s.id()
		if err := s.repo.Insert(ctx, a); err != nil {
			return a, err
		}
		if err := s.options.Conversations.Insert(ctx, conversation.Conversation{ID: a.ConversationID, OwnerID: a.OwnerID, PeerID: parent.ID, PartnerID: a.ID, Version: 1}); err != nil {
			return a, err
		}
		// Lifecycle is a trusted, transaction-bound port, not a user API.
		if s.options.TemporaryLoop == nil {
			return a, core.NewError(core.Unavailable, "temporary lifecycle unavailable")
		}
		if err := s.options.TemporaryLoop(ctx, a.OwnerID, a.ID, "initialize"); err != nil {
			return a, err
		}
		msg, err := s.options.Conversations.Append(ctx, a.OwnerID, conversation.Message{ID: s.id(), ConversationID: a.ConversationID, Sender: conversation.Assistant, SenderID: parent.ID, Content: conversation.Content{Kind: conversation.Text, Text: r.Instructions}, CreatedAt: s.now()})
		if err != nil {
			return a, err
		}
		if err := s.profileChanged(ctx, a); err != nil {
			return a, err
		}
		if err := realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: s.id(), OwnerID: a.OwnerID, Topic: "message.created", Object: core.Object{Kind: "message", ID: msg.ID}, ConversationID: msg.ConversationID, Version: 1, CreatedAt: msg.CreatedAt}); err != nil {
			return a, err
		}
		return a, event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: s.id(), OwnerID: a.OwnerID, AssistantID: a.ID, Kind: event.AssistantMessage, ConversationID: a.ConversationID, MessageID: msg.ID, DeliveryMode: event.WhenIdle, CreatedAt: msg.CreatedAt})
	})
}

func (s *service) EndTemporary(ctx context.Context, p core.Principal, r LifecycleRequest) (Assistant, error) {
	if err := r.Mutation.Validate(true); err != nil {
		return Assistant{}, err
	}
	var out Assistant
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.repo.Get(ctx, p.OwnerID, r.ID)
		if err != nil {
			return err
		}
		if a.EmployerID == "" {
			return forbidden()
		}
		target := a.ID
		if p.Kind == core.ActorAssistant {
			if a.EmployerID != p.SubjectID {
				return forbidden()
			}
			target = p.SubjectID
		}
		if err := s.authorize(ctx, p, core.Object{Kind: "assistant", ID: target}, policy.Write, false); err != nil {
			return err
		}
		if a.State == Dismissed {
			out = a
			return nil
		}
		if a.Version != r.Mutation.ExpectedVersion {
			return conflict()
		}
		out, err = s.endTemporary(ctx, a)
		return err
	})
	return out, err
}
func (s *service) endTemporary(ctx context.Context, a Assistant) (Assistant, error) {
	if s.options.TemporaryLoop == nil {
		return a, core.NewError(core.Unavailable, "temporary lifecycle unavailable")
	}
	if err := s.options.TemporaryLoop(ctx, a.OwnerID, a.ID, "stop"); err != nil {
		return a, err
	}
	c, err := s.options.Conversations.Get(ctx, a.OwnerID, a.ConversationID)
	if err != nil {
		return a, err
	}
	if !c.Archived {
		if err := s.options.Conversations.SetArchived(ctx, a.OwnerID, c.ID, true, c.Version); err != nil {
			return a, err
		}
	}
	old := a.Version
	a.Version++
	a.State = Dismissed
	a.UpdatedAt = s.now()
	if err := s.repo.Save(ctx, a, old); err != nil {
		return a, err
	}
	return a, s.profileChanged(ctx, a)
}

// ExpireTemporary runs only on committed changes or the nearest expiry boundary.
func (s *service) ExpireTemporary(ctx context.Context, now time.Time) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Select[Assistant](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", Active), {Field: "EmployerID", Op: "!=", Value: ""}}})
		if err != nil {
			return err
		}
		for _, a := range rows {
			if a.EmployerID == "" {
				continue
			}
			parent, err := s.repo.Get(ctx, a.OwnerID, a.EmployerID)
			if err != nil {
				return err
			}
			if (a.ExpiresAt != nil && !a.ExpiresAt.After(now)) || parent.State != Active || parent.Hiring == nil || !parent.Hiring.Enabled || parent.Hiring.ID != a.HiringID || (parent.Hiring.ExpiresAt != nil && !parent.Hiring.ExpiresAt.After(now)) {
				if _, err := s.endTemporary(ctx, a); err != nil {
					return err
				}
				if parent.State == Active {
					details, _ := json.Marshal(map[string]any{"AssistantID": a.ID, "Reason": "temporary_employment_ended"})
					if err := event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: s.id(), OwnerID: a.OwnerID, AssistantID: parent.ID, Kind: event.AssistantNotice, Details: details, CreatedAt: now, DeliveryMode: event.WhenIdle}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (s *service) endEmployees(ctx context.Context, owner, employer core.ID) error {
	rows, err := persistence.Select[Assistant](ctx, s.store, bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("EmployerID", employer), persistence.Eq("State", Active)}})
	if err != nil {
		return err
	}
	for _, a := range rows {
		if _, err := s.endTemporary(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
