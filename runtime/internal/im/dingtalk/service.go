// Package dingtalk implements the fixed, single-user DingTalk chat channel.
package dingtalk

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"molr.ai/runtime/internal/im/channelstate"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
)

const bindingsBucket = "dingtalk.bindings"
const deliveriesBucket = "dingtalk.deliveries"
const incomingBucket = "dingtalk.incoming"

type Target struct {
	ConversationID core.ID
	Active         bool
}
type Receiver interface {
	ReceiveChannel(context.Context, core.ID, core.ID, conversation.SendUserRequest) (conversation.Message, error)
}
type Options struct {
	MasterKey      []byte
	Client         Client
	Authorize      func(context.Context, core.Principal, core.ID, bool) (Target, error)
	Live           func(context.Context, core.ID, core.ID) error
	LiveSignals    func(core.ID, core.ID) []persistence.SignalKey
	CheckAvailable func(context.Context, core.ID, core.ID) error
	Receive        Receiver
	Files          files.ChannelStore
	PublicURL      string
	MaxMediaBytes  int64
}
type Service struct {
	store   persistence.Store
	options Options
	cipher  cipher.AEAD
	gates   sync.Map
}
type Binding struct {
	ID, OwnerID, AssistantID, ConversationID               core.ID
	ClientID, CorpID, UserID, SenderID, DingConversationID string
	Secret                                                 []byte
	PairHash                                               string
	ExpiresAt                                              time.Time
	State, LastError                                       string
	ConnectedAt, LastInboundAt                             *time.Time
}
type Status struct {
	Channel, State, ClientID, LastError string
	ConnectedAt, LastInboundAt          *time.Time
	ExpiresAt                           time.Time
}
type Pairing struct {
	Code      string
	ExpiresAt time.Time
}
type ConnectRequest struct {
	AssistantID            core.ID
	ClientID, ClientSecret string
}

func New(store persistence.Store, o Options) (*Service, error) {
	if len(o.MasterKey) != 32 || o.Authorize == nil || o.Live == nil {
		return nil, core.Invalid("dingtalk", "missing dependencies")
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
		o.Client = NewClient()
	}
	if o.MaxMediaBytes <= 0 || o.MaxMediaBytes > 20<<20 {
		o.MaxMediaBytes = 20 << 20
	}
	o.MasterKey = nil
	return &Service{store: store, options: o, cipher: aead}, nil
}
func (s *Service) SetReceiver(r Receiver) { s.options.Receive = r }
func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func (s *Service) lock(ctx context.Context, owner, assistant core.ID) (func(), error) {
	value, _ := s.gates.LoadOrStore(persistence.Key(owner, assistant), make(chan struct{}, 1))
	g := value.(chan struct{})
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
func (s *Service) seal(b Binding, secret string) ([]byte, error) {
	return channelstate.Seal(s.cipher, []byte(secret), persistence.Key(b.OwnerID, b.ID))
}
func (s *Service) open(b Binding) (string, error) {
	raw, err := channelstate.Open(s.cipher, b.Secret, persistence.Key(b.OwnerID, b.ID), "invalid channel credential")
	defer clear(raw)
	return string(raw), err
}
func (s *Service) binding(ctx context.Context, owner, chat core.ID) (Binding, error) {
	return persistence.Load[Binding](ctx, s.store, bindingsBucket, persistence.Key(owner, chat))
}
func (s *Service) Connected(ctx context.Context, owner, chat core.ID) (bool, error) {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return false, nil
	}
	return b.UserID != "", err
}

// CheckAvailable reserves an unexpired pairing without locking WebUI input.
func (s *Service) CheckAvailable(ctx context.Context, owner, chat core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.UserID == "" && time.Now().After(b.ExpiresAt) {
		return nil
	}
	return core.NewError(core.Conflict, "DingTalk is connected or pairing; disconnect first")
}
func (s *Service) CheckWebInput(ctx context.Context, owner, chat core.ID) error {
	bound, err := s.Connected(ctx, owner, chat)
	if err != nil {
		return err
	}
	if bound {
		return core.NewError(core.FailedPrecondition, "send messages through DingTalk or disconnect first")
	}
	return nil
}
func (s *Service) AuthorizeChannel(ctx context.Context, owner, chat, binding core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if err != nil {
		return err
	}
	if b.ID != binding || b.UserID == "" {
		return core.NewError(core.Forbidden, "channel revoked")
	}
	return s.options.Live(ctx, b.OwnerID, b.AssistantID)
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
		result = Status{Channel: "webui", State: b.State, ClientID: b.ClientID, LastError: b.LastError, ConnectedAt: b.ConnectedAt, LastInboundAt: b.LastInboundAt, ExpiresAt: b.ExpiresAt}
		if b.UserID != "" {
			result.Channel = "dingtalk"
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
func (s *Service) Start(ctx context.Context, p core.Principal, r ConnectRequest) (Pairing, error) {
	if r.AssistantID.Validate() != nil || len(r.ClientID) > 256 || strings.TrimSpace(r.ClientID) == "" || strings.TrimSpace(r.ClientID) != r.ClientID || len(r.ClientSecret) > 8192 || strings.TrimSpace(r.ClientSecret) == "" {
		return Pairing{}, core.Invalid("dingtalk", "application credentials required")
	}
	if _, err := s.options.Authorize(ctx, p, r.AssistantID, true); err != nil {
		return Pairing{}, err
	}
	unlock, err := s.lock(ctx, p.OwnerID, r.AssistantID)
	if err != nil {
		return Pairing{}, err
	}
	defer unlock()
	pairing := Pairing{Code: "molr-" + string(newID()), ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, r.AssistantID, true)
		if err != nil {
			return err
		}
		if !a.Active {
			return core.NewError(core.FailedPrecondition, "assistant inactive")
		}
		if s.options.CheckAvailable != nil {
			if err = s.options.CheckAvailable(ctx, p.OwnerID, a.ConversationID); err != nil {
				return err
			}
		}
		old, err := s.binding(ctx, p.OwnerID, a.ConversationID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if old.UserID != "" {
			return core.NewError(core.Conflict, "disconnect the current DingTalk binding first")
		}
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("ClientID", r.ClientID)}})
		if err != nil {
			return err
		}
		for _, b := range rows {
			if (b.UserID != "" || time.Now().Before(b.ExpiresAt)) && (b.OwnerID != p.OwnerID || b.AssistantID != r.AssistantID) {
				return core.NewError(core.Conflict, "DingTalk application already in use")
			}
		}
		b := Binding{ID: newID(), OwnerID: p.OwnerID, AssistantID: r.AssistantID, ConversationID: a.ConversationID, ClientID: r.ClientID, PairHash: hash(pairing.Code), ExpiresAt: pairing.ExpiresAt, State: "connecting"}
		b.Secret, err = s.seal(b, r.ClientSecret)
		if err != nil {
			return err
		}
		if err := s.saveBinding(ctx, tx, b); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Pairing{}, err
	}
	return pairing, nil
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
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
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
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Prefix: string(p.OwnerID) + "/", Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), {Field: "State", Op: "in", Value: []string{"pending", "sending"}}}})
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
}
func (s *Service) saveBinding(ctx context.Context, tx persistence.Tx, b Binding) error {
	if err := tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
		return err
	}
	return s.notify(ctx, b)
}
func (s *Service) connectionNotice(b Binding) channelstate.Connection {
	return channelstate.Connection{Channel: "dingtalk", Bucket: bindingsBucket, State: b.State, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Attention: b.State == "reconnecting" || b.State == "credential_required"}
}
func (s *Service) notify(ctx context.Context, b Binding) error {
	return channelstate.Notify(ctx, s.store, s.connectionNotice(b))
}
func (s *Service) connectionState(ctx context.Context, initial Binding, state, reason string) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if b.ID != initial.ID {
			return nil
		}
		if state == "connected" && b.UserID == "" {
			state = "pairing"
		}
		if b.State == state && b.LastError == reason {
			return nil
		}
		b.State = state
		b.LastError = reason
		return s.saveBinding(ctx, tx, b)
	})
}

// Pairing codes never enter chat history or the model context. A private robot
// conversation is fixed together with the sender and organization on confirmation.
func (s *Service) receive(ctx context.Context, initial Binding, m Incoming) error {
	if m.ConversationType != "1" || m.RobotCode != initial.ClientID || m.SenderStaffID == "" || m.SenderID == "" || m.ChatbotCorpID == "" || m.SenderCorpID != m.ChatbotCorpID || m.ConversationID == "" || m.MsgID == "" || m.SenderID == m.ChatbotUserID {
		return nil
	}
	if len(m.MsgID) > 512 || len(m.SenderStaffID) > 256 || len(m.ConversationID) > 512 {
		return nil
	}
	b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.ID != initial.ID {
		return nil
	}
	if err = s.options.Live(ctx, b.OwnerID, b.AssistantID); err != nil {
		return nil
	}
	if b.UserID == "" {
		return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
			if err != nil {
				return err
			}
			if current.ID != b.ID || current.UserID != "" || time.Now().After(current.ExpiresAt) || m.MsgType != "text" {
				return nil
			}
			code := hash(strings.TrimSpace(m.Text.Content))
			if subtle.ConstantTimeCompare([]byte(code), []byte(current.PairHash)) != 1 {
				return nil
			}
			if err = s.options.Live(ctx, current.OwnerID, current.AssistantID); err != nil {
				return err
			}
			if s.options.CheckAvailable != nil {
				if err = s.options.CheckAvailable(ctx, b.OwnerID, b.ConversationID); err != nil {
					return err
				}
			}
			now := time.Now().UTC()
			current.UserID = m.SenderStaffID
			current.SenderID = m.SenderID
			current.CorpID = m.SenderCorpID
			current.DingConversationID = m.ConversationID
			current.PairHash = ""
			current.ExpiresAt = time.Time{}
			current.State = "connected"
			current.LastError = ""
			current.ConnectedAt = &now
			if err = s.saveBinding(ctx, tx, current); err != nil {
				return err
			}
			if err = tx.Put(ctx, incomingBucket, persistence.Key(current.OwnerID, core.ID(hash(string(current.ID)+"/"+m.MsgID))), true); err != nil {
				return err
			}
			return s.enqueue(ctx, current, "paired", 0, "Molr 已连接。请在此单聊发送消息。 / Molr connected. Send a message here to start chatting.", nil)
		})
	}
	if m.SenderStaffID != b.UserID || m.SenderID != b.SenderID || m.SenderCorpID != b.CorpID || m.ConversationID != b.DingConversationID {
		return nil
	}
	key := persistence.Key(b.OwnerID, core.ID(hash(string(b.ID)+"/"+m.MsgID)))
	var seen bool
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		err := tx.Get(ctx, incomingBucket, key, &seen)
		if persistence.Missing(err) {
			return nil
		}
		return err
	})
	if err != nil || seen {
		return err
	}
	text, refs := parseIncoming(m)
	ids, warning := s.receiveMedia(ctx, b, key, refs)
	if warning != "" {
		text += "\n" + warning
	}
	if strings.TrimSpace(text) == "" && len(ids) == 0 {
		text = "[钉钉发送了暂不支持的消息类型，内容未读取。 / Unsupported DingTalk message; content was not read.]"
	}
	if len(text) > 60000 {
		text = splitText(text, 60000)[0] + "\n[消息过长，已截断。]"
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
			return err
		}
		var duplicate bool
		err := tx.Get(ctx, incomingBucket, key, &duplicate)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if duplicate {
			return nil
		}
		if _, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "dingtalk-" + hash(key)}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text, AttachmentIDs: ids}}); err != nil {
			return err
		}
		if err = tx.Put(ctx, incomingBucket, key, true); err != nil {
			return err
		}
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		current.LastInboundAt = &now
		return s.saveBinding(ctx, tx, current)
	})
}
