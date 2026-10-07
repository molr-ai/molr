package conversation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

const receiptsBucket = "im.receipts"

type Options struct {
	// Channel hooks run inside the message transaction; they perform no network I/O.
	CheckWebInput       func(context.Context, core.ID, core.ID) error
	AuthorizeChannel    func(context.Context, core.ID, core.ID, core.ID) error
	ValidateChannelFile func(context.Context, core.ID, core.ID, core.ID, core.ID) error
	Published           func(context.Context, core.ID, Message) error
	Authorizer          policy.Authorizer
	Visible             realtime.Store
	Now                 func() time.Time
}
type service struct {
	store   persistence.Store
	repo    Repository
	outbox  Outbox
	options Options
}

// NewService never treats a syntactically valid Principal as authorization.
// Its authorizer must check live sessions/executions and grants. All related IM
// writes share the same transaction context, including nested module calls.
func NewService(store persistence.Store, options Options) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Visible == nil {
		options.Visible = realtime.NewStore(store)
	}
	return &service{store: store, repo: NewRepository(store), outbox: NewOutbox(store), options: options}
}
func (s *service) authorize(ctx context.Context, p core.Principal, action policy.Action, id core.ID) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authentication unavailable")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "conversation", ID: id}, Action: action})
}
func (s *service) conversation(ctx context.Context, tx persistence.Tx, p core.Principal, id core.ID, action policy.Action) (Conversation, error) {
	if err := s.authorize(ctx, p, action, id); err != nil {
		return Conversation{}, err
	}
	c, err := getConversation(ctx, tx, p.OwnerID, id)
	if err != nil {
		return c, err
	}
	if p.Kind == core.ActorAssistant && p.SubjectID != c.PeerID {
		return Conversation{}, core.NewError(core.Forbidden, "assistant is not conversation participant")
	}
	return c, nil
}
func (s *service) List(ctx context.Context, p core.Principal, q core.PageRequest) (result core.Page[Conversation], err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, policy.Read, p.OwnerID); err != nil {
			return err
		}
		result, err = s.repo.List(ctx, p.OwnerID, q)
		return err
	})
	return
}
func (s *service) Get(ctx context.Context, p core.Principal, id core.ID) (result Conversation, err error) {
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		result, err = s.conversation(ctx, tx, p, id, policy.Read)
		return err
	})
	return
}
func (s *service) Messages(ctx context.Context, p core.Principal, q MessageQuery) (result core.Page[Message], err error) {
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, err := s.conversation(ctx, tx, p, q.ConversationID, policy.Read); err != nil {
			return err
		}
		result, err = s.repo.Messages(ctx, p.OwnerID, q)
		return err
	})
	return
}
func randomID() (core.ID, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", core.Wrap(core.Internal, "ID generation failed", err)
	}
	return core.ID(hex.EncodeToString(data)), nil
}
func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type receipt struct {
	Digest  string
	Message *Message
}

func receiptKey(owner core.ID, action string, target core.ID, key string) string {
	return persistence.Key(owner, core.ID(digest([]string{action, string(target), key})))
}
func (s *service) checkContentAccess(ctx context.Context, p core.Principal, c Content, source Source) error {
	refs := append([]core.Object{}, source.References...)
	for _, id := range c.AttachmentIDs {
		refs = append(refs, core.Object{Kind: "file", ID: id})
	}
	if c.Card != nil {
		refs = append(refs, core.Object{Kind: string(c.Card.Kind), ID: c.Card.TargetID})
	}
	for _, ref := range refs {
		if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: ref, Action: policy.Read}); err != nil {
			return err
		}
	}
	return nil
}
func (s *service) SendUser(ctx context.Context, p core.Principal, req SendUserRequest) (Message, error) {
	if err := p.RequireOwner(); err != nil {
		return Message{}, err
	}
	if err := req.Validate(); err != nil {
		return Message{}, err
	}
	return s.send(ctx, p, req.Mutation, req.ConversationID, req.Content, Source{}, User)
}
func (s *service) Publish(ctx context.Context, execution core.Execution, req PublishRequest) (Message, error) {
	if err := req.Validate(execution); err != nil {
		return Message{}, err
	}
	req.Source.OperationID = execution.OperationID
	req.Source.ExecutionID = execution.Principal.ExecutionID
	return s.send(ctx, execution.Principal, req.Mutation, req.ConversationID, req.Content, req.Source, Assistant)
}
func (s *service) send(ctx context.Context, p core.Principal, mutation core.Mutation, chatID core.ID, content Content, source Source, sender Sender) (result Message, err error) {
	return s.sendFrom(ctx, p, mutation, chatID, content, source, sender, "")
}

// ReceiveChannel is a trusted internal ingress, deliberately absent from Client
// and the HTTP API. The binding is reauthorized inside the write transaction.
// Attachments must originate from this live binding; no owner session is acquired.
func (s *service) ReceiveChannel(ctx context.Context, owner, binding core.ID, req SendUserRequest) (Message, error) {
	if err := req.Validate(); err != nil {
		return Message{}, err
	}
	if owner.Validate() != nil || binding.Validate() != nil || req.Content.Card != nil {
		return Message{}, core.Invalid("channel", "invalid channel ingress")
	}
	return s.sendFrom(ctx, core.Principal{OwnerID: owner, SubjectID: owner, Kind: core.ActorOwner}, req.Mutation, req.ConversationID, req.Content, Source{}, User, binding)
}

func (s *service) sendFrom(ctx context.Context, p core.Principal, mutation core.Mutation, chatID core.ID, content Content, source Source, sender Sender, binding core.ID) (result Message, err error) {
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var c Conversation
		var err error
		if binding != "" {
			if s.options.AuthorizeChannel == nil {
				return core.NewError(core.Forbidden, "channel ingress unavailable")
			}
			if err = s.options.AuthorizeChannel(ctx, p.OwnerID, chatID, binding); err != nil {
				return err
			}
			for _, id := range content.AttachmentIDs {
				if s.options.ValidateChannelFile == nil {
					return core.NewError(core.Forbidden, "channel attachments unavailable")
				}
				if err := s.options.ValidateChannelFile(ctx, p.OwnerID, chatID, binding, id); err != nil {
					return err
				}
			}
			c, err = getConversation(ctx, tx, p.OwnerID, chatID)
		} else {
			c, err = s.conversation(ctx, tx, p, chatID, policy.Write)
		}
		if err != nil {
			return err
		}
		if sender == User && binding == "" && s.options.CheckWebInput != nil {
			if err = s.options.CheckWebInput(ctx, p.OwnerID, chatID); err != nil {
				return err
			}
		}
		if binding == "" {
			if err = s.checkContentAccess(ctx, p, content, source); err != nil {
				return err
			}
		}
		key := receiptKey(p.OwnerID, "send:"+string(sender), chatID, mutation.IdempotencyKey)
		payload := digest(struct {
			Content Content
			Source  Source
			Sender  core.ID
		}{content, source, p.SubjectID})
		var prior receipt
		err = tx.Get(ctx, receiptsBucket, key, &prior)
		if err == nil {
			if prior.Digest != payload {
				return core.NewError(core.Conflict, "idempotency key payload changed")
			}
			if prior.Message == nil {
				return core.NewError(core.Internal, "invalid send receipt")
			}
			result = *prior.Message
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if c.Archived {
			return core.NewError(core.FailedPrecondition, "conversation archived")
		}

		id, err := randomID()
		if err != nil {
			return err
		}
		m := Message{ID: id, ConversationID: chatID, Sender: sender, SenderID: p.SubjectID, Content: content, Source: source, CreatedAt: s.options.Now().UTC()}
		m, err = s.repo.Append(ctx, p.OwnerID, m)
		if err != nil {
			return err
		}
		eventID, err := randomID()
		if err != nil {
			return err
		}
		if err = s.outbox.Append(ctx, MessageCreated{Sequence: m.Sequence, ID: eventID, OwnerID: p.OwnerID, ConversationID: chatID, MessageID: m.ID, Sender: m.Sender, SenderID: m.SenderID, At: m.CreatedAt}); err != nil {
			return err
		}
		if err = s.visible(ctx, p.OwnerID, "message.created", core.Object{Kind: "message", ID: m.ID}, m.ConversationID); err != nil {
			return err
		}
		if sender == Assistant && s.options.Published != nil {
			if err = s.options.Published(ctx, p.OwnerID, m); err != nil {
				return err
			}
		}
		if err = tx.Put(ctx, receiptsBucket, key, receipt{Digest: payload, Message: &m}); err != nil {
			return err
		}
		result = m
		return nil
	})
	return
}
func (s *service) visible(ctx context.Context, owner core.ID, topic string, object core.Object, conversationID core.ID) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	return s.options.Visible.Append(ctx, realtime.Visible{ID: id, OwnerID: owner, Topic: topic, Object: object, ConversationID: conversationID, Version: 1, CreatedAt: s.options.Now().UTC()})
}
func (s *service) MarkRead(ctx context.Context, p core.Principal, req ReadRequest) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := s.conversation(ctx, tx, p, req.ConversationID, policy.Write)
		if err != nil {
			return err
		}
		if err = s.repo.MarkRead(ctx, p.OwnerID, req); err != nil {
			return err
		}
		if req.ThroughSequence <= c.LastReadSequence {
			return nil
		}
		return s.visible(ctx, p.OwnerID, "conversation.read", core.Object{Kind: "conversation", ID: c.ID}, c.ID)
	})
}
func (s *service) SetFeedback(ctx context.Context, p core.Principal, req FeedbackRequest) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := req.Mutation.Validate(false); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		m, err := getMessage(ctx, tx, p.OwnerID, req.MessageID)
		if err != nil {
			return err
		}
		if _, err = s.conversation(ctx, tx, p, m.ConversationID, policy.Write); err != nil {
			return err
		}
		key := receiptKey(p.OwnerID, "feedback", req.MessageID, req.Mutation.IdempotencyKey)
		payload := digest(req.Value)
		var prior receipt
		err = tx.Get(ctx, receiptsBucket, key, &prior)
		if err == nil {
			if prior.Digest != payload {
				return core.NewError(core.Conflict, "idempotency key payload changed")
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if err = s.repo.SetFeedback(ctx, p.OwnerID, req); err != nil {
			return err
		}
		if err = s.visible(ctx, p.OwnerID, "message.feedback", core.Object{Kind: "message", ID: m.ID}, m.ConversationID); err != nil {
			return err
		}
		return tx.Put(ctx, receiptsBucket, key, receipt{Digest: payload})
	})
}
func (s *service) AcceptUserMessage(ctx context.Context, execution core.Execution, id core.ID) error {
	if err := execution.Validate(); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		m, err := getMessage(ctx, tx, execution.Principal.OwnerID, id)
		if err != nil {
			return err
		}
		if _, err = s.conversation(ctx, tx, execution.Principal, m.ConversationID, policy.Write); err != nil {
			return err
		}
		if err = s.repo.AcceptUserMessage(ctx, execution.Principal.OwnerID, id, s.options.Now().UTC()); err != nil {
			return err
		}
		if m.AcceptedAt != nil {
			return nil
		}
		return s.visible(ctx, execution.Principal.OwnerID, "message.accepted", core.Object{Kind: "message", ID: id}, m.ConversationID)
	})
}

// ReadUserMessages acknowledges actual model-input delivery, independently of
// scheduler acceptance. The provider callback supplies only trusted input IDs.
func (s *service) ReadUserMessages(ctx context.Context, execution core.Execution, ids []core.ID) error {
	if err := execution.Validate(); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		seen := map[core.ID]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			m, err := getMessage(ctx, tx, execution.Principal.OwnerID, id)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			c, err := s.conversation(ctx, tx, execution.Principal, m.ConversationID, policy.Read)
			if err != nil {
				return err
			}
			if c.PeerID != execution.AssistantID || m.Sender != User {
				return core.NewError(core.Forbidden, "message is not this assistant's user input")
			}
			if m.ReadAt != nil {
				continue
			}
			at := s.options.Now().UTC()
			m.ReadAt = &at
			if err := tx.Put(ctx, messagesBucket, persistence.Key(execution.Principal.OwnerID, id), m); err != nil {
				return err
			}
			if err := s.visible(ctx, execution.Principal.OwnerID, "message.read", core.Object{Kind: "message", ID: id}, m.ConversationID); err != nil {
				return err
			}
		}
		return nil
	})
}
