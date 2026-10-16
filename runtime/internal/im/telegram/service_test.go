package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

const testToken = "123456:synthetic-token-for-telegram-tests"

type testClient struct {
	updates                  []Update
	webhook                  bool
	sendError, errorDownload error
	sent                     []string
	downloads                int
	media                    []MediaFile
	onDownload               func()
	onSend                   func()
}

func (c *testClient) Me(context.Context, string) (User, error) {
	return User{ID: 123456, IsBot: true, Username: "fixture_bot"}, nil
}
func (c *testClient) Webhook(context.Context, string) (bool, error) { return c.webhook, nil }
func (c *testClient) Poll(ctx context.Context, _ string, _ int64, timeout int) ([]Update, error) {
	if timeout > 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.updates, nil
}
func (c *testClient) Send(_ context.Context, _ string, _ int64, text string) (int64, error) {
	c.sent = append(c.sent, text)
	if c.onSend != nil {
		c.onSend()
	}
	return 42, c.sendError
}
func (c *testClient) Download(context.Context, string, Media, int64) (MediaFile, error) {
	c.downloads++
	if c.onDownload != nil {
		c.onDownload()
	}
	return MediaFile{Name: "image.png", Type: "image/png", Data: []byte("synthetic media")}, c.errorDownload
}
func (c *testClient) SendMedia(_ context.Context, _ string, _ int64, file MediaFile) (int64, error) {
	c.media = append(c.media, file)
	return 43, c.sendError
}

type testAuthorizer struct{}

func (testAuthorizer) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}

type fixture struct {
	s      *Service
	db     *sqlitestore.Store
	owner  core.Principal
	chat   conversation.Service
	client *testClient
	live   bool
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	f := &fixture{db: db, owner: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, client: &testClient{}, live: true}
	f.s, err = New(db, Options{MasterKey: bytes.Repeat([]byte{1}, 32), Client: f.client, PublicURL: "https://molr.example.com", Authorize: func(_ context.Context, p core.Principal, id core.ID, _ bool) (Target, error) {
		if p != f.owner {
			return Target{}, core.NewError(core.Forbidden, "denied")
		}
		return Target{ConversationID: "chat", Active: f.live}, nil
	}, Live: func(_ context.Context, owner, id core.ID) error {
		if !f.live || owner != "owner" || id != "assistant" {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	}})
	must(t, err)
	objects, err := files.NewObjects(filepath.Join(t.TempDir(), "files"), db)
	must(t, err)
	t.Cleanup(func() { objects.Close() })
	fileService := files.NewService(db, objects, files.Options{Authorizer: testAuthorizer{}, AuthorizeChannel: f.s.AuthorizeChannel})
	f.s.options.Files = fileService.(files.ChannelStore)
	must(t, conversation.NewRepository(db).Insert(t.Context(), conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	f.chat = conversation.NewService(db, conversation.Options{Authorizer: testAuthorizer{}, CheckWebInput: f.s.CheckWebInput, AuthorizeChannel: f.s.AuthorizeChannel, ValidateChannelFile: f.s.options.Files.ValidateChannelFile, Published: f.s.Published})
	f.s.SetReceiver(f.chat.(Receiver))
	return f
}
func incoming(id int64, text string) Update {
	return Update{ID: id, Message: &Message{ID: id, From: &User{ID: 9007199254740000, FirstName: "Fixture"}, Chat: Chat{ID: 9007199254740000, Type: "private"}, Text: text}}
}
func (f *fixture) ready(t *testing.T) Login {
	t.Helper()
	l, err := f.s.StartLogin(t.Context(), f.owner, "assistant", testToken)
	must(t, err)
	f.client.updates = []Update{incoming(100, "/start "+f.s.logins[l.ID].Code)}
	l, err = f.s.PollLogin(t.Context(), f.owner, "assistant", l.ID)
	must(t, err)
	if l.Status != "ready" {
		t.Fatal(l)
	}
	return l
}
func (f *fixture) bind(t *testing.T) Binding {
	t.Helper()
	l := f.ready(t)
	must(t, f.s.Confirm(t.Context(), f.owner, "assistant", l.ID))
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}
func (f *fixture) publish(t *testing.T, text string, ids ...core.ID) {
	t.Helper()
	_, err := f.chat.Publish(t.Context(), core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: newID()}, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: string(newID())}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: text, AttachmentIDs: ids}})
	must(t, err)
}
func (f *fixture) deliveries(t *testing.T) []Delivery {
	t.Helper()
	rows, err := persistence.Select[Delivery](t.Context(), f.db, deliveriesBucket, persistence.Query{})
	must(t, err)
	sortDeliveries(rows)
	return rows
}
func TestBindingRequiresWebConfirmationAndLocksOnlyAfterCommit(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	l := f.ready(t)
	must(t, f.s.CheckWebInput(ctx, "owner", "chat"))
	forged := f.owner
	forged.SessionID = "forged"
	if f.s.Confirm(ctx, forged, "assistant", l.ID) == nil {
		t.Fatal("forged confirmation")
	}
	must(t, f.s.Confirm(ctx, f.owner, "assistant", l.ID))
	if f.s.Confirm(ctx, f.owner, "assistant", l.ID) == nil {
		t.Fatal("replayed confirmation")
	}
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	if b.UserID != 9007199254740000 || b.Offset != 101 {
		t.Fatal(b)
	}
	token, err := f.s.open(b)
	must(t, err)
	if token != testToken || bytes.Contains(b.Secrets, []byte(testToken)) {
		t.Fatal("credential storage")
	}
	b.OwnerID = "other"
	if _, err = f.s.open(b); err == nil {
		t.Fatal("credential substitution")
	}
	status, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte(testToken)) || bytes.Contains(raw, []byte("Secrets")) {
		t.Fatal("secret in status")
	}
	req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "web"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "hello"}}
	if _, err = f.chat.SendUser(ctx, f.owner, req); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	_, err = f.chat.SendUser(ctx, f.owner, req)
	must(t, err)
}
func TestBindingCancellationExpiryConflictAndWebhook(t *testing.T) {
	for _, kind := range []string{"cancel", "expire", "other_channel", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			l := f.ready(t)
			switch kind {
			case "cancel":
				must(t, f.s.CancelLogin(t.Context(), f.owner, "assistant", l.ID))
			case "expire":
				f.s.logins[l.ID].ExpiresAt = time.Now().Add(-time.Second)
			case "other_channel":
				f.s.options.CheckBinding = func(context.Context, core.ID, core.ID) error { return core.NewError(core.Conflict, "other channel") }
			case "revoked":
				f.live = false
			}
			if f.s.Confirm(t.Context(), f.owner, "assistant", l.ID) == nil {
				t.Fatal("late confirmation accepted")
			}
			bound, err := f.s.Connected(t.Context(), "owner", "chat")
			must(t, err)
			if bound {
				t.Fatal("bound")
			}
		})
	}
	f := setup(t)
	f.client.webhook = true
	if _, err := f.s.StartLogin(t.Context(), f.owner, "assistant", testToken); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	f.client.webhook = false
	f.bind(t)
	if _, err := f.s.StartLogin(t.Context(), f.owner, "assistant", testToken); err == nil {
		t.Fatal("bot reused")
	}
}
func TestInboundIdentityDedupMediaAndCursor(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	good := incoming(101, "hello")
	good.Message.Document = &Media{ID: "document", Name: "file.txt"}
	bad := incoming(102, "intruder")
	bad.Message.From.ID = 77
	bad.Message.Document = &Media{ID: "private-file"}
	group := incoming(103, "group")
	group.Message.Chat.Type = "group"
	topic := incoming(104, "topic")
	topic.Message.ThreadID = 2
	updates := []Update{good, good, bad, group, topic}
	must(t, f.s.ingest(ctx, b, testToken, updates))
	must(t, f.s.ingest(ctx, b, testToken, updates))
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items) != 1 || f.client.downloads != 1 || len(messages.Items[0].Content.AttachmentIDs) != 1 {
		t.Fatalf("messages=%+v downloads=%d", messages.Items, f.client.downloads)
	}
	current, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	if current.Offset != 105 {
		t.Fatal(current.Offset)
	}
	f.publish(t, "reply", messages.Items[0].Content.AttachmentIDs...)
	rows := f.deliveries(t)
	for _, d := range rows {
		f.s.deliver(ctx, d)
	}
	if len(f.client.sent) != 1 || len(f.client.media) != 1 {
		t.Fatal("attachment not delivered")
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	if err = f.s.ingest(ctx, b, testToken, []Update{incoming(106, "late")}); err == nil {
		t.Fatal("revoked ingress")
	}
}

type failingReceiver struct{}

func (failingReceiver) ReceiveChannel(context.Context, core.ID, core.ID, conversation.SendUserRequest) (conversation.Message, error) {
	return conversation.Message{}, errors.New("storage failed")
}
func TestCursorAndDedupRollBackWithMessage(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	f.s.SetReceiver(failingReceiver{})
	u := incoming(101, "persist me")
	if f.s.ingest(t.Context(), b, testToken, []Update{u}) == nil {
		t.Fatal("expected rollback")
	}
	current, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	if current.Offset != 101 {
		t.Fatal(current.Offset)
	}
	if _, err = persistence.Load[bool](t.Context(), f.db, incomingBucket, incomingKey(b, u)); !persistence.Missing(err) {
		t.Fatal("dedup committed")
	}
	f.s.SetReceiver(f.chat.(Receiver))
	must(t, f.s.ingest(t.Context(), b, testToken, []Update{u}))
}
func TestMediaFailureAndRevocationDuringDownload(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	u := incoming(101, "caption")
	u.Message.Photo = []Media{{ID: "photo"}}
	f.client.errorDownload = errors.New("failed")
	must(t, f.s.ingest(t.Context(), b, testToken, []Update{u}))
	messages, err := f.chat.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if !strings.Contains(messages.Items[0].Content.Text, "not been read") {
		t.Fatal("missing failure notice")
	}
	f.client.errorDownload = nil
	f.client.onDownload = func() { must(t, f.s.Disconnect(t.Context(), f.owner, "assistant")) }
	u.ID = 102
	if f.s.ingest(t.Context(), b, testToken, []Update{u}) == nil {
		t.Fatal("revoked media accepted")
	}
}
func TestDeliveryUnknownRateLimitRestartAndCancellation(t *testing.T) {
	for _, kind := range []string{"unknown", "rate_limited", "blocked", "token_invalid", "success"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			f.bind(t)
			f.publish(t, "hello")
			if kind != "success" {
				f.client.sendError = &apiError{Reason: kind, RetryAfter: 17}
			}
			d := f.deliveries(t)[0]
			f.s.deliver(t.Context(), d)
			d = f.deliveries(t)[0]
			switch kind {
			case "unknown":
				if d.State != "unknown" {
					t.Fatal(d)
				}
				f.s.deliver(t.Context(), d)
				if len(f.client.sent) != 1 {
					t.Fatal("unknown resent")
				}
			case "rate_limited":
				if d.State != "pending" || time.Until(d.NextAttempt) < 15*time.Second {
					t.Fatal(d)
				}
				f.s.deliver(t.Context(), d)
				if len(f.client.sent) != 1 {
					t.Fatal("ignored retry delay")
				}
			case "success":
				if d.State != "accepted" || d.TelegramMessageID != 42 {
					t.Fatal(d)
				}
			default:
				if d.State != "failed" {
					t.Fatal(d)
				}
			}
		})
	}
	f := setup(t)
	f.bind(t)
	f.publish(t, strings.Repeat("hello", 1000))
	rows := f.deliveries(t)
	rows[0].State = "sending"
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return f.s.saveDelivery(ctx, tx, rows[0]) }))
	must(t, f.s.recoverDeliveries(t.Context()))
	must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
	rows = f.deliveries(t)
	if rows[0].State != "unknown" || rows[1].State != "canceled" {
		t.Fatal(rows)
	}
}
func TestUnicodeChunksAndLifecycle(t *testing.T) {
	text := strings.Repeat("你好🧪", 3000)
	parts := splitText(text)
	if strings.Join(parts, "") != text {
		t.Fatal("text changed")
	}
	for _, p := range parts {
		if len(utf16.Encode([]rune(p))) > 4000 {
			t.Fatal("oversized")
		}
	}
	f := setup(t)
	f.bind(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil && core.CodeOf(err) != core.Canceled {
			t.Log(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestBotOwnershipPendingExpiryAndPausedDelivery(t *testing.T) {
	f := setup(t)
	l := f.ready(t)
	if _, err := f.s.StartLogin(t.Context(), f.owner, "assistant", testToken); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	f.s.logins[l.ID].ExpiresAt = time.Now().Add(-time.Second)
	f.s.expireLogins()
	if len(f.s.logins) != 0 {
		t.Fatal("expired token retained")
	}
	b := f.bind(t)
	alternate := f.owner
	alternate.OwnerID = "another"
	alternate.SubjectID = "another"
	authorize := f.s.options.Authorize
	f.s.options.Authorize = func(ctx context.Context, p core.Principal, id core.ID, write bool) (Target, error) {
		if p == alternate {
			return Target{ConversationID: "another-chat", Active: true}, nil
		}
		return authorize(ctx, p, id, write)
	}
	if _, err := f.s.StartLogin(t.Context(), alternate, "another-assistant", testToken); core.CodeOf(err) != core.Conflict {
		t.Fatal("cross-account bot reuse", err)
	}
	f.publish(t, "paused")
	f.live = false
	f.s.deliver(t.Context(), f.deliveries(t)[0])
	if len(f.client.sent) != 0 {
		t.Fatal("sent for inactive assistant")
	}
	if f.s.ingest(t.Context(), b, testToken, []Update{incoming(102, "paused")}) == nil {
		t.Fatal("inactive ingress")
	}
	f.live = true
	f.s.deliver(t.Context(), f.deliveries(t)[0])
	if len(f.client.sent) != 1 {
		t.Fatal("delivery did not resume")
	}
}
