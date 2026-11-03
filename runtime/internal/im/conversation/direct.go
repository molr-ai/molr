package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"slices"
)

func recipient(c Conversation, sender core.ID) Endpoint {
	if sender == c.OwnerID {
		return Endpoint{Type: Assistant, ID: c.PeerID}
	}
	if c.PartnerID == "" {
		return Endpoint{Type: User, ID: c.OwnerID}
	}
	if sender == c.PeerID {
		return Endpoint{Type: Assistant, ID: c.PartnerID}
	}
	return Endpoint{Type: Assistant, ID: c.PeerID}
}

// OpenDirect creates or finds the unique direct conversation. Authorization
// precedes creation; participant identities never come from a model's sender field.
func (s *service) OpenDirect(ctx context.Context, e core.Execution, target Endpoint) (out Conversation, err error) {
	if err = e.Validate(); err != nil {
		return
	}
	if target.ID.Validate() != nil || (target.Type != Assistant && target.Type != User) || target.ID == e.AssistantID {
		return out, core.Invalid("receiver", "a different user or assistant required")
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if s.options.Authorizer == nil {
			return core.NewError(core.Unauthenticated, "authorization unavailable")
		}
		if err := s.options.Authorizer.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: e.AssistantID}, Action: policy.Read}); err != nil {
			return err
		}
		if s.options.CheckDirect == nil {
			return core.NewError(core.Forbidden, "direct messaging unavailable")
		}
		if err := s.options.CheckDirect(ctx, e.Principal, target); err != nil {
			return err
		}
		if target.Type == User {
			var a struct{ ConversationID core.ID }
			if err := tx.Get(ctx, "business.assistants", persistence.Key(e.Principal.OwnerID, e.AssistantID), &a); err != nil {
				return err
			}
			out, err = s.conversation(ctx, tx, e.Principal, a.ConversationID, policy.Write)
			return err
		}
		for _, participant := range []core.ID{e.AssistantID, target.ID} {
			var a struct{ EmployerID, ConversationID core.ID }
			if err := tx.Get(ctx, "business.assistants", persistence.Key(e.Principal.OwnerID, participant), &a); err != nil {
				return err
			}
			if a.EmployerID != "" {
				out, err = s.conversation(ctx, tx, e.Principal, a.ConversationID, policy.Write)
				return err
			}
		}
		ids := []string{string(e.AssistantID), string(target.ID)}
		slices.Sort(ids)
		sum := sha256.Sum256([]byte(string(e.Principal.OwnerID) + "/direct/" + ids[0] + "/" + ids[1]))
		id := core.ID(hex.EncodeToString(sum[:]))
		out, err = getConversation(ctx, tx, e.Principal.OwnerID, id)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		out = Conversation{ID: id, OwnerID: e.Principal.OwnerID, PeerID: core.ID(ids[0]), PartnerID: core.ID(ids[1]), Version: 1}
		return s.repo.Insert(ctx, out)
	})
	return
}

// Collaborations returns owner-visible assistant conversation history. Reading
// this view never advances either assistant's model-input receipts.
func (s *service) Collaborations(ctx context.Context, p core.Principal, assistant core.ID, q core.PageRequest) (core.Page[Conversation], error) {
	if err := p.RequireOwner(); err != nil {
		return core.Page[Conversation]{}, err
	}
	if assistant.Validate() != nil {
		return core.Page[Conversation]{}, core.Invalid("assistant_id", "required")
	}
	if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: assistant}, Action: policy.Read}); err != nil {
		return core.Page[Conversation]{}, err
	}
	return persistence.Page[Conversation](ctx, s.store, conversationsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), {Field: "PartnerID", Op: "!=", Value: ""}}, Any: [][]persistence.Filter{{persistence.Eq("PeerID", assistant)}, {persistence.Eq("PartnerID", assistant)}}}, q, assistant, func(c Conversation) bool { return c.PartnerID != "" })
}
