package conversation

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// The adapter must bind Service, Repository and Outbox to one fresh database.
// Seed two owner-owned conversations, including Chat, and one foreign conversation.
// ObserveFeedback and VisibleCount read persisted state; FailNextCommit injects a
// storage failure, never a canned Service response.
type contractFixture struct {
	Service         Service
	Repository      Repository
	Outbox          Outbox
	Owner, Other    core.Principal
	Execution       core.Execution
	Chat            Conversation
	ObserveFeedback func(core.ID) Feedback
	VisibleCount    func() int
	FailNextCommit  func()
}

var newContractFixture func(*testing.T) contractFixture

func requireCode(t *testing.T, err error, codes ...core.Code) {
	t.Helper()
	for _, c := range codes {
		if core.CodeOf(err) == c {
			return
		}
	}
	t.Fatalf("error = %v (%s), want one of %v", err, core.CodeOf(err), codes)
}
func sendRequest(id core.ID, key, body string) SendUserRequest {
	return SendUserRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: id, Content: Content{Kind: Text, Text: body}}
}
func TestTDDSendUserPersistsOneMessageAndOutboxEvent(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	req := sendRequest(f.Chat.ID, "send-1", "hello")
	m, err := f.Service.SendUser(ctx, f.Owner, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.Service.SendUser(ctx, f.Owner, req)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || m.ID != again.ID || m.Sender != User || m.SenderID != f.Owner.SubjectID || m.Sequence == 0 {
		t.Fatalf("unexpected send/retry: %+v %+v", m, again)
	}
	page, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != m.ID {
		t.Fatalf("messages = %+v", page)
	}
	pending, err := f.Outbox.Pending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].MessageID != m.ID || pending[0].Sender != User || f.VisibleCount() != 1 {
		t.Fatalf("outbox = %+v, visible = %d", pending, f.VisibleCount())
	}
	req.Content.Text = "changed"
	_, err = f.Service.SendUser(ctx, f.Owner, req)
	requireCode(t, err, core.Conflict)
}
func TestTDDSendTransactionFailureLeavesNoPartialMessage(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	f.FailNextCommit()
	_, err := f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, "send-failed", "hello"))
	if err == nil {
		t.Fatal("injected commit failure ignored")
	}
	page, err := f.Repository.Messages(ctx, f.Owner.OwnerID, MessageQuery{ConversationID: f.Chat.ID})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.Outbox.Pending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || len(pending) != 0 || f.VisibleCount() != 0 {
		t.Fatal("partial transaction became visible")
	}
	_, err = f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, "send-failed", "hello"))
	if err != nil {
		t.Fatalf("failed transaction consumed idempotency key: %v", err)
	}
}
func TestTDDConversationAuthorizationAndArchive(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	page, err := f.Service.List(ctx, f.Owner, core.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].OwnerID != f.Owner.OwnerID || page.NextCursor == "" {
		t.Fatalf("owner page = %+v", page)
	}
	next, err := f.Service.List(ctx, f.Owner, core.PageRequest{Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.Items[0].OwnerID != f.Owner.OwnerID {
		t.Fatalf("next page = %+v", next)
	}
	c, err := f.Service.Get(ctx, f.Owner, f.Chat.ID)
	if err != nil || c.ID != f.Chat.ID {
		t.Fatalf("Get = %+v, %v", c, err)
	}
	_, err = f.Service.Get(ctx, f.Other, f.Chat.ID)
	requireCode(t, err, core.Forbidden, core.NotFound)
	_, err = f.Service.Messages(ctx, f.Other, MessageQuery{ConversationID: f.Chat.ID})
	requireCode(t, err, core.Forbidden, core.NotFound)
	_, err = f.Service.SendUser(ctx, f.Other, sendRequest(f.Chat.ID, "foreign", "bad"))
	requireCode(t, err, core.Forbidden, core.NotFound)
	if err = f.Repository.SetArchived(ctx, f.Owner.OwnerID, f.Chat.ID, true, f.Chat.Version); err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, "archived", "bad"))
	requireCode(t, err, core.FailedPrecondition)
	_, err = f.Service.Publish(ctx, f.Execution, PublishRequest{Mutation: core.Mutation{IdempotencyKey: "archived-publish"}, ConversationID: f.Chat.ID, Content: Content{Kind: Text, Text: "bad"}})
	requireCode(t, err, core.FailedPrecondition)
}
func TestTDDPublishReadFeedbackAndAcceptance(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	user, err := f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, "user", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Service.AcceptUserMessage(ctx, f.Execution, user.ID); err != nil {
		t.Fatal(err)
	}
	req := PublishRequest{Mutation: core.Mutation{IdempotencyKey: "reply"}, ConversationID: f.Chat.ID, Content: Content{Kind: Card, Card: &CardContent{Kind: TaskCard, TargetID: "task", Title: "Done"}}, Source: Source{OperationID: f.Execution.OperationID}}
	reply, err := f.Service.Publish(ctx, f.Execution, req)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := f.Service.Publish(ctx, f.Execution, req)
	if err != nil || repeat.ID != reply.ID {
		t.Fatalf("publish retry = %+v, %v", repeat, err)
	}
	if reply.Sender != Assistant || reply.SenderID != f.Execution.AssistantID || reply.Sequence <= user.Sequence {
		t.Fatalf("reply = %+v", reply)
	}
	if err = f.Service.MarkRead(ctx, f.Owner, ReadRequest{ConversationID: f.Chat.ID, ThroughSequence: reply.Sequence}); err != nil {
		t.Fatal(err)
	}
	if err = f.Service.MarkRead(ctx, f.Owner, ReadRequest{ConversationID: f.Chat.ID, ThroughSequence: user.Sequence}); err != nil {
		t.Fatal(err)
	}
	chat, err := f.Service.Get(ctx, f.Owner, f.Chat.ID)
	if err != nil || chat.LastReadSequence != reply.Sequence || chat.UnreadCount != 0 {
		t.Fatalf("read state = %+v, %v", chat, err)
	}
	page, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID})
	if err != nil {
		t.Fatal(err)
	}
	var accepted bool
	for _, m := range page.Items {
		if m.ID == user.ID {
			accepted = m.AcceptedAt != nil
		}
	}
	if !accepted {
		t.Fatal("acceptance was not persisted")
	}
	for i, value := range []Feedback{Like, Dislike, NoFeedback} {
		err = f.Service.SetFeedback(ctx, f.Owner, FeedbackRequest{Mutation: core.Mutation{IdempotencyKey: []string{"like", "dislike", "clear"}[i]}, MessageID: reply.ID, Value: value})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.ObserveFeedback(reply.ID); got != value {
			t.Fatalf("feedback = %s, want %s", got, value)
		}
		visible, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID})
		testkit.RequireNoError(t, err)
		for _, message := range visible.Items {
			if message.ID == reply.ID && message.Feedback != value {
				t.Fatalf("public feedback = %q, want %q", message.Feedback, value)
			}
		}
	}
	err = f.Service.MarkRead(ctx, f.Other, ReadRequest{ConversationID: f.Chat.ID, ThroughSequence: reply.Sequence})
	requireCode(t, err, core.Forbidden, core.NotFound)
	err = f.Service.SetFeedback(ctx, f.Other, FeedbackRequest{Mutation: core.Mutation{IdempotencyKey: "foreign-feedback"}, MessageID: reply.ID, Value: Like})
	requireCode(t, err, core.Forbidden, core.NotFound)
	foreign := f.Execution
	foreign.Principal.OwnerID = f.Other.OwnerID
	_, err = f.Service.Publish(ctx, foreign, req)
	requireCode(t, err, core.Forbidden, core.NotFound)
	err = f.Service.AcceptUserMessage(ctx, foreign, user.ID)
	requireCode(t, err, core.Forbidden, core.NotFound)
}
func TestTDDConversationRepositoryOwnershipVersionAndMessageState(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	r := f.Repository
	inserted := Conversation{ID: "extra-chat", OwnerID: f.Owner.OwnerID, PeerID: f.Execution.AssistantID, Version: 1}
	if err := r.Insert(ctx, inserted); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx, f.Owner.OwnerID, inserted.ID)
	if err != nil || got.ID != inserted.ID {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	if err = r.Insert(ctx, inserted); err == nil {
		t.Fatal("duplicate conversation accepted")
	}
	page, err := r.List(ctx, f.Owner.OwnerID, core.PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range page.Items {
		if c.OwnerID != f.Owner.OwnerID {
			t.Fatal("cross-owner list leak")
		}
		if c.ID == inserted.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("inserted conversation missing")
	}
	_, err = r.Get(ctx, f.Other.OwnerID, inserted.ID)
	requireCode(t, err, core.NotFound, core.Forbidden)
	if err = r.SetArchived(ctx, f.Owner.OwnerID, inserted.ID, true, 1); err != nil {
		t.Fatal(err)
	}
	err = r.SetArchived(ctx, f.Owner.OwnerID, inserted.ID, false, 1)
	requireCode(t, err, core.Conflict)
	got, err = r.Get(ctx, f.Owner.OwnerID, inserted.ID)
	if err != nil || !got.Archived || got.Version <= 1 {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	m, err := r.Append(ctx, f.Owner.OwnerID, Message{ID: "message-1", ConversationID: f.Chat.ID, Sender: User, SenderID: f.Owner.SubjectID, Content: Content{Kind: Text, Text: "hello"}, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if m.Sequence == 0 {
		t.Fatal("missing message sequence")
	}
	if err = r.MarkRead(ctx, f.Owner.OwnerID, ReadRequest{ConversationID: f.Chat.ID, ThroughSequence: m.Sequence}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err = r.AcceptUserMessage(ctx, f.Owner.OwnerID, m.ID, at); err != nil {
		t.Fatal(err)
	}
	if err = r.SetFeedback(ctx, f.Owner.OwnerID, FeedbackRequest{Mutation: core.Mutation{IdempotencyKey: "repo-like"}, MessageID: m.ID, Value: Like}); err != nil {
		t.Fatal(err)
	}
	if f.ObserveFeedback(m.ID) != Like {
		t.Fatal("repository feedback not persisted")
	}
	messages, err := r.Messages(ctx, f.Owner.OwnerID, MessageQuery{ConversationID: f.Chat.ID})
	if err != nil || len(messages.Items) != 1 || messages.Items[0].AcceptedAt == nil {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	_, err = r.Messages(ctx, f.Other.OwnerID, MessageQuery{ConversationID: f.Chat.ID})
	requireCode(t, err, core.NotFound, core.Forbidden)
}
func TestTDDOutboxPendingIsNonDestructiveAndAcknowledgementIsScoped(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	e := MessageCreated{ID: "outbox-1", OwnerID: f.Owner.OwnerID, ConversationID: f.Chat.ID, MessageID: "message", Sender: User, SenderID: f.Owner.SubjectID, At: time.Now()}
	if err := f.Outbox.Append(ctx, e); err != nil {
		t.Fatal(err)
	}
	a, err := f.Outbox.Pending(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Outbox.Pending(ctx, 1)
	if err != nil || len(a) != 1 || !reflect.DeepEqual(a, b) {
		t.Fatalf("pending consumed data: %+v %+v, %v", a, b, err)
	}
	if err = f.Outbox.Acknowledge(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	b, err = f.Outbox.Pending(ctx, 1)
	if err != nil || len(b) != 0 {
		t.Fatalf("ack not persisted: %+v, %v", b, err)
	}
}

func TestTDDConversationRepositoryRejectsCrossOwnerMutations(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	m, err := f.Repository.Append(ctx, f.Owner.OwnerID, Message{ID: "owned-message", ConversationID: f.Chat.ID, Sender: User, SenderID: f.Owner.SubjectID, Content: Content{Kind: Text, Text: "hello"}, CreatedAt: time.Now()})
	testkit.RequireNoError(t, err)
	requireCode(t, f.Repository.SetArchived(ctx, f.Other.OwnerID, f.Chat.ID, true, f.Chat.Version), core.Forbidden, core.NotFound)
	_, err = f.Repository.Append(ctx, f.Other.OwnerID, Message{ID: "foreign-append", ConversationID: f.Chat.ID, Sender: User, SenderID: f.Other.SubjectID, Content: Content{Kind: Text, Text: "bad"}, CreatedAt: time.Now()})
	requireCode(t, err, core.Forbidden, core.NotFound)
	requireCode(t, f.Repository.MarkRead(ctx, f.Other.OwnerID, ReadRequest{ConversationID: f.Chat.ID, ThroughSequence: m.Sequence}), core.Forbidden, core.NotFound)
	requireCode(t, f.Repository.AcceptUserMessage(ctx, f.Other.OwnerID, m.ID, time.Now()), core.Forbidden, core.NotFound)
	requireCode(t, f.Repository.SetFeedback(ctx, f.Other.OwnerID, FeedbackRequest{Mutation: core.Mutation{IdempotencyKey: "cross-owner"}, MessageID: m.ID, Value: Like}), core.Forbidden, core.NotFound)
	page, err := f.Repository.Messages(ctx, f.Owner.OwnerID, MessageQuery{ConversationID: f.Chat.ID})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].AcceptedAt != nil {
		t.Fatal("denied mutation changed message state")
	}
	chat, err := f.Repository.Get(ctx, f.Owner.OwnerID, f.Chat.ID)
	testkit.RequireNoError(t, err)
	if chat.Archived || chat.LastReadSequence != 0 {
		t.Fatal("denied mutation changed conversation state")
	}
}
func TestTDDConversationMessagePaginationAndCancellation(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	for _, key := range []string{"one", "two", "three"} {
		_, err := f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, key, key))
		testkit.RequireNoError(t, err)
	}
	first, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Page: core.PageRequest{Limit: 2}})
	testkit.RequireNoError(t, err)
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first messages page = %+v", first)
	}
	next, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Page: core.PageRequest{Limit: 2, Cursor: first.NextCursor}})
	testkit.RequireNoError(t, err)
	if len(next.Items) != 1 {
		t.Fatalf("next messages page = %+v", next)
	}
	for _, m := range first.Items {
		if next.Items[0].ID == m.ID {
			t.Fatal("pagination duplicated a message")
		}
	}
	latest, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Descending: true, Page: core.PageRequest{Limit: 2}})
	testkit.RequireNoError(t, err)
	if len(latest.Items) != 2 || latest.Items[0].Content.Text != "three" || latest.Items[1].Content.Text != "two" || latest.NextCursor == "" {
		t.Fatalf("latest page = %+v", latest)
	}
	older, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Descending: true, Page: core.PageRequest{Limit: 2, Cursor: latest.NextCursor}})
	testkit.RequireNoError(t, err)
	if len(older.Items) != 1 || older.Items[0].Content.Text != "one" || older.NextCursor != "" {
		t.Fatalf("older page = %+v", older)
	}
	_, err = f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Page: core.PageRequest{Cursor: latest.NextCursor}})
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, Page: core.PageRequest{Limit: -1}})
	testkit.RequireCode(t, err, core.InvalidArgument)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = f.Service.SendUser(canceled, f.Owner, sendRequest(f.Chat.ID, "canceled", "never committed"))
	testkit.RequireCode(t, err, core.Canceled)
	all, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID})
	testkit.RequireNoError(t, err)
	if len(all.Items) != 3 {
		t.Fatal("canceled send persisted a message")
	}
}

func TestMessageIncrementalSequenceAndExactLookup(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	var sent []Message
	for _, text := range []string{"one", "two", "three", "four", "five", "six"} {
		m, err := f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, text, text))
		testkit.RequireNoError(t, err)
		sent = append(sent, m)
	}
	q := MessageQuery{ConversationID: f.Chat.ID, AfterSequence: sent[1].Sequence, Page: core.PageRequest{Limit: 2}}
	first, err := f.Service.Messages(ctx, f.Owner, q)
	testkit.RequireNoError(t, err)
	if len(first.Items) != 2 || first.Items[0].ID != sent[2].ID || first.Items[1].ID != sent[3].ID || first.NextCursor == "" {
		t.Fatalf("unexpected first delta: %+v", first)
	}
	q.Page.Cursor = first.NextCursor
	second, err := f.Service.Messages(ctx, f.Owner, q)
	testkit.RequireNoError(t, err)
	if len(second.Items) != 2 || second.Items[0].ID != sent[4].ID || second.Items[1].ID != sent[5].ID || second.NextCursor != "" {
		t.Fatalf("unexpected second delta: %+v", second)
	}
	q.Page.Cursor = ""
	q.AfterSequence = sent[5].Sequence
	empty, err := f.Service.Messages(ctx, f.Owner, q)
	testkit.RequireNoError(t, err)
	if len(empty.Items) != 0 {
		t.Fatalf("latest message repeated: %+v", empty)
	}
	q.AfterSequence = sent[1].Sequence
	q.Descending = true
	descending, err := f.Service.Messages(ctx, f.Owner, q)
	testkit.RequireNoError(t, err)
	if len(descending.Items) != 2 || descending.Items[0].ID != sent[5].ID {
		t.Fatalf("unexpected descending delta: %+v", descending)
	}
	exact, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, MessageID: sent[0].ID})
	testkit.RequireNoError(t, err)
	if len(exact.Items) != 1 || exact.Items[0].ID != sent[0].ID {
		t.Fatalf("unexpected exact lookup: %+v", exact)
	}
	otherChat, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: "second", MessageID: sent[0].ID})
	testkit.RequireNoError(t, err)
	if len(otherChat.Items) != 0 {
		t.Fatal("exact lookup escaped conversation scope")
	}
	_, err = f.Service.Messages(ctx, f.Other, q)
	requireCode(t, err, core.NotFound, core.Forbidden)
}

func TestReadUserMessagesTracksDeliveryAndEnforcesOwnership(t *testing.T) {
	f := testkit.RequireFactory(t, "conversation", newContractFixture)
	ctx := testkit.Context(t)
	m, err := f.Service.SendUser(ctx, f.Owner, SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "read-delivery"}, ConversationID: f.Chat.ID, Content: Content{Kind: Text, Text: "input"}})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.AcceptUserMessage(ctx, f.Execution, m.ID))
	page, err := f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, MessageID: m.ID})
	testkit.RequireNoError(t, err)
	if page.Items[0].ReadAt != nil {
		t.Fatal("scheduler acceptance is not delivery")
	}
	other := f.Execution
	other.AssistantID = "second-assistant"
	other.Principal.SubjectID = "second-assistant"
	if err := f.Service.ReadUserMessages(ctx, other, []core.ID{m.ID}); err == nil {
		t.Fatal("another assistant marked message read")
	}
	testkit.RequireNoError(t, f.Service.ReadUserMessages(ctx, f.Execution, []core.ID{m.ID, m.ID}))
	page, err = f.Service.Messages(ctx, f.Owner, MessageQuery{ConversationID: f.Chat.ID, MessageID: m.ID})
	testkit.RequireNoError(t, err)
	if page.Items[0].ReadAt == nil {
		t.Fatal("delivery missing")
	}
	before := f.VisibleCount()
	testkit.RequireNoError(t, f.Service.ReadUserMessages(ctx, f.Execution, []core.ID{m.ID}))
	if f.VisibleCount() != before {
		t.Fatal("duplicate read notification")
	}
}

func TestLatestMessagesBatchesNewestAndEnforcesAccess(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := t.Context()
	for _, text := range []string{"old", "new"} {
		_, err := f.Service.SendUser(ctx, f.Owner, sendRequest(f.Chat.ID, text, text))
		testkit.RequireNoError(t, err)
	}
	latest, err := f.Service.LatestMessages(ctx, f.Owner, []core.ID{f.Chat.ID, "second", f.Chat.ID})
	testkit.RequireNoError(t, err)
	if len(latest) != 1 || latest[f.Chat.ID].Content.Text != "new" || latest[f.Chat.ID].Sequence != 2 {
		t.Fatal(latest)
	}
	_, err = f.Service.LatestMessages(ctx, f.Other, []core.ID{f.Chat.ID})
	if err == nil {
		t.Fatal("foreign conversation admitted")
	}
	_, err = f.Service.LatestMessages(ctx, f.Owner, []core.ID{f.Chat.ID, "missing"})
	if err == nil {
		t.Fatal("missing conversation admitted")
	}
	_, err = f.Service.LatestMessages(ctx, f.Owner, make([]core.ID, 201))
	if err == nil {
		t.Fatal("unbounded request admitted")
	}
	empty, err := f.Service.LatestMessages(ctx, f.Owner, nil)
	testkit.RequireNoError(t, err)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
}

func TestLatestMessagesFullPage(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ids := make([]core.ID, 200)
	for i := range ids {
		ids[i] = core.ID(fmt.Sprintf("batch-%03d", i))
		testkit.RequireNoError(t, f.Repository.Insert(t.Context(), Conversation{ID: ids[i], OwnerID: f.Owner.OwnerID, PeerID: core.ID("peer-" + string(ids[i])), Version: 1}))
		for j := 0; j < 2; j++ {
			_, err := f.Repository.Append(t.Context(), f.Owner.OwnerID, Message{ID: core.ID(fmt.Sprintf("%s-message-%d", ids[i], j)), ConversationID: ids[i], Sender: User, SenderID: f.Owner.OwnerID, Content: Content{Kind: Text, Text: fmt.Sprint(j)}, CreatedAt: time.Now()})
			testkit.RequireNoError(t, err)
		}
	}
	latest, err := f.Service.LatestMessages(t.Context(), f.Owner, ids)
	testkit.RequireNoError(t, err)
	if len(latest) != 200 {
		t.Fatal(len(latest))
	}
	for _, id := range ids {
		if latest[id].Sequence != 2 || latest[id].Content.Text != "1" {
			t.Fatal(id, latest[id])
		}
	}
}
