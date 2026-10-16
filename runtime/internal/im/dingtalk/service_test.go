package dingtalk

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
	"molr.ai/runtime/internal/storage/files"
)

type fakeClient struct {
	sent      []string
	sendError error
	downloads int
	onSend    func()
}

func (c *fakeClient) Stream(ctx context.Context, _ Credentials, ready func() error, _ func(context.Context, Incoming) error) error {
	if err := ready(); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
func (c *fakeClient) Send(_ context.Context, _ Credentials, _ string, key string, p map[string]any) error {
	raw, _ := json.Marshal(p)
	c.sent = append(c.sent, key+":"+string(raw))
	if c.onSend != nil {
		c.onSend()
	}
	return c.sendError
}
func (c *fakeClient) Download(_ context.Context, _ Credentials, _ string, _ int64) ([]byte, string, error) {
	c.downloads++
	return []byte("fixture file"), "text/plain", nil
}
func (c *fakeClient) Upload(context.Context, Credentials, string, []byte) (string, error) {
	return "@media", nil
}

type authorizer struct{}

func (authorizer) Require(context.Context, core.Principal, policy.CheckRequest) error   { return nil }
func (authorizer) Authorize(context.Context, core.Execution, policy.CheckRequest) error { return nil }
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	s      *Service
	db     *sqlitestore.Store
	chat   conversation.Service
	client *fakeClient
	owner  core.Principal
	live   bool
}

func setup(t *testing.T) *fixture {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	f := &fixture{db: db, client: &fakeClient{}, owner: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, live: true}
	s, err := New(db, Options{MasterKey: bytes.Repeat([]byte{1}, 32), Client: f.client, PublicURL: "https://molr.example", Authorize: func(_ context.Context, p core.Principal, id core.ID, _ bool) (Target, error) {
		if p != f.owner || id != "assistant" {
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
	objects, err := files.NewObjects(filepath.Join(t.TempDir(), "files"), db)
	must(t, err)
	t.Cleanup(func() { objects.Close() })
	fileService := files.NewService(db, objects, files.Options{Authorizer: authorizer{}, AuthorizeChannel: s.AuthorizeChannel})
	s.options.Files = fileService.(files.ChannelStore)
	must(t, conversation.NewRepository(db).Insert(t.Context(), conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	f.chat = conversation.NewService(db, conversation.Options{Authorizer: authorizer{}, CheckWebInput: s.CheckWebInput, AuthorizeChannel: s.AuthorizeChannel, ValidateChannelFile: s.options.Files.ValidateChannelFile, Published: s.Published})
	s.SetReceiver(f.chat.(Receiver))
	return f
}
func (f *fixture) start(t *testing.T) (Binding, Pairing) {
	p, err := f.s.Start(t.Context(), f.owner, ConnectRequest{AssistantID: "assistant", ClientID: "ding-app", ClientSecret: "fixture-secret"})
	must(t, err)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b, p
}
func incoming(id, text string) Incoming {
	m := Incoming{ConversationType: "1", ConversationID: "private-chat", RobotCode: "ding-app", SenderStaffID: "staff", SenderID: "sender", SenderCorpID: "corp", ChatbotCorpID: "corp", ChatbotUserID: "bot", MsgID: id, MsgType: "text"}
	m.Text.Content = text
	return m
}
func (f *fixture) pair(t *testing.T) Binding {
	b, p := f.start(t)
	must(t, f.s.receive(t.Context(), b, incoming("pair", p.Code)))
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}
func TestPairingRejectsGroupsAndPinsIdentity(t *testing.T) {
	f := setup(t)
	b, p := f.start(t)
	must(t, f.s.CheckWebInput(t.Context(), "owner", "chat"))
	raw, _ := json.Marshal(b)
	if bytes.Contains(raw, []byte("fixture-secret")) || bytes.Contains(raw, []byte(p.Code)) {
		t.Fatal("plaintext credential or code persisted")
	}
	for _, change := range []func(*Incoming){func(m *Incoming) { m.ConversationType = "2" }, func(m *Incoming) { m.RobotCode = "other" }, func(m *Incoming) { m.SenderCorpID = "other" }, func(m *Incoming) { m.SenderStaffID = "" }, func(m *Incoming) { m.Text.Content = "wrong" }} {
		m := incoming("pair", p.Code)
		change(&m)
		must(t, f.s.receive(t.Context(), b, m))
		bound, err := f.s.Connected(t.Context(), "owner", "chat")
		must(t, err)
		if bound {
			t.Fatal("unauthorized pairing")
		}
	}
	m := incoming("pair", p.Code)
	must(t, f.s.receive(t.Context(), b, m))
	must(t, f.s.receive(t.Context(), b, m)) // pairing replay must not reach the model
	if f.s.CheckWebInput(t.Context(), "owner", "chat") == nil {
		t.Fatal("WebUI input accepted")
	}
	for _, change := range []func(*Incoming){func(m *Incoming) { m.ConversationType = "2" }, func(m *Incoming) { m.SenderStaffID = "other" }, func(m *Incoming) { m.SenderID = "other" }, func(m *Incoming) { m.ConversationID = "other" }} {
		m := incoming("message", "forbidden")
		change(&m)
		must(t, f.s.receive(t.Context(), b, m))
	}
	must(t, f.s.receive(t.Context(), b, incoming("message", "hello")))
	must(t, f.s.receive(t.Context(), b, incoming("message", "hello")))
	messages, err := persistence.Scan[conversation.Message](t.Context(), f.db, "im.messages")
	must(t, err)
	if len(messages) != 1 || messages[0].Content.Text != "hello" {
		t.Fatalf("messages: %+v", messages)
	}
	st, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	encoded, _ := json.Marshal(st)
	if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), p.Code) {
		t.Fatal("status exposed secrets")
	}
	forged := f.owner
	forged.OwnerID = "other"
	if _, err = f.s.Status(t.Context(), forged, "assistant"); err == nil {
		t.Fatal("owner isolation")
	}
}
func TestCancellationExpiryAndCrossChannelConflict(t *testing.T) {
	f := setup(t)
	b, p := f.start(t)
	must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
	must(t, f.s.receive(t.Context(), b, incoming("late", p.Code)))
	bound, err := f.s.Connected(t.Context(), "owner", "chat")
	must(t, err)
	if bound {
		t.Fatal("late pairing restored binding")
	}
	b, p = f.start(t)
	b.ExpiresAt = time.Now().Add(-time.Second)
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, "owner/chat", b)
	}))
	must(t, f.s.receive(t.Context(), b, incoming("expired", p.Code)))
	bound, err = f.s.Connected(t.Context(), "owner", "chat")
	must(t, err)
	if bound {
		t.Fatal("expired paired")
	}
	b, p = f.start(t)
	f.s.options.CheckAvailable = func(context.Context, core.ID, core.ID) error {
		return core.NewError(core.Conflict, "other channel bound")
	}
	if err = f.s.receive(t.Context(), b, incoming("conflict", p.Code)); err == nil {
		t.Fatal("cross-channel conflict ignored")
	}
}
func TestDeliveryUnknownNoRetryAndDisconnect(t *testing.T) {
	f := setup(t)
	b := f.pair(t)
	must(t, f.s.deliverNext(t.Context())) // pairing acknowledgement
	must(t, f.s.Published(t.Context(), "owner", conversation.Message{ID: "reply", ConversationID: "chat", Sequence: 2, Content: conversation.Content{Kind: conversation.Text, Text: "reply"}}))
	f.client.sendError = errors.New("connection lost")
	must(t, f.s.deliverNext(t.Context()))
	must(t, f.s.deliverNext(t.Context()))
	if len(f.client.sent) != 2 {
		t.Fatalf("unknown reply resent: %v", f.client.sent)
	}
	rows, err := persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	found := false
	for _, d := range rows {
		if d.MessageID == "reply" {
			found = d.State == "unknown"
		}
	}
	if !found {
		t.Fatal("missing unknown outcome")
	}
	must(t, f.s.enqueue(t.Context(), b, "later", 3, "later", nil))
	must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
	must(t, f.s.deliverNext(t.Context()))
	must(t, f.s.CheckWebInput(t.Context(), "owner", "chat"))
	if len(f.client.sent) != 2 {
		t.Fatal("sent after disconnect")
	}
}
func TestRateLimitAndRevocation(t *testing.T) {
	f := setup(t)
	b := f.pair(t)
	f.client.sendError = &apiError{Reason: "rate_limited", RetryAfter: 2 * time.Minute}
	must(t, f.s.deliverNext(t.Context()))
	rows, err := persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	if len(rows) != 1 || rows[0].State != "pending" || time.Until(rows[0].NextAttempt) < 119*time.Second {
		t.Fatalf("retry schedule: %+v", rows)
	}
	f.live = false
	must(t, f.s.enqueue(t.Context(), b, "revoked", 2, "revoked", nil))
	must(t, f.s.deliverNext(t.Context()))
	if len(f.client.sent) != 1 {
		t.Fatal("sent while revoked")
	}
	must(t, f.s.receive(t.Context(), b, incoming("revoked", "not allowed")))
	messages, err := persistence.Scan[conversation.Message](t.Context(), f.db, "im.messages")
	must(t, err)
	if len(messages) != 0 {
		t.Fatal("revoked input")
	}
}
func TestMediaScopedAndDeduplicated(t *testing.T) {
	f := setup(t)
	b := f.pair(t)
	m := incoming("file", "")
	m.MsgType = "file"
	m.Content = json.RawMessage(`{"downloadCode":"download","fileName":"fixture.txt"}`)
	must(t, f.s.receive(t.Context(), b, m))
	must(t, f.s.receive(t.Context(), b, m))
	if f.client.downloads != 1 {
		t.Fatal("duplicate download")
	}
	messages, err := persistence.Scan[conversation.Message](t.Context(), f.db, "im.messages")
	must(t, err)
	if len(messages) != 1 || len(messages[0].Content.AttachmentIDs) != 1 {
		t.Fatalf("missing attachment: %+v", messages)
	}
	id := messages[0].Content.AttachmentIDs[0]
	must(t, f.s.options.Files.ValidateChannelFile(t.Context(), "owner", "chat", b.ID, id))
	must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
	if f.s.options.Files.ValidateChannelFile(t.Context(), "owner", "chat", b.ID, id) == nil {
		t.Fatal("revoked attachment")
	}
}
func TestRunStopsWorkers(t *testing.T) {
	f := setup(t)
	f.start(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not stop")
	}
}

func TestAvailabilityReservesPairingAndExpires(t *testing.T) {
	f := setup(t)
	must(t, f.s.CheckAvailable(t.Context(), "owner", "chat"))
	b, _ := f.start(t)
	if core.CodeOf(f.s.CheckAvailable(t.Context(), "owner", "chat")) != core.Conflict {
		t.Fatal("pairing not reserved")
	}
	b.ExpiresAt = time.Now().Add(-time.Second)
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return f.s.saveBinding(ctx, tx, b) }))
	must(t, f.s.CheckAvailable(t.Context(), "owner", "chat"))
}
