package telegram

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/im/channelstate"
	"molr.ai/runtime/internal/im/channelworker"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
)

const bindingsBucket = "telegram.bindings"
const deliveriesBucket = "telegram.deliveries"
const incomingBucket = "telegram.incoming"

type Receiver interface {
	ReceiveChannel(context.Context, core.ID, core.ID, conversation.SendUserRequest) (conversation.Message, error)
}
type Target struct {
	ConversationID core.ID
	Active         bool
}
type Options struct {
	MasterKey     []byte
	Client        Client
	Authorize     func(context.Context, core.Principal, core.ID, bool) (Target, error)
	Live          func(context.Context, core.ID, core.ID) error
	LiveSignals   func(core.ID, core.ID) []persistence.SignalKey
	CheckBinding  func(context.Context, core.ID, core.ID) error
	Receive       Receiver
	Files         files.ChannelStore
	PublicURL     string
	MaxMediaBytes int64
}
type Service struct {
	store     persistence.Store
	options   Options
	cipher    cipher.AEAD
	loginGate channelGate
	loginWake chan struct{}
	logins    map[core.ID]*pendingLogin
	gates     sync.Map
}
type Binding struct {
	ID, OwnerID, AssistantID, ConversationID core.ID
	BotID, UserID, ChatID                    int64
	BotUsername                              string
	Secrets                                  []byte
	Offset                                   int64
	State, LastError                         string
	ConnectedAt                              time.Time
	LastInboundAt                            *time.Time
}
type Delivery struct {
	ID, BindingID, OwnerID, AssistantID, ConversationID, MessageID core.ID
	Sequence                                                       uint64
	Part                                                           int
	Text                                                           string
	FileID                                                         core.ID
	State, Error                                                   string
	Attempts                                                       int
	TelegramMessageID                                              int64
	NextAttempt, CreatedAt                                         time.Time
}
type DeliveryStatus struct {
	ID, BindingID, MessageID core.ID
	Sequence                 uint64
	Part                     int
	State, Error             string
	Attempts                 int
	CreatedAt                time.Time
}
type Status struct {
	Channel, State, BotUsername, LastError string
	ConnectedAt, LastInboundAt             *time.Time
	Deliveries                             []DeliveryStatus
}
type Login struct {
	ID                                 core.ID
	Status, URL, BotUsername, UserName string
	UserID                             int64
	ExpiresAt                          time.Time
}
type pendingLogin struct {
	Login
	OwnerID, AssistantID, ConversationID core.ID
	Token, Code                          string
	BotID, ChatID, Offset                int64
}

func New(store persistence.Store, o Options) (*Service, error) {
	if len(o.MasterKey) != 32 || o.Authorize == nil || o.Live == nil || o.Client == nil {
		return nil, core.Invalid("telegram", "missing dependencies")
	}
	block, err := aes.NewCipher(o.MasterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if o.MaxMediaBytes <= 0 {
		o.MaxMediaBytes = 20 << 20
	}
	if o.MaxMediaBytes > uploadLimit {
		o.MaxMediaBytes = uploadLimit
	}
	o.MasterKey = nil
	return &Service{store: store, options: o, cipher: aead, loginGate: make(channelGate, 1), loginWake: make(chan struct{}, 1), logins: map[core.ID]*pendingLogin{}}, nil
}
func (s *Service) SetReceiver(r Receiver) { s.options.Receive = r }
func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func hash(v string) core.ID { b := sha256.Sum256([]byte(v)); return core.ID(hex.EncodeToString(b[:])) }
func (s *Service) seal(b Binding, token string) ([]byte, error) {
	return channelstate.Seal(s.cipher, []byte(token), "telegram/"+persistence.Key(b.OwnerID, b.ID))
}
func (s *Service) open(b Binding) (string, error) {
	v, err := channelstate.Open(s.cipher, b.Secrets, "telegram/"+persistence.Key(b.OwnerID, b.ID), "channel credential unavailable")
	defer clear(v)
	if err != nil {
		return "", core.NewError(core.Internal, "channel credential unavailable")
	}
	return string(v), nil
}
func (s *Service) binding(ctx context.Context, owner, chat core.ID) (Binding, error) {
	return persistence.Load[Binding](ctx, s.store, bindingsBucket, persistence.Key(owner, chat))
}
func (s *Service) Connected(ctx context.Context, owner, chat core.ID) (bool, error) {
	_, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return false, nil
	}
	return err == nil, err
}
func (s *Service) CheckWebInput(ctx context.Context, owner, chat core.ID) error {
	bound, err := s.Connected(ctx, owner, chat)
	if err != nil {
		return err
	}
	if bound {
		return core.NewError(core.FailedPrecondition, "send messages through Telegram or disconnect first")
	}
	return nil
}
func (s *Service) AuthorizeChannel(ctx context.Context, owner, chat, binding core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if err != nil {
		return err
	}
	if b.ID != binding {
		return core.NewError(core.Forbidden, "channel revoked")
	}
	return s.options.Live(ctx, owner, b.AssistantID)
}
func (s *Service) connectionNotice(b Binding) channelstate.Connection {
	return channelstate.Connection{Channel: "telegram", Bucket: bindingsBucket, State: b.State, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Attention: b.State == "reconnecting" || b.State == "token_invalid" || b.State == "blocked" || b.State == "poll_conflict"}
}
func (s *Service) notify(ctx context.Context, b Binding) error {
	return channelstate.Notify(ctx, s.store, s.connectionNotice(b))
}
func (s *Service) Status(ctx context.Context, p core.Principal, id core.ID) (Status, error) {
	return s.status(ctx, p, id, true)
}

// Summary reads the authorized binding state without loading delivery history.
func (s *Service) Summary(ctx context.Context, p core.Principal, id core.ID) (channelstate.Summary, error) {
	value, err := s.status(ctx, p, id, false)
	return channelstate.Summary{Channel: value.Channel, State: value.State}, err
}

func (s *Service) status(ctx context.Context, p core.Principal, id core.ID, deliveries bool) (out Status, err error) {
	out = Status{Channel: "webui", State: "disconnected", Deliveries: []DeliveryStatus{}}
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
		out.Channel = "telegram"
		out.State = b.State
		out.BotUsername = b.BotUsername
		out.ConnectedAt = &b.ConnectedAt
		out.LastInboundAt = b.LastInboundAt
		out.LastError = b.LastError
		if !a.Active {
			out.State = "paused"
		}
		if !deliveries {
			return nil
		}
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", p.OwnerID), persistence.Eq("BindingID", b.ID)}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "key", Limit: 30})
		if err != nil {
			return err
		}
		for _, d := range rows {
			out.Deliveries = append(out.Deliveries, DeliveryStatus{d.ID, d.BindingID, d.MessageID, d.Sequence, d.Part, d.State, d.Error, d.Attempts, d.CreatedAt})
		}
		return nil
	})
	return
}
func (s *Service) available(ctx context.Context, owner, chat core.ID, bot int64) error {
	if err := s.CheckWebInput(ctx, owner, chat); err != nil {
		return err
	}
	if s.options.CheckBinding != nil {
		if err := s.options.CheckBinding(ctx, owner, chat); err != nil {
			return err
		}
	}
	rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BotID", bot)}, Limit: 1})
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return core.WithReason(core.NewError(core.Conflict, "bot already bound"), "telegram_bot_bound")
	}
	return nil
}
func (s *Service) StartLogin(ctx context.Context, p core.Principal, id core.ID, token string) (Login, error) {
	a, err := s.options.Authorize(ctx, p, id, true)
	if err != nil {
		return Login{}, err
	}
	if !a.Active {
		return Login{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	token = strings.TrimSpace(token)
	if !tokenPattern.MatchString(token) {
		return Login{}, core.WithReason(core.Invalid("token", "invalid Bot Token"), "telegram_token_invalid")
	}
	if err = s.loginGate.acquire(ctx); err != nil {
		return Login{}, err
	}
	defer s.loginGate.release()
	for key, l := range s.logins {
		if time.Now().After(l.ExpiresAt) {
			delete(s.logins, key)
		}
	}
	if len(s.logins) >= 100 {
		return Login{}, core.NewError(core.RateLimited, "too many pending connections")
	}
	netctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bot, err := s.options.Client.Me(netctx, token)
	if err != nil {
		return Login{}, connectionError(err)
	}
	if err = s.available(ctx, p.OwnerID, a.ConversationID, bot.ID); err != nil {
		return Login{}, err
	}
	for _, l := range s.logins {
		if l.BotID == bot.ID || (l.OwnerID == p.OwnerID && l.AssistantID == id) {
			return Login{}, core.WithReason(core.NewError(core.Conflict, "connection pending"), "telegram_connection_pending")
		}
	}
	webhook, err := s.options.Client.Webhook(netctx, token)
	if err != nil {
		return Login{}, connectionError(err)
	}
	if webhook {
		return Login{}, core.WithReason(core.NewError(core.Conflict, "webhook configured"), "telegram_webhook_configured")
	}
	if _, err = s.options.Authorize(ctx, p, id, true); err != nil {
		return Login{}, err
	}
	l := &pendingLogin{Login: Login{ID: newID(), Status: "waiting", BotUsername: bot.Username, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}, OwnerID: p.OwnerID, AssistantID: id, ConversationID: a.ConversationID, Token: token, Code: string(newID()), BotID: bot.ID}
	l.URL = "https://t.me/" + bot.Username + "?start=" + l.Code
	s.logins[l.ID] = l
	select {
	case s.loginWake <- struct{}{}:
	default:
	}
	return l.Login, nil
}
func (s *Service) login(ctx context.Context, p core.Principal, id, loginID core.ID) (*pendingLogin, error) {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return nil, err
	}
	l := s.logins[loginID]
	if l == nil || l.OwnerID != p.OwnerID || l.AssistantID != id {
		return nil, core.NewError(core.NotFound, "connection unavailable")
	}
	if time.Now().After(l.ExpiresAt) {
		delete(s.logins, loginID)
		return nil, core.WithReason(core.NewError(core.FailedPrecondition, "connection expired"), "telegram_connection_expired")
	}
	return l, nil
}
func (s *Service) PollLogin(ctx context.Context, p core.Principal, id, loginID core.ID) (Login, error) {
	if err := s.loginGate.acquire(ctx); err != nil {
		return Login{}, err
	}
	defer s.loginGate.release()
	l, err := s.login(ctx, p, id, loginID)
	if err != nil {
		return Login{}, err
	}
	if l.Status == "ready" {
		return l.Login, nil
	}
	netctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	updates, err := s.options.Client.Poll(netctx, l.Token, l.Offset, 0)
	if err != nil {
		return Login{}, connectionError(err)
	}
	if _, err = s.login(ctx, p, id, loginID); err != nil {
		return Login{}, err
	}
	for _, u := range updates {
		if u.ID >= l.Offset {
			l.Offset = u.ID + 1
		}
		m := u.Message
		if l.Status == "ready" || m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" || m.ThreadID != 0 || m.Text != "/start "+l.Code {
			continue
		}
		if m.From.ID <= 0 || m.Chat.ID <= 0 {
			continue
		}
		l.UserID = m.From.ID
		l.UserName = m.From.FirstName
		if m.From.Username != "" {
			l.UserName += " (@" + m.From.Username + ")"
		}
		l.ChatID = m.Chat.ID
		l.Status = "ready"
	}
	return l.Login, nil
}
func (s *Service) Confirm(ctx context.Context, p core.Principal, id, loginID core.ID) error {
	if err := s.loginGate.acquire(ctx); err != nil {
		return err
	}
	defer s.loginGate.release()
	l, err := s.login(ctx, p, id, loginID)
	if err != nil {
		return err
	}
	if l.Status != "ready" {
		return core.NewError(core.FailedPrecondition, "start the bot first")
	}
	g := s.gate(p.OwnerID, id)
	if err = g.acquire(ctx); err != nil {
		return err
	}
	defer g.release()
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, id, true)
		if err != nil {
			return err
		}
		if !a.Active || a.ConversationID != l.ConversationID {
			return core.NewError(core.Forbidden, "assistant unavailable")
		}
		if time.Now().After(l.ExpiresAt) {
			return core.WithReason(core.NewError(core.FailedPrecondition, "connection expired"), "telegram_connection_expired")
		}
		if err = s.available(ctx, p.OwnerID, a.ConversationID, l.BotID); err != nil {
			return err
		}
		b := Binding{ID: newID(), OwnerID: p.OwnerID, AssistantID: id, ConversationID: a.ConversationID, BotID: l.BotID, BotUsername: l.BotUsername, UserID: l.UserID, ChatID: l.ChatID, Offset: l.Offset, State: "connected", ConnectedAt: time.Now().UTC()}
		b.Secrets, err = s.seal(b, l.Token)
		if err != nil {
			return err
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
	if err == nil {
		delete(s.logins, loginID)
	}
	return err
}
func (s *Service) CancelLogin(ctx context.Context, p core.Principal, id, loginID core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
	if err := s.loginGate.acquire(ctx); err != nil {
		return err
	}
	defer s.loginGate.release()
	if l := s.logins[loginID]; l != nil {
		if l.OwnerID != p.OwnerID || l.AssistantID != id {
			return core.NewError(core.NotFound, "connection unavailable")
		}
		delete(s.logins, loginID)
	}
	return nil
}
func (s *Service) Disconnect(ctx context.Context, p core.Principal, id core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
	if err := s.loginGate.acquire(ctx); err != nil {
		return err
	}
	defer s.loginGate.release()
	g := s.gate(p.OwnerID, id)
	if err := g.acquire(ctx); err != nil {
		return err
	}
	defer g.release()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
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
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
	if err == nil {
		for key, l := range s.logins {
			if l.OwnerID == p.OwnerID && l.AssistantID == id {
				delete(s.logins, key)
			}
		}
	}
	return err
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
	if c := m.Content.Card; c != nil {
		text = c.Title + "\n" + c.Summary + "\n" + strings.TrimRight(s.options.PublicURL, "/") + "/#/assistants/" + string(b.AssistantID)
	}
	parts := splitText(text)
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < len(parts)+len(m.Content.AttachmentIDs); i++ {
			d := Delivery{ID: hash(string(b.ID) + "/" + string(m.ID) + "/" + strconv.Itoa(i)), BindingID: b.ID, OwnerID: owner, AssistantID: b.AssistantID, ConversationID: b.ConversationID, MessageID: m.ID, Sequence: m.Sequence, Part: i, State: "pending", CreatedAt: time.Now().UTC()}
			if i < len(parts) {
				d.Text = parts[i]
			} else {
				d.FileID = m.Content.AttachmentIDs[i-len(parts)]
			}
			if err := s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}

// Bound conservatively by UTF-16 units, preserving complete Unicode code points.
func splitText(text string) []string {
	var parts []string
	start, units := 0, 0
	for i, r := range text {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > 4000 {
			parts = append(parts, text[start:i])
			start = i
			units = 0
		}
		units += n
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
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
	return notification.Sync(ctx, s.store, d.OwnerID, notification.Input{Kind: "telegram.delivery", Code: d.State + ":" + d.Error, Source: core.Object{Kind: "telegram_delivery", ID: d.ID}, AssistantID: d.AssistantID}, d.State != "accepted" && d.State != "canceled")
}
func sortDeliveries(rows []Delivery) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Sequence != b.Sequence {
			return a.Sequence < b.Sequence
		}
		if a.MessageID == b.MessageID {
			return a.Part < b.Part
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}

func connectionError(err error) error {
	reason := "telegram_unavailable"
	switch failure(err) {
	case "token_invalid":
		reason = "telegram_token_invalid"
	case "poll_conflict":
		reason = "telegram_poll_conflict"
	case "rate_limited":
		reason = "telegram_rate_limited"
	}
	return core.WithReason(core.NewError(core.Unavailable, "Telegram connection unavailable"), core.Reason(reason))
}
