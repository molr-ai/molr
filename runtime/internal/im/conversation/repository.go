package conversation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const conversationsBucket = "im.conversations"
const messagesBucket = "im.messages"
const feedbackBucket = "im.feedback"
const outboxBucket = "im.outbox"

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store: store} }

type pageCursor struct {
	Owner core.ID
	Scope string
	After string
}

func cursorValue(owner core.ID, scope, after string) string {
	data, _ := json.Marshal(pageCursor{Owner: owner, Scope: scope, After: after})
	return base64.RawURLEncoding.EncodeToString(data)
}
func readCursor(raw string, owner core.ID, scope string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", core.Invalid("cursor", "invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", core.Invalid("cursor", "invalid cursor")
	}
	var c pageCursor
	if json.Unmarshal(data, &c) != nil || c.Owner != owner || c.Scope != scope {
		return "", core.Invalid("cursor", "cursor does not match query")
	}
	return c.After, nil
}
func validIDs(ids ...core.ID) error {
	for _, id := range ids {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func getConversation(ctx context.Context, tx persistence.Tx, owner, id core.ID) (Conversation, error) {
	var c Conversation
	if err := validIDs(owner, id); err != nil {
		return c, err
	}
	err := tx.Get(ctx, conversationsBucket, persistence.Key(owner, id), &c)
	return c, err
}
func getMessage(ctx context.Context, tx persistence.Tx, owner, id core.ID) (Message, error) {
	var m Message
	if err := validIDs(owner, id); err != nil {
		return m, err
	}
	err := tx.Get(ctx, messagesBucket, persistence.Key(owner, id), &m)
	return m, err
}
func (r *repository) Get(ctx context.Context, owner, id core.ID) (c Conversation, err error) {
	err = r.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err = getConversation(ctx, tx, owner, id)
		return err
	})
	return
}
func (r *repository) List(ctx context.Context, owner core.ID, q core.PageRequest) (page core.Page[Conversation], err error) {
	if err = owner.Validate(); err != nil {
		return
	}
	q, err = q.Normalize()
	if err != nil {
		return
	}
	after, err := readCursor(q.Cursor, owner, "conversations")
	if err != nil {
		return page, err
	}
	err = r.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Query(ctx, conversationsBucket, persistence.Query{Prefix: string(owner) + "/", Where: []persistence.Filter{{Field: "key", Op: ">", Value: persistence.Key(owner, core.ID(after))}}, Limit: q.Limit + 1})
		if err != nil {
			return err
		}
		items := []Conversation{}
		for _, row := range rows {
			c, err := persistence.Decode[Conversation](row)
			if err != nil {
				return err
			}
			if c.OwnerID == owner && string(c.ID) > after {
				items = append(items, c)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		if len(items) > q.Limit {
			items = items[:q.Limit]
			page.NextCursor = cursorValue(owner, "conversations", string(items[len(items)-1].ID))
		}
		page.Items = items
		return nil
	})
	return
}
func (r *repository) Insert(ctx context.Context, c Conversation) error {
	if err := validIDs(c.ID, c.OwnerID, c.PeerID); err != nil {
		return err
	}
	if c.Version == 0 {
		return core.Invalid("version", "initial version required")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		_, err := getConversation(ctx, tx, c.OwnerID, c.ID)
		if err == nil {
			return core.NewError(core.Conflict, "conversation already exists")
		}
		if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, conversationsBucket, persistence.Key(c.OwnerID, c.ID), c)
	})
}
func (r *repository) SetArchived(ctx context.Context, owner, id core.ID, archive bool, version core.Version) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := getConversation(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if version == 0 || c.Version != version {
			return core.NewError(core.Conflict, "conversation version changed")
		}
		c.Archived = archive
		c.Version++
		return tx.Put(ctx, conversationsBucket, persistence.Key(owner, id), c)
	})
}
func (r *repository) Messages(ctx context.Context, owner core.ID, q MessageQuery) (page core.Page[Message], err error) {
	q.Page, err = q.Page.Normalize()
	if err != nil {
		return
	}
	scope := "messages:" + string(q.ConversationID)
	if q.Descending {
		scope += ":desc"
	}
	after, err := readCursor(q.Page.Cursor, owner, scope)
	if err != nil {
		return page, err
	}
	var from, to uint64
	if after != "" {
		from, err = strconv.ParseUint(after, 10, 64)
		if err != nil {
			return page, core.Invalid("cursor", "invalid message position")
		}
	}
	if q.Before != "" {
		before, err := readCursor(q.Before, owner, scope)
		if err != nil {
			return page, err
		}
		to, err = strconv.ParseUint(before, 10, 64)
		if err != nil {
			return page, core.Invalid("before", "invalid message position")
		}
	}
	err = r.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, err := getConversation(ctx, tx, owner, q.ConversationID); err != nil {
			return err
		}
		filters := []persistence.Filter{persistence.Eq("OwnerKey", owner), persistence.Eq("ConversationID", q.ConversationID), {Field: "Sequence", Op: ">", Value: q.AfterSequence}}
		if q.Descending {
			if from > 0 {
				filters = append(filters, persistence.Filter{Field: "Sequence", Op: "<", Value: from})
			}
		} else {
			filters = append(filters, persistence.Filter{Field: "Sequence", Op: ">", Value: from})
		}
		if to > 0 {
			filters = append(filters, persistence.Filter{Field: "Sequence", Op: "<", Value: to})
		}
		if q.MessageID != "" {
			filters = append(filters, persistence.Eq("ID", q.MessageID))
		}
		rows, err := tx.Query(ctx, messagesBucket, persistence.Query{Where: filters, OrderBy: "Sequence", Descending: q.Descending, Limit: q.Page.Limit + 1})
		if err != nil {
			return err
		}
		items := []Message{}
		for _, row := range rows {
			if len(row.Key) <= len(owner) || row.Key[:len(owner)+1] != string(owner)+"/" {
				continue
			}
			m, err := persistence.Decode[Message](row)
			if err != nil {
				return err
			}
			inRange := m.Sequence > from
			if q.Descending {
				inRange = from == 0 || m.Sequence < from
			}
			if m.ConversationID == q.ConversationID && (q.MessageID == "" || m.ID == q.MessageID) && inRange && m.Sequence > q.AfterSequence && (to == 0 || m.Sequence < to) {
				items = append(items, m)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if q.Descending {
				return items[i].Sequence > items[j].Sequence
			}
			return items[i].Sequence < items[j].Sequence
		})
		if len(items) > q.Page.Limit {
			items = items[:q.Page.Limit]
			page.NextCursor = cursorValue(owner, scope, strconv.FormatUint(items[len(items)-1].Sequence, 10))
		}
		for i := range items {
			value := NoFeedback
			if err := tx.Get(ctx, feedbackBucket, persistence.Key(owner, items[i].ID), &value); err != nil && !persistence.Missing(err) {
				return err
			}
			items[i].Feedback = value
		}
		page.Items = items
		return nil
	})
	return
}
func (r *repository) Append(ctx context.Context, owner core.ID, m Message) (result Message, err error) {
	if err = validIDs(m.ID, m.SenderID); err != nil {
		return
	}
	if err = m.Content.Validate(); err != nil {
		return
	}
	if m.CreatedAt.IsZero() {
		return result, core.Invalid("created_at", "required")
	}
	err = r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := getConversation(ctx, tx, owner, m.ConversationID)
		if err != nil {
			return err
		}
		if c.Archived {
			return core.NewError(core.FailedPrecondition, "conversation archived")
		}
		if (m.Sender == User && m.SenderID != owner) || (m.Sender == Assistant && m.SenderID != c.PeerID) || (m.Sender != User && m.Sender != Assistant) {
			return core.NewError(core.Forbidden, "sender is not a conversation participant")
		}
		if _, err = getMessage(ctx, tx, owner, m.ID); err == nil {
			return core.NewError(core.Conflict, "message already exists")
		}
		if !persistence.Missing(err) {
			return err
		}
		previous := c.LastSequence
		if c.LastMessageID != "" {
			last, err := getMessage(ctx, tx, owner, c.LastMessageID)
			if err != nil && (!persistence.Missing(err) || previous == 0) {
				return err
			}
			if last.Sequence > previous {
				previous = last.Sequence
			}
		}
		if previous == ^uint64(0) {
			return core.NewError(core.FailedPrecondition, "message sequence exhausted")
		}
		m.Sequence = previous + 1
		c.LastSequence = m.Sequence
		c.LastMessageID = m.ID
		if m.Sender == Assistant {
			c.UnreadCount++
		}
		if err = tx.Put(ctx, messagesBucket, persistence.Key(owner, m.ID), m); err != nil {
			return err
		}
		if err = tx.Put(ctx, conversationsBucket, persistence.Key(owner, c.ID), c); err != nil {
			return err
		}
		result = m
		return nil
	})
	return
}
func (r *repository) MarkRead(ctx context.Context, owner core.ID, req ReadRequest) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := getConversation(ctx, tx, owner, req.ConversationID)
		if err != nil {
			return err
		}
		last := c.LastSequence
		if c.LastMessageID != "" {
			m, err := getMessage(ctx, tx, owner, c.LastMessageID)
			if err != nil && (!persistence.Missing(err) || last == 0) {
				return err
			}
			if m.Sequence > last {
				last = m.Sequence
			}
		}
		if req.ThroughSequence > last {
			return core.Invalid("through_sequence", "sequence exceeds conversation history")
		}
		if req.ThroughSequence <= c.LastReadSequence {
			return nil
		}
		c.LastReadSequence = req.ThroughSequence
		unread, err := tx.Count(ctx, messagesBucket, persistence.Query{Where: []persistence.Filter{
			persistence.Eq("OwnerKey", owner), persistence.Eq("ConversationID", c.ID),
			{Field: "Sequence", Op: ">", Value: c.LastReadSequence}, persistence.Eq("Sender", Assistant),
		}})
		if err != nil {
			return err
		}
		c.UnreadCount = uint64(unread)
		return tx.Put(ctx, conversationsBucket, persistence.Key(owner, c.ID), c)
	})
}
func (r *repository) AcceptUserMessage(ctx context.Context, owner, id core.ID, at time.Time) error {
	if at.IsZero() {
		return core.Invalid("accepted_at", "required")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		m, err := getMessage(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if m.Sender != User {
			return core.Invalid("message", "only user messages can be accepted")
		}
		if m.AcceptedAt != nil {
			return nil
		}
		m.AcceptedAt = &at
		return tx.Put(ctx, messagesBucket, persistence.Key(owner, id), m)
	})
}
func (r *repository) SetFeedback(ctx context.Context, owner core.ID, req FeedbackRequest) error {
	if err := req.Mutation.Validate(false); err != nil {
		return err
	}
	if req.Value != Like && req.Value != Dislike && req.Value != NoFeedback {
		return core.Invalid("feedback", "unsupported feedback")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if _, err := getMessage(ctx, tx, owner, req.MessageID); err != nil {
			return err
		}
		return tx.Put(ctx, feedbackBucket, persistence.Key(owner, req.MessageID), req.Value)
	})
}
