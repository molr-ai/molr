package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type fakeClient struct {
	sent    []string
	sendErr error
	result  RegistrationResult
	onSend  func()
	onPoll  func()
}

func (f *fakeClient) Begin(context.Context) (Registration, error) {
	return Registration{"device-secret", "https://accounts.feishu.cn/authorize", 600, 5}, nil
}
func (f *fakeClient) Poll(context.Context, string) (RegistrationResult, error) {
	if f.onPoll != nil {
		f.onPoll()
	}
	return f.result, nil
}
func (f *fakeClient) Check(context.Context, Credentials) error { return nil }
func (f *fakeClient) Listen(ctx context.Context, _ Credentials, _ func(Incoming) error, state func(bool)) error {
	state(true)
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeClient) Send(_ context.Context, _ Credentials, id, text string) (string, error) {
	f.sent = append(f.sent, id+":"+text)
	if f.onSend != nil {
		f.onSend()
	}
	return "om_sent", f.sendErr
}

type authorizer struct{}

func (authorizer) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}

type fixture struct {
	s      *Service
	db     *sqlitestore.Store
	client *fakeClient
	owner  core.Principal
	chat   conversation.Service
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
	f := &fixture{db: db, client: &fakeClient{}, live: true, owner: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}}
	s, err := New(db, Options{MasterKey: bytes.Repeat([]byte{1}, 32), Client: f.client, PublicURL: "https://molr.example", Authorize: func(_ context.Context, p core.Principal, id core.ID, _ bool) (Target, error) {
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
	f.s = s
	must(t, conversation.NewRepository(db).Insert(t.Context(), conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	f.chat = conversation.NewService(db, conversation.Options{Authorizer: authorizer{}, CheckWebInput: s.CheckWebInput, AuthorizeChannel: s.AuthorizeChannel, Published: s.Published})
	s.SetReceiver(f.chat.(Receiver))
	return f
}
func (f *fixture) bind(t *testing.T) Binding {
	t.Helper()
	_, err := f.s.Connect(t.Context(), f.owner, ConnectRequest{AssistantID: "assistant", AppID: "cli_test", AppSecret: "fixture-secret", OpenID: "ou_user"})
	must(t, err)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}
func incoming() Incoming {
	return Incoming{AppID: "cli_test", OpenID: "ou_user", Tenant: "tenant", SenderType: "user", ChatType: "p2p", ChatID: "oc_chat", MessageID: "om_message", Type: "text", Content: `{"text":"hello"}`}
}
func publish(t *testing.T, f *fixture, text string) {
	t.Helper()
	must(t, f.s.Published(t.Context(), "owner", conversation.Message{ID: "published", ConversationID: "chat", Sequence: 2, Content: conversation.Content{Kind: conversation.Text, Text: text}}))
}
func deliveries(t *testing.T, f *fixture) []Delivery {
	t.Helper()
	r, e := persistence.Select[Delivery](t.Context(), f.db, deliveriesBucket, persistence.Query{})
	must(t, e)
	return r
}
func TestInboundIsolationDedupAndRebind(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	for _, change := range []func(*Incoming){func(m *Incoming) { m.OpenID = "ou_other" }, func(m *Incoming) { m.AppID = "cli_other" }, func(m *Incoming) { m.ChatType = "group" }, func(m *Incoming) { m.SenderType = "bot" }} {
		m := incoming()
		change(&m)
		must(t, f.s.receive(ctx, b, m))
	}
	r, e := persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, e)
	if len(r) != 0 {
		t.Fatal("unauthorized ingress")
	}
	must(t, f.s.receive(ctx, b, incoming()))
	must(t, f.s.receive(ctx, b, incoming()))
	m := incoming()
	m.Tenant = "another-tenant"
	m.MessageID = "om_other"
	must(t, f.s.receive(ctx, b, m))
	r, e = persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, e)
	if len(r) != 1 {
		t.Fatalf("messages %d", len(r))
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	newBinding := f.bind(t)
	must(t, f.s.receive(ctx, newBinding, incoming()))
	must(t, f.s.receive(ctx, b, m))
	r, e = persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, e)
	if len(r) != 1 {
		t.Fatal("rebind replayed old message")
	}
}
func TestBindingPermissionsAndSecrets(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	if bytes.Contains(b.Secrets, []byte("fixture-secret")) {
		t.Fatal("plaintext secret")
	}
	status, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("fixture-secret")) {
		t.Fatal("secret in status")
	}
	forged := f.owner
	forged.SessionID = "forged"
	if _, err = f.s.Status(ctx, forged, "assistant"); err == nil {
		t.Fatal("forged session")
	}
	if err = f.s.Disconnect(ctx, forged, "assistant"); err == nil {
		t.Fatal("forged disconnect")
	}
	req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "web"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "web"}}
	if _, err = f.chat.SendUser(ctx, f.owner, req); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	f.live = false
	must(t, f.s.receive(ctx, b, incoming()))
	publish(t, f, "reply")
	must(t, f.s.deliverNext(ctx))
	if len(f.client.sent) != 0 {
		t.Fatal("inactive assistant sent")
	}
	f.live = true
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	if _, err = f.chat.SendUser(ctx, f.owner, req); err != nil {
		t.Fatal(err)
	}
	if deliveries(t, f)[0].State != "canceled" {
		t.Fatal("pending delivery survived disconnect")
	}
}
func TestUUIDRetryAndCrashRecovery(t *testing.T) {
	f := setup(t)
	f.bind(t)
	ctx := t.Context()
	publish(t, f, "reply")
	f.client.sendErr = errors.New("lost acknowledgement")
	must(t, f.s.deliverNext(ctx))
	d := deliveries(t, f)[0]
	if d.State != "pending" || d.Error != "unknown" {
		t.Fatal(d)
	}
	d.State = "sending"
	d.NextAttempt = time.Time{}
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d)
	}))
	must(t, f.s.recover(ctx))
	f.client.sendErr = nil
	must(t, f.s.deliverNext(ctx))
	if len(f.client.sent) != 2 || f.client.sent[0] != f.client.sent[1] {
		t.Fatal("retry changed UUID")
	}
	if deliveries(t, f)[0].State != "accepted" {
		t.Fatal("not accepted")
	}
	d.State = "sending"
	d.FirstAttempt = time.Now().Add(-time.Hour)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d)
	}))
	must(t, f.s.recover(ctx))
	must(t, f.s.deliverNext(ctx))
	if len(f.client.sent) != 2 || deliveries(t, f)[0].State != "unknown" {
		t.Fatal("retried past idempotency window")
	}
}
func TestRegistrationThrottleCancellationAndRevocation(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	f.client.result = RegistrationResult{Credentials: Credentials{"cli_test", "fixture-secret", "ou_user"}, Status: "confirmed"}
	login, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	must(t, f.s.CancelLogin(ctx, f.owner, "assistant", login.ID))
	if _, err = f.s.PollLogin(ctx, f.owner, "assistant", login.ID); core.CodeOf(err) != core.NotFound {
		t.Fatal("canceled registration accepted")
	}
	login, err = f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	f.live = false
	if _, err = f.s.PollLogin(ctx, f.owner, "assistant", login.ID); err == nil {
		t.Fatal("inactive binding accepted")
	}
	f.live = true
	f.s.mu.Lock()
	f.s.logins[login.ID].NextPoll = time.Time{}
	f.s.mu.Unlock()
	result, err := f.s.PollLogin(ctx, f.owner, "assistant", login.ID)
	must(t, err)
	if result.Status != "confirmed" || result.QRCode != "" {
		t.Fatal(result)
	}
}
func TestTransactionRollbackAllowsRedelivery(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	err := f.db.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		must(t, f.s.receive(ctx, b, incoming()))
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	must(t, f.s.receive(ctx, b, incoming()))
	r, e := persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, e)
	if len(r) != 1 {
		t.Fatal("lost redelivery")
	}
}
func TestTextNormalizationAndUTF8Chunks(t *testing.T) {
	text := strings.Repeat("文字", 10000)
	chunks := splitText(text)
	if strings.Join(chunks, "") != text {
		t.Fatal("corrupt split")
	}
	for _, chunk := range chunks {
		if len(chunk) > 12000 || !json.Valid([]byte(`"`+chunk+`"`)) {
			t.Fatal("bad chunk")
		}
	}
	m := incoming()
	m.Type = "post"
	m.Content = `{"title":"Title","content":[[{"tag":"text","text":"hello"},{"tag":"img","image_key":"image"}]]}`
	text = incomingText(m)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "未读取") {
		t.Fatal(text)
	}
}

func TestCanceledInFlightRegistrationCannotBind(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	login, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	f.client.result = RegistrationResult{Credentials: Credentials{"cli_test", "fixture-secret", "ou_user"}, Status: "confirmed"}
	f.client.onPoll = func() { must(t, f.s.CancelLogin(ctx, f.owner, "assistant", login.ID)) }
	if _, err = f.s.PollLogin(ctx, f.owner, "assistant", login.ID); core.CodeOf(err) != core.NotFound {
		t.Fatal(err)
	}
	bound, err := f.s.Connected(ctx, "owner", "chat")
	must(t, err)
	if bound {
		t.Fatal("canceled flow bound")
	}
}
func TestOtherChannelBlocksBindingAndPublishRollback(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	f.s.options.CheckBinding = func(context.Context, core.ID, core.ID) error {
		return core.NewError(core.FailedPrecondition, "other channel")
	}
	if _, err := f.s.Connect(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppID: "cli_test", AppSecret: "secret", OpenID: "ou_user"}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	f.s.options.CheckBinding = nil
	f.bind(t)
	err := f.db.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := f.s.Published(ctx, "owner", conversation.Message{ID: "rolledback", ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: "never send"}}); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if len(deliveries(t, f)) != 0 {
		t.Fatal("delivery escaped rollback")
	}
}

func TestRunJoinsConnectionsOnShutdown(t *testing.T) {
	f := setup(t)
	f.bind(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := f.s.Status(t.Context(), f.owner, "assistant")
		must(t, err)
		if status.State == "connected" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("not connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not join")
	}
}
func TestAppCannotBindAcrossOwners(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, "other/chat", Binding{ID: "other-binding", OwnerID: "other", AssistantID: "other", AppID: "cli_test"})
	}))
	_, err := f.s.Connect(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppID: "cli_test", AppSecret: "secret", OpenID: "ou_user"})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
}
