package qq

import (
	"bytes"
	"context"
	"path/filepath"
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
	return Registration{"device-secret", "https://q.qq.com/authorize", 600, 5}, nil
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
func (f *fakeClient) Send(_ context.Context, _ Credentials, id string, seq int, text string) (string, error) {
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
	err := f.s.bind(t.Context(), f.owner, "assistant", Credentials{AppID: "123456", AppSecret: "fixture-secret", OpenID: "user"}, "")
	must(t, err)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	return b
}
func incoming() Incoming {
	return Incoming{AppID: "123456", OpenID: "user", MessageID: "message", Content: "hello"}
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

func TestQRConnectIdentityAndCancellation(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	f.client.result = RegistrationResult{Credentials: Credentials{"123456", "fixture-secret", "user"}, Status: "confirmed"}
	l, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	if l.QRCode == "" || l.ID == "" {
		t.Fatal(l)
	}
	other := f.owner
	other.SubjectID = "other"
	if _, err = f.s.PollLogin(ctx, other, "assistant", l.ID); err == nil {
		t.Fatal("unauthorized poll")
	}
	got, err := f.s.PollLogin(ctx, f.owner, "assistant", l.ID)
	must(t, err)
	if got.Status != "confirmed" || got.QRCode != "" {
		t.Fatal(got)
	}
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if st.Channel != "qq" {
		t.Fatal(st)
	}
	if _, err = f.s.StartLogin(ctx, f.owner, "assistant"); err == nil {
		t.Fatal("replaced active binding")
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	l, err = f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	must(t, f.s.CancelLogin(ctx, f.owner, "assistant", l.ID))
	if _, err = f.s.PollLogin(ctx, f.owner, "assistant", l.ID); err == nil {
		t.Fatal("canceled login was polled")
	}
}
func TestManualPairingDedupAndRevoke(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	pair, err := f.s.Connect(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppID: "123456", AppSecret: "fixture-secret"})
	must(t, err)
	if pair.Code == "" {
		t.Fatal("missing pairing code")
	}
	must(t, f.s.CheckWebInput(ctx, "owner", "chat"))
	if f.s.CheckAvailable(ctx, "owner", "chat") == nil {
		t.Fatal("pairing not reserved")
	}
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	m := incoming()
	must(t, f.s.receive(ctx, b, m))
	st, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if st.Channel != "webui" {
		t.Fatal("wrong code paired")
	}
	m.Content = pair.Code
	must(t, f.s.receive(ctx, b, m))
	must(t, f.s.receive(ctx, b, m))
	rows, err := persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("pairing reached conversation")
	}
	m.MessageID = "next"
	m.Content = "hello"
	must(t, f.s.receive(ctx, b, m))
	must(t, f.s.receive(ctx, b, m))
	m.OpenID = "stranger"
	m.MessageID = "other"
	must(t, f.s.receive(ctx, b, m))
	rows, err = persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, err)
	if len(rows) != 1 {
		t.Fatal(len(rows))
	}
	if f.s.CheckWebInput(ctx, "owner", "chat") == nil {
		t.Fatal("WebUI was not locked")
	}
	connected, err := f.s.Connected(ctx, "owner", "chat")
	must(t, err)
	if !connected {
		t.Fatal("paired channel is not connected")
	}
	must(t, f.s.AuthorizeChannel(ctx, "owner", "chat", b.ID))
	if err := f.s.AuthorizeChannel(ctx, "owner", "chat", "other-binding"); err == nil {
		t.Fatal("authorized another binding")
	}
	publish(t, f, "reply")
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	connected, err = f.s.Connected(ctx, "owner", "chat")
	must(t, err)
	if connected {
		t.Fatal("disconnected channel remains connected")
	}
	if deliveries(t, f)[0].State != "canceled" {
		t.Fatal("unsent delivery not canceled")
	}
	must(t, f.s.receive(ctx, b, incoming()))
	rows, err = persistence.Select[conversation.Message](ctx, f.db, "im.messages", persistence.Query{})
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("revoked binding accepted message")
	}
}
func TestUnknownDeliveryNeverRetriesAndRateLimitPersists(t *testing.T) {
	for _, reason := range []string{"unknown", "rate_limited"} {
		t.Run(reason, func(t *testing.T) {
			f := setup(t)
			f.bind(t)
			publish(t, f, "reply")
			f.client.sendErr = &apiError{reason: reason, retryAfter: 3 * time.Minute}
			d := deliveries(t, f)[0]
			must(t, f.s.deliver(t.Context(), d))
			got := deliveries(t, f)[0]
			if reason == "unknown" && got.State != "unknown" {
				t.Fatal(got)
			}
			if reason == "rate_limited" && (got.State != "pending" || time.Until(got.NextAttempt) < 2*time.Minute) {
				t.Fatal(got)
			}
		})
	}
	f := setup(t)
	f.bind(t)
	publish(t, f, "reply")
	d := deliveries(t, f)[0]
	d.State = "sending"
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d)
	}))
	must(t, f.s.recover(t.Context()))
	if deliveries(t, f)[0].State != "unknown" {
		t.Fatal("recovery retried unknown send")
	}
}
func TestPairingExpiresAndLateLoginCannotReplaceOtherChannel(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	pair, err := f.s.Connect(ctx, f.owner, ConnectRequest{AssistantID: "assistant", AppID: "123456", AppSecret: "fixture-secret"})
	must(t, err)
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	b.ExpiresAt = time.Now().Add(-time.Second)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b)
	}))
	m := incoming()
	m.Content = pair.Code
	must(t, f.s.receive(ctx, b, m))
	must(t, f.s.expire(ctx, b))
	must(t, f.s.CheckAvailable(ctx, "owner", "chat"))
	l, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	f.client.result = RegistrationResult{Credentials: Credentials{"123456", "fixture-secret", "user"}, Status: "confirmed"}
	f.s.options.CheckBinding = func(context.Context, core.ID, core.ID) error { return core.NewError(core.Conflict, "other channel") }
	if _, err = f.s.PollLogin(ctx, f.owner, "assistant", l.ID); err == nil {
		t.Fatal("ignored channel conflict")
	}
}
func TestQRMissingUserRequiresPairing(t *testing.T) {
	f := setup(t)
	f.client.result = RegistrationResult{Credentials: Credentials{"123456", "fixture-secret", ""}, Status: "confirmed"}
	l, err := f.s.StartLogin(t.Context(), f.owner, "assistant")
	must(t, err)
	l, err = f.s.PollLogin(t.Context(), f.owner, "assistant", l.ID)
	must(t, err)
	if l.Status != "pairing" || l.Code == "" {
		t.Fatal(l)
	}
	must(t, f.s.CheckWebInput(t.Context(), "owner", "chat"))
}

func TestPacingDoesNotAllocateDuplicateReplySequences(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	must(t, f.s.receive(ctx, b, incoming()))
	b, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	b.NextSend = time.Now().Add(time.Minute)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b)
	}))
	publish(t, f, "reply")
	d := deliveries(t, f)[0]
	must(t, f.s.deliver(ctx, d))
	d = deliveries(t, f)[0]
	if d.ReplyID != "" || d.Attempts != 0 {
		t.Fatal("allocated reply before pacing deadline")
	}
	b.NextSend = time.Time{}
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b)
	}))
	must(t, f.s.deliver(ctx, d))
	d = deliveries(t, f)[0]
	b, err = f.s.binding(ctx, "owner", "chat")
	must(t, err)
	if d.ReplySeq != 1 || b.ReplySeq != 1 {
		t.Fatal(d.ReplySeq, b.ReplySeq)
	}
}
