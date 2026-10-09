// Package conversation stores interaction history; model context belongs to the agent package.
package conversation

import (
	"context"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

type Conversation struct {
	LastMessageID core.ID
	// LastSequence is a durable high-water mark; history cleanup never resets it.
	LastSequence     uint64
	UnreadCount      uint64
	LastReadSequence uint64
	ID               core.ID
	OwnerID          core.ID
	PeerID           core.ID
	Archived         bool
	Version          core.Version
}
type Sender string

const (
	User      Sender = "user"
	Assistant Sender = "assistant"
)

type ContentKind string

const (
	Text ContentKind = "text"
	Card ContentKind = "card"
)

// Cards are fixed product types, not executable HTML or arbitrary UI templates.
type CardKind string

const (
	PermissionRequestCard CardKind = "permission_request"
	TaskCard              CardKind = "task"
	ResourceCard          CardKind = "resource"
	ScheduleCard          CardKind = "schedule"
)

type CardContent struct {
	Kind     CardKind
	TargetID core.ID
	Title    string
	Summary  string
}
type Content struct {
	Kind          ContentKind
	Text          string
	Card          *CardContent
	AttachmentIDs []core.ID
}

func (c Content) Validate() error {
	if len(c.Text) > 65536 || len(c.AttachmentIDs) > 20 {
		return core.Invalid("content", "content limit exceeded")
	}
	for _, id := range c.AttachmentIDs {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	switch c.Kind {
	case Text:
		if c.Card != nil || (strings.TrimSpace(c.Text) == "" && len(c.AttachmentIDs) == 0) {
			return core.Invalid("content", "text or attachments required")
		}
	case Card:
		if c.Card == nil || c.Text != "" || c.Card.TargetID.Validate() != nil {
			return core.Invalid("card", "valid card target required")
		}
		switch c.Card.Kind {
		case TaskCard, ResourceCard, ScheduleCard, PermissionRequestCard:
		default:
			return core.Invalid("card_kind", "unsupported card")
		}
		if len(c.Card.Title) > 256 || len(c.Card.Summary) > 8192 {
			return core.Invalid("card", "card limit exceeded")
		}
	default:
		return core.Invalid("content_kind", "unsupported content")
	}
	return nil
}

type Source struct {
	ExecutionID core.ID `json:",omitempty"`
	References  []core.Object
	OperationID core.ID
}
type Message struct {
	Feedback       Feedback
	ID             core.ID
	ConversationID core.ID
	Sequence       uint64
	Sender         Sender
	SenderID       core.ID
	Content        Content
	Source         Source
	CreatedAt      time.Time
	AcceptedAt     *time.Time
	ReadAt         *time.Time
}
type SendUserRequest struct {
	Mutation       core.Mutation
	ConversationID core.ID
	Content        Content
}

func (r SendUserRequest) Validate() error {
	if err := r.Mutation.Validate(false); err != nil {
		return err
	}
	if err := r.ConversationID.Validate(); err != nil {
		return err
	}
	if r.Content.Kind != Text {
		return core.Invalid("content_kind", "user messages must be text or attachments")
	}
	return r.Content.Validate()
}

// PublishRequest is available only with a trusted assistant Execution; sender
// is not a request field. It emits visible events, never user input events.
type PublishRequest struct {
	Mutation       core.Mutation
	ConversationID core.ID
	Content        Content
	Source         Source
}
type MessageQuery struct {
	MessageID      core.ID // Optional exact message, still scoped to ConversationID.
	Descending     bool
	ConversationID core.ID
	Before         string
	AfterSequence  uint64 // Exclusive lower bound, independent of pagination direction.
	Page           core.PageRequest
}
type ReadRequest struct {
	ConversationID  core.ID
	ThroughSequence uint64
}
type Feedback string

const (
	Like       Feedback = "like"
	Dislike    Feedback = "dislike"
	NoFeedback Feedback = "none"
)

type FeedbackRequest struct {
	Mutation  core.Mutation
	MessageID core.ID
	Value     Feedback
}

// Service separates actual user input from explicit assistant publishing.
// SendUser and Publish persist message, IM outbox event, visible event and
// idempotency receipt in one transaction. IM never enqueues an Agent event.
// The Agent scheduler consumes IM events and wakes only for real user input.
type Client interface {
	List(context.Context, core.Principal, core.PageRequest) (core.Page[Conversation], error)
	Get(context.Context, core.Principal, core.ID) (Conversation, error)
	Messages(context.Context, core.Principal, MessageQuery) (core.Page[Message], error)
	LatestMessages(context.Context, core.Principal, []core.ID) (map[core.ID]Message, error)
	SendUser(context.Context, core.Principal, SendUserRequest) (Message, error)
	MarkRead(context.Context, core.Principal, ReadRequest) error
	SetFeedback(context.Context, core.Principal, FeedbackRequest) error
}
type Publisher interface {
	Publish(context.Context, core.Execution, PublishRequest) (Message, error)
	AcceptUserMessage(context.Context, core.Execution, core.ID) error
	ReadUserMessages(context.Context, core.Execution, []core.ID) error
}
type Service interface {
	Client
	Publisher
}
type Repository interface {
	List(context.Context, core.ID, core.PageRequest) (core.Page[Conversation], error)
	Get(context.Context, core.ID, core.ID) (Conversation, error)
	Insert(context.Context, Conversation) error
	SetArchived(context.Context, core.ID, core.ID, bool, core.Version) error
	Messages(context.Context, core.ID, MessageQuery) (core.Page[Message], error)
	Append(context.Context, core.ID, Message) (Message, error)
	MarkRead(context.Context, core.ID, ReadRequest) error
	AcceptUserMessage(context.Context, core.ID, core.ID, time.Time) error
	SetFeedback(context.Context, core.ID, FeedbackRequest) error
}
