package qq

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"molr.ai/runtime/internal/im/channelstate"

	"encoding/hex"
	"molr.ai/runtime/internal/im/channelworker"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

const bindingsBucket = "qq.bindings"
const deliveriesBucket = "qq.deliveries"
const incomingBucket = "qq.incoming"

type Receiver interface {
	ReceiveChannel(context.Context, core.ID, core.ID, conversation.SendUserRequest) (conversation.Message, error)
}
type Target struct {
	ConversationID core.ID
	Active         bool
}
type Options struct {
	MasterKey    []byte
	Client       Client
	Authorize    func(context.Context, core.Principal, core.ID, bool) (Target, error)
	Live         func(context.Context, core.ID, core.ID) error
	LiveSignals  func(core.ID, core.ID) []persistence.SignalKey
	CheckBinding func(context.Context, core.ID, core.ID) error
	Receive      Receiver
	PublicURL    string
}
type Service struct {
	store   persistence.Store
	options Options
	cipher  cipher.AEAD
	mu      sync.Mutex
	logins  map[core.ID]*pendingLogin
	gates   sync.Map
}
type Binding struct {
	ID, OwnerID, AssistantID, ConversationID core.ID
	AppID, OpenID, State, LastError          string
	ReplyID                                  string
	ReplyAt, NextSend                        time.Time
	ReplySeq                                 int
	PairHash                                 core.ID
	ExpiresAt                                time.Time
	Secrets                                  []byte
	ConnectedAt                              time.Time
	LastInboundAt                            *time.Time
}
type Status struct {
	Channel, State, LastError, AppID string
	ConnectedAt, LastInboundAt       *time.Time
}
type Login struct {
	ID                   core.ID
	Status, QRCode, Code string
	ExpiresAt            time.Time
}
type pendingLogin struct {
	Login
	OwnerID, AssistantID core.ID
	DeviceCode           string
	NextPoll             time.Time
	Interval             time.Duration
}
type ConnectRequest struct {
	AssistantID      core.ID
	AppID, AppSecret string
}
type Delivery struct {
	ID, BindingID, OwnerID, AssistantID, ConversationID, MessageID core.ID
	ReplyID                                                        string
	ReplySeq                                                       int
	Text, State, Error, RemoteID                                   string
	Sequence                                                       uint64
	Part, Attempts                                                 int
	FirstAttempt, NextAttempt                                      time.Time
}

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func hash(s string) core.ID { v := sha256.Sum256([]byte(s)); return core.ID(hex.EncodeToString(v[:])) }
func New(store persistence.Store, o Options) (*Service, error) {
	if len(o.MasterKey) != 32 || o.Authorize == nil || o.Live == nil {
		return nil, core.Invalid("qq", "missing dependencies")
	}
	block, err := aes.NewCipher(o.MasterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if o.Client == nil {
		o.Client = HTTPClient{}
	}
	o.MasterKey = nil
	return &Service{store: store, options: o, cipher: aead, logins: map[core.ID]*pendingLogin{}}, nil
}
func (s *Service) SetReceiver(r Receiver) { s.options.Receive = r }
func (s *Service) lock(ctx context.Context, owner, id core.ID) (func(), error) {
	v, _ := s.gates.LoadOrStore(persistence.Key(owner, id), make(chan struct{}, 1))
	g := v.(chan struct{})
	select {
	case g <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g
			return nil, err
		}
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Service) binding(ctx context.Context, owner, chat core.ID) (Binding, error) {
	return persistence.Load[Binding](ctx, s.store, bindingsBucket, persistence.Key(owner, chat))
}
func (s *Service) Connected(ctx context.Context, owner, chat core.ID) (bool, error) {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return b.OpenID != "", err
}
func (s *Service) seal(b Binding, secret string) ([]byte, error) {
	return channelstate.Seal(s.cipher, []byte(secret), "qq/"+persistence.Key(b.OwnerID, b.ID)+"/"+b.AppID)
}
func (s *Service) credentials(b Binding) (Credentials, error) {
	raw, err := channelstate.Open(s.cipher, b.Secrets, "qq/"+persistence.Key(b.OwnerID, b.ID)+"/"+b.AppID, "invalid credential")
	if err != nil {
		return Credentials{}, err
	}
	defer clear(raw)
	return Credentials{b.AppID, string(raw), b.OpenID}, nil
}
func (s *Service) connectionNotice(b Binding) channelstate.Connection {
	return channelstate.Connection{Channel: "qq", Bucket: bindingsBucket, State: b.State, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Attention: b.State == "reconnecting" || b.State == "credentials_required"}
}
func (s *Service) notify(ctx context.Context, b Binding) error {
	return channelstate.Notify(ctx, s.store, s.connectionNotice(b))
}
func (s *Service) Status(ctx context.Context, p core.Principal, id core.ID) (result Status, err error) {
	result = Status{Channel: "webui", State: "disconnected"}
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, id, false)
		if err != nil {
			return err
		}
		b, err := s.binding(ctx, p.OwnerID, a.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		result = Status{Channel: "webui", State: b.State, LastError: b.LastError, AppID: b.AppID, ConnectedAt: &b.ConnectedAt, LastInboundAt: b.LastInboundAt}
		if b.OpenID != "" {
			result.Channel = "qq"
		} else if time.Now().After(b.ExpiresAt) {
			result.State = "expired"
		}
		if !a.Active {
			result.State = "paused"
		}
		return nil
	})
	return
}
func (s *Service) CheckWebInput(ctx context.Context, owner, chat core.ID) error {
	bound, err := s.Connected(ctx, owner, chat)
	if err != nil {
		return err
	}
	if bound {
		return core.NewError(core.FailedPrecondition, "send messages through QQ or disconnect first")
	}
	return nil
}
func (s *Service) AuthorizeChannel(ctx context.Context, owner, chat, binding core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if err != nil {
		return err
	}
	if b.ID != binding || b.OpenID == "" {
		return core.NewError(core.Forbidden, "channel revoked")
	}
	return s.options.Live(ctx, owner, b.AssistantID)
}
func validCredentials(c Credentials) bool {
	if c.AppID == "" || len(c.AppID) > 32 || len(c.OpenID) > 128 || c.AppSecret == "" || len(c.AppSecret) > 8192 {
		return false
	}
	for _, ch := range c.AppID {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return !strings.ContainsAny(c.OpenID, " /\r\n\t")
}
func (s *Service) CheckAvailable(ctx context.Context, owner, chat core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.OpenID == "" && time.Now().After(b.ExpiresAt) {
		return nil
	}
	return core.NewError(core.Conflict, "QQ is connected or pairing; disconnect first")
}
func (s *Service) Connect(ctx context.Context, p core.Principal, r ConnectRequest) (Login, error) {
	if _, err := s.options.Authorize(ctx, p, r.AssistantID, true); err != nil {
		return Login{}, err
	}
	c := Credentials{AppID: strings.TrimSpace(r.AppID), AppSecret: strings.TrimSpace(r.AppSecret)}
	if !validCredentials(c) {
		return Login{}, core.Invalid("qq", "invalid application credentials")
	}
	unlock, err := s.lock(ctx, p.OwnerID, r.AssistantID)
	if err != nil {
		return Login{}, err
	}
	defer unlock()
	if err = s.options.Client.Check(ctx, c); err != nil {
		return Login{}, core.NewError(core.Unavailable, "QQ credentials could not be verified")
	}
	code := "molr-" + string(newID())
	if err = s.bind(ctx, p, r.AssistantID, c, code); err != nil {
		return Login{}, err
	}
	s.clearLogins(p.OwnerID, r.AssistantID)
	return Login{Status: "pairing", Code: code, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
}
func (s *Service) bind(ctx context.Context, p core.Principal, id core.ID, c Credentials, code string) error {
	if !validCredentials(c) {
		return core.Invalid("qq", "incomplete application registration")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, id, true)
		if err != nil {
			return err
		}
		if !a.Active {
			return core.NewError(core.FailedPrecondition, "assistant inactive")
		}
		if s.options.CheckBinding != nil {
			if err = s.options.CheckBinding(ctx, p.OwnerID, a.ConversationID); err != nil {
				return err
			}
		}
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("AppID", c.AppID)}})
		if err != nil {
			return err
		}
		for _, b := range rows {
			if (b.OpenID != "" || time.Now().Before(b.ExpiresAt)) && (b.OwnerID != p.OwnerID || b.AssistantID != id) {
				return core.NewError(core.Conflict, "application already connected")
			}
		}
		old, err := s.binding(ctx, p.OwnerID, a.ConversationID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil && (old.OpenID != "" || time.Now().Before(old.ExpiresAt)) {
			return core.NewError(core.Conflict, "disconnect the current QQ binding first")
		}
		if err == nil {
			if err = s.cancelDeliveries(ctx, tx, old); err != nil {
				return err
			}
		}
		b := Binding{ID: newID(), OwnerID: p.OwnerID, AssistantID: id, ConversationID: a.ConversationID, AppID: c.AppID, OpenID: c.OpenID, State: "connecting", ConnectedAt: time.Now().UTC()}
		if c.OpenID == "" {
			b.PairHash = hash(code)
			b.ExpiresAt = time.Now().UTC().Add(10 * time.Minute)
		}
		b.Secrets, err = s.seal(b, c.AppSecret)
		if err != nil {
			return err
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(p.OwnerID, a.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
}
func (s *Service) clearLogins(owner, id core.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, l := range s.logins {
		if l.OwnerID == owner && l.AssistantID == id {
			delete(s.logins, key)
		}
	}
}
func (s *Service) StartLogin(ctx context.Context, p core.Principal, id core.ID) (Login, error) {
	a, err := s.options.Authorize(ctx, p, id, true)
	if err != nil {
		return Login{}, err
	}
	if !a.Active {
		return Login{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	unlock, err := s.lock(ctx, p.OwnerID, id)
	if err != nil {
		return Login{}, err
	}
	defer unlock()
	if err = s.CheckAvailable(ctx, p.OwnerID, a.ConversationID); err != nil {
		return Login{}, err
	}
	if s.options.CheckBinding != nil {
		if err = s.options.CheckBinding(ctx, p.OwnerID, a.ConversationID); err != nil {
			return Login{}, err
		}
	}
	s.mu.Lock()
	for key, l := range s.logins {
		if time.Now().After(l.ExpiresAt) {
			delete(s.logins, key)
		}
	}
	count := len(s.logins)
	s.mu.Unlock()
	if count >= 100 {
		return Login{}, core.NewError(core.RateLimited, "too many pending registrations")
	}
	r, err := s.options.Client.Begin(ctx)
	if err != nil {
		return Login{}, core.NewError(core.Unavailable, "QQ registration unavailable")
	}
	s.clearLogins(p.OwnerID, id)
	l := &pendingLogin{Login: Login{ID: newID(), Status: "waiting", QRCode: r.URL, ExpiresAt: time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)}, OwnerID: p.OwnerID, AssistantID: id, DeviceCode: r.DeviceCode, Interval: time.Duration(r.Interval) * time.Second}
	s.mu.Lock()
	s.logins[l.ID] = l
	s.mu.Unlock()
	return l.Login, nil
}
func (s *Service) PollLogin(ctx context.Context, p core.Principal, id, loginID core.ID) (Login, error) {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return Login{}, err
	}
	unlock, err := s.lock(ctx, p.OwnerID, id)
	if err != nil {
		return Login{}, err
	}
	defer unlock()
	s.mu.Lock()
	stored, ok := s.logins[loginID]
	if !ok || stored.OwnerID != p.OwnerID || stored.AssistantID != id {
		s.mu.Unlock()
		return Login{}, core.NewError(core.NotFound, "registration expired")
	}
	l := *stored
	s.mu.Unlock()
	if time.Now().After(l.ExpiresAt) {
		l.Status = "expired"
		l.DeviceCode = ""
		l.QRCode = ""
	}
	if l.Status != "waiting" || time.Now().Before(l.NextPoll) {
		return l.Login, nil
	}
	// Persist the next poll before the network call, including failures.
	l.NextPoll = time.Now().Add(l.Interval)
	s.mu.Lock()
	s.logins[loginID] = &l
	s.mu.Unlock()
	r, err := s.options.Client.Poll(ctx, l.DeviceCode)
	if err != nil {
		return l.Login, core.NewError(core.Unavailable, "QQ registration unavailable")
	}
	if r.Status == "confirmed" {
		if err = s.options.Client.Check(ctx, r.Credentials); err != nil {
			return l.Login, core.NewError(core.Unavailable, "application not ready")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.logins[loginID]; current == nil || current.ID != l.ID {
		return Login{}, core.NewError(core.NotFound, "registration canceled")
	}
	switch r.Status {
	case "confirmed":
		if time.Now().After(l.ExpiresAt) {
			l.Status = "expired"
			l.DeviceCode = ""
			l.QRCode = ""
			s.logins[loginID] = &l
			return l.Login, nil
		}
		code := ""
		if r.OpenID == "" {
			code = "molr-" + string(newID())
		}
		if err = s.bind(ctx, p, id, r.Credentials, code); err != nil {
			return l.Login, err
		}
		l.Status = "confirmed"
		if code != "" {
			l.Status = "pairing"
			l.ExpiresAt = time.Now().UTC().Add(10 * time.Minute)
			l.Code = code
		}
	case "slow_down":
		l.Interval += 5 * time.Second
		l.NextPoll = time.Now().Add(l.Interval)
	default:
		l.Status = r.Status
	}
	if l.Status != "waiting" {
		l.DeviceCode = ""
		l.QRCode = ""
	}
	s.logins[loginID] = &l
	return l.Login, nil
}
func (s *Service) CancelLogin(ctx context.Context, p core.Principal, id, loginID core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
	unlock, err := s.lock(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.logins[loginID]; l != nil && l.OwnerID == p.OwnerID && l.AssistantID == id {
		delete(s.logins, loginID)
	}
	return nil
}
func (s *Service) Disconnect(ctx context.Context, p core.Principal, id core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
	unlock, err := s.lock(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	defer unlock()
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, id, true)
		if err != nil {
			return err
		}
		b, err := s.binding(ctx, p.OwnerID, a.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = s.cancelDeliveries(ctx, tx, b); err != nil {
			return err
		}
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(p.OwnerID, a.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
	if err == nil {
		s.clearLogins(p.OwnerID, id)
	}
	return err
}
func (s *Service) cancelDeliveries(ctx context.Context, tx persistence.Tx, b Binding) error {
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), {Field: "State", Op: "in", Value: []string{"pending", "sending"}}}})
	if err != nil {
		return err
	}
	for _, d := range rows {
		d.State = "canceled"
		if err = s.saveDelivery(ctx, tx, d); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) saveDelivery(ctx context.Context, tx persistence.Tx, d Delivery) error {
	if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
		return err
	}
	if err := channelworker.Wake(ctx, tx, bindingsBucket, d.BindingID); err != nil {
		return err
	}
	if d.State == "sending" || d.State == "pending" && d.Error == "" {
		return nil
	}
	return notification.Sync(ctx, s.store, d.OwnerID, notification.Input{Kind: "qq.delivery", Code: d.State + ":" + d.Error, Source: core.Object{Kind: "qq_delivery", ID: d.ID}, AssistantID: d.AssistantID}, d.State != "accepted" && d.State != "canceled")
}
func (s *Service) Published(ctx context.Context, owner core.ID, m conversation.Message) error {
	b, err := s.binding(ctx, owner, m.ConversationID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	text := m.Content.Text
	if m.Content.Card != nil {
		text = m.Content.Card.Title + "\n" + m.Content.Card.Summary
	}
	if m.Content.Card != nil || len(m.Content.AttachmentIDs) > 0 {
		text += "\n请在 Molr 中查看附件或处理操作：\n" + strings.TrimRight(s.options.PublicURL, "/") + "/#/assistants/" + string(b.AssistantID)
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for part, chunk := range splitText(text) {
			d := Delivery{ID: hash(string(b.ID) + "/" + string(m.ID) + "/" + strconv.Itoa(part)), BindingID: b.ID, OwnerID: owner, AssistantID: b.AssistantID, ConversationID: b.ConversationID, MessageID: m.ID, Sequence: m.Sequence, Part: part, Text: chunk, State: "pending"}
			if err = s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}
