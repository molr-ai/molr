package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/notification"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

type testClient struct {
	result       LoginResult
	sent         []string
	sendError    error
	serverID     string
	qrTokens     []string
	checkError   error
	mediaError   error
	downloads    int
	mediaSent    []MediaFile
	onSend       func()
	loginWait    chan struct{}
	loginStarted chan struct{}
}

func (c *testClient) QR(_ context.Context, tokens []string) (QR, error) {
	c.qrTokens = append([]string(nil), tokens...)
	return QR{Code: "qr-token", Content: "https://weixin.qq.com/qr/test"}, nil
}
func (c *testClient) Login(ctx context.Context, _ string, _ string, _ string) (LoginResult, error) {
	if c.loginWait != nil {
		close(c.loginStarted)
		select {
		case <-ctx.Done():
			return LoginResult{}, ctx.Err()
		case <-c.loginWait:
		}
	}
	return c.result, nil
}
func (c *testClient) Poll(ctx context.Context, _ string, _ string, _ string) (Updates, error) {
	<-ctx.Done()
	return Updates{}, ctx.Err()
}
func (c *testClient) Send(_ context.Context, _ string, _ string, _ string, _ string, id, text string) (string, error) {
	c.sent = append(c.sent, id+":"+text)
	if c.onSend != nil {
		c.onSend()
	}
	return c.serverID, c.sendError
}

func (c *testClient) Download(_ context.Context, item Item, _ int64) (MediaFile, error) {
	c.downloads++
	if item.File.Name == "bad" {
		return MediaFile{}, errors.New("download failed")
	}
	return MediaFile{Name: "photo.png", Type: "image/png", Data: []byte("image bytes")}, nil
}
func (c *testClient) SendMedia(_ context.Context, _, _, _, _, _ string, file MediaFile) (string, error) {
	c.mediaSent = append(c.mediaSent, file)
	return c.serverID, c.mediaError
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
	f := &fixture{db: db, owner: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, live: true, client: &testClient{result: LoginResult{Status: "confirmed", Token: "SECRET-BOT-TOKEN", BotID: "bot", UserID: "user@im.wechat", BaseURL: baseURL}}}
	s, err := New(db, Options{MasterKey: bytes.Repeat([]byte{1}, 32), Client: f.client, PublicURL: "https://molr.example",
		Authorize: func(_ context.Context, p core.Principal, id core.ID, _ bool) (Target, error) {
			if p != f.owner {
				return Target{}, core.NewError(core.Forbidden, "denied")
			}
			return Target{ConversationID: "chat", Active: true}, nil
		},
		Live: func(_ context.Context, owner, id core.ID) error {
			if !f.live || owner != "owner" || id != "assistant" {
				return core.NewError(core.Forbidden, "revoked")
			}
			return nil
		},
	})
	must(t, err)
	f.s = s
	objects, err := files.NewObjects(filepath.Join(t.TempDir(), "files"), db)
	must(t, err)
	t.Cleanup(func() { objects.Close() })
	fileService := files.NewService(db, objects, files.Options{Authorizer: testAuthorizer{}, AuthorizeChannel: s.AuthorizeChannel})
	s.options.Files = fileService.(files.ChannelStore)
	must(t, conversation.NewRepository(db).Insert(t.Context(), conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	f.chat = conversation.NewService(db, conversation.Options{Authorizer: testAuthorizer{}, CheckWebInput: s.CheckWebInput, AuthorizeChannel: s.AuthorizeChannel, ValidateChannelFile: s.options.Files.ValidateChannelFile, Published: s.Published})
	s.SetReceiver(f.chat.(Receiver))
	return f
}
func (f *fixture) bind(t *testing.T) Binding {
	t.Helper()
	l, err := f.s.StartLogin(t.Context(), f.owner, "assistant")
	must(t, err)
	_, err = f.s.PollLogin(t.Context(), f.owner, "assistant", PollRequest{ID: l.ID})
	must(t, err)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}
func incoming(id, text string) Incoming {
	var m Incoming
	m.ID = json.RawMessage(id)
	m.Type = 1
	m.From = "user@im.wechat"
	m.Context = "SECRET-CONTEXT"
	var item Item
	item.Type = 1
	item.Text.Text = text
	m.Items = []Item{item}
	return m
}
func (f *fixture) publish(t *testing.T, key, text string) conversation.Message {
	t.Helper()
	m, err := f.chat.Publish(t.Context(), core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: core.ID(key)}, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: text}})
	must(t, err)
	return m
}
func TestBindSwitchIngressAndDisconnect(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	web := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "web"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "before"}}
	_, err := f.chat.SendUser(ctx, f.owner, web)
	must(t, err)
	l, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	status, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if status.Channel != "webui" {
		t.Fatal("pending login switched channel")
	}
	f.client.result.Status = "wait"
	_, err = f.s.PollLogin(ctx, f.owner, "assistant", PollRequest{ID: l.ID})
	must(t, err)
	f.client.result.Status = "confirmed"
	_, err = f.s.PollLogin(ctx, f.owner, "assistant", PollRequest{ID: l.ID})
	must(t, err)
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	_, err = f.chat.SendUser(ctx, f.owner, web)
	if core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("web input accepted: %v", err)
	}
	wrong := incoming("2", "wrong sender")
	wrong.From = "other"
	group := incoming("3", "group")
	group.Group = "group"
	bot := incoming("4", "echo")
	bot.Type = 2
	r := Updates{Cursor: "CURSOR", Messages: []Incoming{incoming("18446744073709551615", "hello"), wrong, group, bot}}
	must(t, f.s.ingest(ctx, b, r))
	must(t, f.s.ingest(ctx, b, r))
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items) != 2 {
		t.Fatalf("messages: %+v", messages.Items)
	}
	f.publish(t, "reply", "answer")
	f.s.deliverNext(ctx)
	if len(f.client.sent) != 1 {
		t.Fatal("reply not sent")
	}
	status, err = f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if status.Deliveries[0].State != "accepted" {
		t.Fatal(status)
	}
	f.publish(t, "unsent", "pending")
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	f.s.deliverNext(ctx)
	if len(f.client.sent) != 1 {
		t.Fatal("sent after disconnect")
	}
	_, err = f.chat.(Receiver).ReceiveChannel(ctx, "owner", b.ID, web)
	if err == nil {
		t.Fatal("revoked binding accepted")
	}
	web.Mutation.IdempotencyKey = "restored"
	_, err = f.chat.SendUser(ctx, f.owner, web)
	must(t, err)
	status, err = f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if status.Channel != "webui" {
		t.Fatal(status)
	}
}
func TestIngressAndCursorRollbackTogether(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	f.db.FailNextCommit(errors.New("disk unavailable"))
	if err := f.s.ingest(t.Context(), b, Updates{Cursor: "next", Messages: []Incoming{incoming("1", "hello")}}); err == nil {
		t.Fatal("expected commit failure")
	}
	current, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	secret, err := f.s.open(current)
	must(t, err)
	if secret.Cursor != "" {
		t.Fatal("cursor advanced before durable message")
	}
	page, err := f.chat.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(page.Items) != 0 {
		t.Fatal("message survived failed commit")
	}
	must(t, f.s.ingest(t.Context(), b, Updates{Cursor: "next", Messages: []Incoming{incoming("1", "hello")}}))
	current, err = f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	raw, _ := json.Marshal(current)
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("plaintext credentials in storage")
	}
}
func TestUnknownNotRetriedAndContextRecovery(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	f.publish(t, "reply", "answer")
	f.s.deliverNext(t.Context())
	if len(f.client.sent) != 0 {
		t.Fatal("sent without context")
	}
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{incoming("1", "hello")}}))
	f.client.sendError = &apiError{Code: -2}
	f.s.deliverNext(t.Context())
	f.s.deliverNext(t.Context())
	if len(f.client.sent) != 1 {
		t.Fatal("retried without new context")
	}
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{incoming("2", "hello again")}}))
	f.client.sendError = errors.New("timeout")
	f.s.deliverNext(t.Context())
	f.s.deliverNext(t.Context())
	if len(f.client.sent) != 2 {
		t.Fatal("unknown send retried")
	}
	st, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if st.Deliveries[0].State != "unknown" {
		t.Fatal(st)
	}
	f.live = false
	f.publish(t, "later", "should not send")
	f.s.deliverNext(t.Context())
	if len(f.client.sent) != 2 {
		t.Fatal("sent for disabled account")
	}
}
func TestPublishAndDeliveryCommitTogether(t *testing.T) {
	f := setup(t)
	f.bind(t)
	f.db.FailNextCommit(errors.New("write failure"))
	_, err := f.chat.Publish(t.Context(), core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "reply"}, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: "reply"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "answer"}})
	if err == nil {
		t.Fatal("expected failure")
	}
	rows, err := persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("delivery survived failed message publish")
	}
	f.publish(t, "reply", "answer")
	f.publish(t, "reply", "answer")
	rows, err = persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("duplicate publish delivery")
	}
}
func TestLoginIsolationCancelAndRefresh(t *testing.T) {
	f := setup(t)
	l, err := f.s.StartLogin(t.Context(), f.owner, "assistant")
	must(t, err)
	other := f.owner
	other.OwnerID = "other"
	if _, err = f.s.PollLogin(t.Context(), other, "assistant", PollRequest{ID: l.ID}); err == nil {
		t.Fatal("foreign login allowed")
	}
	f.client.loginWait = make(chan struct{})
	f.client.loginStarted = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := f.s.PollLogin(t.Context(), f.owner, "assistant", PollRequest{ID: l.ID}); done <- err }()
	<-f.client.loginStarted
	must(t, f.s.CancelLogin(t.Context(), f.owner, "assistant", l.ID))
	close(f.client.loginWait)
	if err = <-done; err == nil {
		t.Fatal("canceled login confirmed")
	}
	status, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if status.Channel != "webui" {
		t.Fatal("canceled QR bound")
	}
	f.client.loginWait = nil
	l, err = f.s.StartLogin(t.Context(), f.owner, "assistant")
	must(t, err)
	f.client.result.Status = "expired"
	_, err = f.s.PollLogin(t.Context(), f.owner, "assistant", PollRequest{ID: l.ID})
	must(t, err)
	next, err := f.s.StartLogin(t.Context(), f.owner, "assistant")
	must(t, err)
	if next.ID == l.ID {
		t.Fatal("expired QR reused")
	}
}
func TestRestartKeepsCursorAndMarksInFlightUnknown(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	must(t, f.s.ingest(t.Context(), b, Updates{Cursor: "saved", Messages: []Incoming{incoming("1", "hello")}}))
	f.publish(t, "reply", "answer")
	rows, err := persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	d := rows[0]
	d.State = "sending"
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d)
	}))
	restarted, err := New(f.db, Options{MasterKey: bytes.Repeat([]byte{1}, 32), Client: f.client, Authorize: f.s.options.Authorize, Live: f.s.options.Live, Receive: f.chat.(Receiver)})
	must(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	must(t, restarted.Run(ctx))
	current, err := restarted.binding(t.Context(), "owner", "chat")
	must(t, err)
	secret, err := restarted.open(current)
	must(t, err)
	if secret.Cursor != "saved" || secret.Context != "SECRET-CONTEXT" {
		t.Fatal("restart lost state")
	}
	st, err := restarted.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if st.Deliveries[0].State != "unknown" || len(f.client.sent) != 0 {
		t.Fatal("restart resent uncertain delivery")
	}
}
func TestProtocolBoundaries(t *testing.T) {
	for _, raw := range []string{"http://ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com.evil.test", "https://user@ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com:444", "https://127.0.0.1", "https://ilinkai.weixin.qq.com/a"} {
		if _, err := validBase(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if got := messageID(json.RawMessage("18446744073709551615")); got != "18446744073709551615" {
		t.Fatal(got)
	}
	text := strings.Repeat("汉🙂", 1500)
	parts := splitText(text)
	if strings.Join(parts, "") != text {
		t.Fatal("unicode split corruption")
	}
	for _, p := range parts {
		if len(p) > 3500 {
			t.Fatal("oversized chunk")
		}
	}
}

func TestMediaIngressDeduplicationAndIndependentDelivery(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	m := incoming("42", "please read the image")
	m.Items = append(m.Items, Item{Type: 2})
	wrong := m
	wrong.From = "other"
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{wrong, m, m}}))
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{m}}))
	if f.client.downloads != 1 {
		t.Fatal("duplicate or unbound media downloaded", f.client.downloads)
	}
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items) != 1 || len(messages.Items[0].Content.AttachmentIDs) != 1 {
		t.Fatal("media missing from conversation", messages)
	}
	id := messages.Items[0].Content.AttachmentIDs[0]
	must(t, f.s.options.Files.ValidateChannelFile(ctx, "owner", "chat", b.ID, id))
	if err := f.s.options.Files.ValidateChannelFile(ctx, "owner", "chat", "another-binding", id); err == nil {
		t.Fatal("cross-binding attachment allowed")
	}
	reply, err := f.chat.Publish(ctx, core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "media"}, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: "media"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "here is the image", AttachmentIDs: []core.ID{id}}})
	must(t, err)
	f.client.mediaError = errors.New("connection lost")
	f.s.deliverNext(ctx)
	f.s.deliverNext(ctx)
	f.s.deliverNext(ctx)
	if len(f.client.sent) != 1 || len(f.client.mediaSent) != 1 || string(f.client.mediaSent[0].Data) != "image bytes" {
		t.Fatal("media was not independently sent or retried")
	}
	status, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	states := map[string]bool{}
	for _, d := range status.Deliveries {
		if d.MessageID == reply.ID {
			states[d.State] = true
		}
	}
	if !states["accepted"] || !states["unknown"] {
		t.Fatal("text success masked media uncertainty", states)
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	if _, err := f.s.options.Files.OpenChannel(ctx, "owner", "chat", b.ID, id); err == nil {
		t.Fatal("revoked channel can open file")
	}
}
func TestMediaIngressReportsPartialFailureWithoutLosingText(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	m := incoming("43", "caption")
	m.Items = append(m.Items, Item{Type: 4, File: MediaItem{Name: "bad"}}, Item{Type: 2})
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{m}}))
	messages, err := f.chat.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	c := messages.Items[0].Content
	if !strings.Contains(c.Text, "caption") || !strings.Contains(c.Text, "接收失败") || len(c.AttachmentIDs) != 1 {
		t.Fatal(c)
	}
}

func sendError(_ string, err error) error                                 { return err }
func (c *testClient) Check(context.Context, string, string, string) error { return c.checkError }

func TestDeliveryNotificationDismissalDoesNotResendAndNewOutcomeRearms(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{incoming("notice-input", "hello")}}))
	f.client.sendError = errors.New("unconfirmed")
	f.publish(t, "notice-reply", "answer")
	f.s.deliverNext(ctx)
	service := notification.New(f.db, testAuthorizer{})
	page, err := service.List(ctx, f.owner, notification.Query{Filter: "active", Kind: "weixin.delivery"})
	must(t, err)
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	first := page.Items[0]
	must(t, service.Update(ctx, f.owner, []core.ID{first.ID}, "dismiss"))
	f.s.deliverNext(ctx)
	if len(f.client.sent) != 1 {
		t.Fatal("dismissal resent a message")
	}
	deliveries, err := persistence.Scan[Delivery](ctx, f.db, deliveriesBucket)
	must(t, err)
	d := deliveries[0]
	d.Attempts++
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return f.s.saveDelivery(ctx, tx, d) }))
	page, err = service.List(ctx, f.owner, notification.Query{Filter: "active", Kind: "weixin.delivery"})
	must(t, err)
	if len(page.Items) != 0 {
		t.Fatal("retry metadata rearmed dismissed notice")
	}
	d.State = "failed"
	d.Error = "rejected"
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return f.s.saveDelivery(ctx, tx, d) }))
	page, err = service.List(ctx, f.owner, notification.Query{Filter: "active", Kind: "weixin.delivery"})
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].ID == first.ID {
		t.Fatal(page)
	}
	d.State = "accepted"
	d.Error = ""
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return f.s.saveDelivery(ctx, tx, d) }))
	page, err = service.List(ctx, f.owner, notification.Query{Filter: "active", Kind: "weixin.delivery"})
	must(t, err)
	if len(page.Items) != 0 {
		t.Fatal("successful delivery still active")
	}
}
