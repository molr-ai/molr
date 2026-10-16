package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testClient struct {
	onSend    func()
	sent      []string
	sendError error
	downloads int
	media     []Media
}

func (c *testClient) Identity(context.Context, string) (Identity, error) {
	return Identity{"TTEAM", "AAPP", "BBOT", "UBOT"}, nil
}
func (c *testClient) Connect(context.Context, string, string) (Socket, error) {
	return &testSocket{}, nil
}
func (c *testClient) Send(_ context.Context, _, _, text string) (string, error) {
	c.sent = append(c.sent, text)
	if c.onSend != nil {
		c.onSend()
	}
	return "2000000000.000001", c.sendError
}
func (c *testClient) Download(_ context.Context, _ string, f File, _ int64) (Media, error) {
	c.downloads++
	if f.Name == "bad" {
		return Media{}, errors.New("bad")
	}
	return Media{f.Name, f.MIME, []byte("test-media")}, nil
}
func (c *testClient) Upload(_ context.Context, _, _ string, m Media) (string, error) {
	c.media = append(c.media, m)
	return "", c.sendError
}

type testSocket struct{}

func (*testSocket) Read() (Envelope, error) { return Envelope{}, errors.New("closed") }
func (*testSocket) Ack(string) error        { return nil }
func (*testSocket) Close() error            { return nil }
func event(id, text string) Event {
	return Event{Type: "event_callback", TeamID: "TTEAM", AppID: "AAPP", ID: id, Message: Message{Type: "message", User: "UUSER", Channel: "DDM", ChannelType: "im", Text: text, TS: strconv.FormatInt(time.Now().Unix()+1, 10) + ".000001"}}
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
	f := &fixture{db: db, owner: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, live: true, client: &testClient{}}
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
	l, err := f.s.StartLogin(t.Context(), f.owner, ConnectRequest{AssistantID: "assistant", AppToken: "xapp-test", BotToken: "xoxb-test", UserID: "UUSER"})
	must(t, err)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	pair := event("PAIR", l.Code)
	pair.Message.TS = "1000000000.000001"
	must(t, f.s.accept(t.Context(), b, pair))
	b, err = f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}

func TestPairingIdentityAndIngress(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	l, err := f.s.StartLogin(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppToken: "xapp-test", BotToken: "xoxb-test", UserID: "UUSER"})
	must(t, err)
	must(t, f.s.CheckWebInput(ctx, "owner", "chat"))
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	raw, _ := json.Marshal(b)
	if bytes.Contains(raw, []byte("xapp-test")) || bytes.Contains(raw, []byte("xoxb-test")) || bytes.Contains(raw, []byte(l.Code)) {
		t.Fatal("stored plaintext secret")
	}
	for _, change := range []func(*Event){func(e *Event) { e.Message.User = "UOTHER" }, func(e *Event) { e.TeamID = "TOTHER" }, func(e *Event) { e.AppID = "AOTHER" }, func(e *Event) { e.Message.ChannelType = "channel" }, func(e *Event) { e.Message.BotID = "BBOT" }, func(e *Event) { e.Message.Text = "wrong" }} {
		e := event("P", l.Code)
		change(&e)
		must(t, f.s.accept(ctx, b, e))
	}
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if st.Channel != "webui" {
		t.Fatal(st)
	}
	pair := event("P", l.Code)
	pair.Message.TS = "1000000000.000001"
	must(t, f.s.accept(ctx, b, pair))
	if f.s.CheckWebInput(ctx, "owner", "chat") == nil {
		t.Fatal("WebUI accepted after pairing")
	}
	b, err = f.s.binding(ctx, "owner", "chat")
	must(t, err)
	e := event("E1", "Hello &amp; welcome")
	must(t, f.s.accept(ctx, b, e))
	must(t, f.s.accept(ctx, b, e))
	f.s.receiveNext(ctx)
	f.s.receiveNext(ctx)
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items) != 1 || messages.Items[0].Content.Text != "Hello & welcome" {
		t.Fatal(messages)
	}
	forged := f.owner
	forged.OwnerID = "other"
	if _, err = f.s.Status(ctx, forged, "assistant"); err == nil {
		t.Fatal("cross-owner read")
	}
	if err = f.s.Disconnect(ctx, forged, "assistant"); err == nil {
		t.Fatal("cross-owner disconnect")
	}
	f.live = false
	if err = f.s.accept(ctx, b, event("E2", "denied")); err == nil {
		t.Fatal("revocation ignored")
	}
	f.live = true
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	must(t, f.s.CheckWebInput(ctx, "owner", "chat"))
	must(t, f.s.accept(ctx, b, event("E3", "late")))
	if f.s.AuthorizeChannel(ctx, "owner", "chat", b.ID) == nil {
		t.Fatal("old binding authorized")
	}
}
func TestAttachmentsQuotesAndOutbound(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	e := event("E1", "picture")
	e.Message.Files = []File{{Name: "photo.png", MIME: "image/png"}, {Name: "bad", MIME: "text/plain"}}
	must(t, f.s.accept(ctx, b, e))
	f.s.receiveNext(ctx)
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	m := messages.Items[0]
	if len(m.Content.AttachmentIDs) != 1 || !strings.Contains(m.Content.Text, "接收失败") {
		t.Fatal(m)
	}
	reply := event("E2", "explain")
	reply.Message.TS = "2000000000.000002"
	reply.Message.ThreadTS = e.Message.TS
	must(t, f.s.accept(ctx, b, reply))
	f.s.receiveNext(ctx)
	messages, err = f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items) != 2 || !strings.Contains(messages.Items[1].Content.Text, "[Slack 引用消息]") || len(messages.Items[1].Content.AttachmentIDs) != 1 {
		t.Fatal(messages)
	}
	must(t, f.s.Published(ctx, "owner", conversation.Message{ID: "out", ConversationID: "chat", Sequence: 3, Content: conversation.Content{Text: "done", AttachmentIDs: m.Content.AttachmentIDs}}))
	f.s.deliverNext(ctx)
	f.s.deliverNext(ctx)
	if len(f.client.sent) != 1 || len(f.client.media) != 1 {
		t.Fatalf("text %v media %v", f.client.sent, f.client.media)
	}
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	for _, d := range st.Deliveries {
		if d.State != "accepted" {
			t.Fatal(d)
		}
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	if _, err = persistence.Load[quoteRecord](ctx, f.db, quotesBucket, quoteKey(b, e.Message.TS)); !persistence.Missing(err) {
		t.Fatal("quote not deleted", err)
	}
}
func TestDeliveryUnknownRateLimitAndCancel(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	f.client.sendError = errors.New("timeout")
	must(t, f.s.enqueue(ctx, b, "out", 1, "message"))
	f.s.deliverNext(ctx)
	f.s.deliverNext(ctx)
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if st.Deliveries[0].State != "unknown" || len(f.client.sent) != 1 {
		t.Fatal(st)
	}
	f.client.sendError = &apiError{Code: "ratelimited", RetryAfter: 125 * time.Second}
	must(t, f.s.enqueue(ctx, b, "rate", 2, "limited"))
	f.s.deliverNext(ctx)
	rows, err := persistence.Select[Delivery](ctx, f.db, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "pending")}})
	must(t, err)
	if len(rows) != 1 || time.Until(rows[0].NextAttempt) < 120*time.Second {
		t.Fatal(rows)
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	d, err := persistence.Load[Delivery](ctx, f.db, deliveriesBucket, persistence.Key("owner", rows[0].ID))
	must(t, err)
	if d.State != "canceled" {
		t.Fatal(d)
	}
}
func TestExpiredPairingAndCrossChannelConflict(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	l, err := f.s.StartLogin(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppToken: "xapp-test", BotToken: "xoxb-test", UserID: "UUSER"})
	must(t, err)
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	b.ExpiresAt = time.Now().Add(-time.Second)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, persistence.Key("owner", "chat"), b)
	}))
	must(t, f.s.accept(ctx, b, event("P", l.Code)))
	must(t, f.s.expire(ctx, b))
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if st.State != "disconnected" {
		t.Fatal(st)
	}
	f.s.options.CheckConnect = func(context.Context, core.ID, core.ID) error { return core.NewError(core.Conflict, "other channel") }
	if _, err = f.s.StartLogin(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppToken: "xapp-test", BotToken: "xoxb-test", UserID: "UUSER"}); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
}

func TestRestartRecoveryAndCancelledGate(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	must(t, f.s.enqueue(ctx, b, "out", 1, "uncertain"))
	rows, err := persistence.Select[Delivery](ctx, f.db, deliveriesBucket, persistence.Query{})
	must(t, err)
	d := rows[0]
	d.State = "sending"
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return f.s.saveDelivery(ctx, tx, d) }))
	// A second service instance uses the same durable storage and key.
	resumed, err := New(f.db, f.s.options)
	if err == nil {
		t.Fatal("cleared master key unexpectedly accepted")
	}
	options := f.s.options
	options.MasterKey = bytes.Repeat([]byte{1}, 32)
	resumed, err = New(f.db, options)
	must(t, err)
	secret, err := resumed.open(b)
	must(t, err)
	if secret.Token != "xoxb-test" {
		t.Fatal("credential did not survive restart")
	}
	runctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- resumed.Run(runctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		v, err := persistence.Load[Delivery](ctx, f.db, deliveriesBucket, persistence.Key("owner", d.ID))
		must(t, err)
		if v.State == "unknown" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("sending not recovered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err = <-done:
		must(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
	gate := f.s.gate("owner", "assistant")
	must(t, gate.acquire(ctx))
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if err = f.s.Disconnect(cancelled, f.owner, "assistant"); err == nil {
		t.Fatal("canceled lock waiter succeeded")
	}
	gate.release()
	if _, err = f.s.binding(ctx, "owner", "chat"); err != nil {
		t.Fatal("canceled disconnect changed binding")
	}
}
func TestExactSlackTimestamp(t *testing.T) {
	a, ok := slackTimestamp("1700000000.000001")
	if !ok {
		t.Fatal("valid timestamp rejected")
	}
	b, ok := slackTimestamp("1700000000.000002")
	if !ok || b-a != 1 {
		t.Fatal("precision lost")
	}
	for _, bad := range []string{"NaN", "+Inf", "1700000000", "1700000000.000001x", "-1.000001"} {
		if _, ok = slackTimestamp(bad); ok {
			t.Fatal(bad)
		}
	}
}

type streamClient struct {
	*testClient
	socket *streamSocket
}

func (c *streamClient) Connect(ctx context.Context, _, _ string) (Socket, error) {
	c.socket.ctx = ctx
	return c.socket, nil
}

type streamSocket struct {
	ctx    context.Context
	events chan Envelope
	acks   chan string
	closed bool
}

func (s *streamSocket) Read() (Envelope, error) {
	select {
	case e := <-s.events:
		return e, nil
	case <-s.ctx.Done():
		return Envelope{}, s.ctx.Err()
	}
}
func (s *streamSocket) Ack(id string) error {
	select {
	case s.acks <- id:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func (s *streamSocket) Close() error { s.closed = true; return nil }
func TestSocketAcknowledgesOnlyAfterDurableInbox(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	stream := &streamSocket{events: make(chan Envelope, 1), acks: make(chan string, 1)}
	f.s.options.Client = &streamClient{f.client, stream}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.s.listen(ctx, b) }()
	e := event("EDURABLE", "persist before ack")
	stream.events <- Envelope{Type: "events_api", ID: "envelope", Payload: e}
	select {
	case id := <-stream.acks:
		if id != "envelope" {
			t.Fatal(id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no acknowledgement")
	}
	row, err := persistence.Load[incomingRecord](t.Context(), f.db, incomingBucket, persistence.Key(b.OwnerID, hash(string(b.ID)+"/"+e.ID)))
	must(t, err)
	if row.State != "pending" || row.Event.Message.Text != e.Message.Text {
		t.Fatal(row)
	}
	if f.client.downloads != 0 {
		t.Fatal("download before acknowledgement")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("socket did not stop")
	}
	if !stream.closed {
		t.Fatal("socket was not closed")
	}
}

func TestAvailabilityAndConnectedBinding(t *testing.T) {
	f := setup(t)
	must(t, f.s.CheckAvailable(t.Context(), "owner", "chat"))
	bound, err := f.s.Connected(t.Context(), "owner", "chat")
	must(t, err)
	if bound {
		t.Fatal("unbound chat connected")
	}
	f.bind(t)
	if core.CodeOf(f.s.CheckAvailable(t.Context(), "owner", "chat")) != core.Conflict {
		t.Fatal("binding not reserved")
	}
	bound, err = f.s.Connected(t.Context(), "owner", "chat")
	must(t, err)
	if !bound {
		t.Fatal("bound chat disconnected")
	}
}
