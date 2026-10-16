package slack

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/notification"
	"sort"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
)

const bindingsBucket = "slack.bindings"
const deliveriesBucket = "slack.deliveries"
const incomingBucket = "slack.incoming"

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
	Receive       Receiver
	CheckConnect  func(context.Context, core.ID, core.ID) error
	PublicURL     string
	Files         files.ChannelStore
	MaxMediaBytes int64
}
type Service struct {
	store   persistence.Store
	options Options
	cipher  cipher.AEAD
	gates   sync.Map
}
type Binding struct {
	ID             core.ID
	OwnerID        core.ID
	AssistantID    core.ID
	ConversationID core.ID
	BotID          string
	UserID         string
	TeamID         string
	AppID          string
	BotUserID      string
	ChannelID      string
	BoundTS        string
	PairHash       core.ID
	ExpiresAt      time.Time
	Secrets        []byte
	State          string
	LastError      string
	ConnectedAt    time.Time
	LastInboundAt  *time.Time
}
type secrets struct{ Token, AppToken string }
type Delivery struct {
	ID             core.ID
	BindingID      core.ID
	OwnerID        core.ID
	AssistantID    core.ID
	ConversationID core.ID
	MessageID      core.ID
	Sequence       uint64
	Text           string
	FileID         core.ID
	Part           int
	State          string
	Error          string
	Attempts       int
	NextAttempt    time.Time
	CreatedAt      time.Time
}
type DeliveryStatus struct {
	ID        core.ID
	BindingID core.ID
	MessageID core.ID
	Sequence  uint64
	Part      int
	State     string
	Error     string
	Attempts  int
	CreatedAt time.Time
}
type Status struct {
	Channel       string
	State         string
	ConnectedAt   *time.Time
	LastInboundAt *time.Time
	LastError     string
	Deliveries    []DeliveryStatus
}
type Login struct {
	ID        core.ID
	Code      string
	ExpiresAt time.Time
}
type ConnectRequest struct {
	AssistantID                core.ID
	AppToken, BotToken, UserID string
}

func New(store persistence.Store, o Options) (*Service, error) {
	if len(o.MasterKey) != 32 || o.Authorize == nil || o.Live == nil {
		return nil, core.Invalid("slack", "missing dependencies")
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
	if o.MaxMediaBytes <= 0 {
		o.MaxMediaBytes = 20 << 20
	}
	if o.MaxMediaBytes > 100<<20 {
		o.MaxMediaBytes = 100 << 20
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
func hash(text string) core.ID {
	sum := sha256.Sum256([]byte(text))
	return core.ID(hex.EncodeToString(sum[:]))
}
func (s *Service) seal(b Binding, value secrets) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.cipher.Seal(nonce, nonce, raw, []byte(persistence.Key(b.OwnerID, b.ID))), nil
}
func (s *Service) open(b Binding) (value secrets, err error) {
	n := s.cipher.NonceSize()
	if len(b.Secrets) < n {
		return value, core.NewError(core.Internal, "invalid channel credential")
	}
	raw, err := s.cipher.Open(nil, b.Secrets[:n], b.Secrets[n:], []byte(persistence.Key(b.OwnerID, b.ID)))
	if err != nil {
		return value, err
	}
	defer clear(raw)
	err = json.Unmarshal(raw, &value)
	return
}
func (s *Service) binding(ctx context.Context, owner, chat core.ID) (Binding, error) {
	return persistence.Load[Binding](ctx, s.store, bindingsBucket, persistence.Key(owner, chat))
}

// Connected is a trusted context-composition query; even an expired binding
// retains the Slack output format until explicitly disconnected.
func (s *Service) Connected(ctx context.Context, owner, chat core.ID) (bool, error) {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return false, nil
	}
	return err == nil && b.ChannelID != "", err
}
func (s *Service) syncConnectionNotice(ctx context.Context, b Binding) error {
	return notification.Sync(ctx, s.store, b.OwnerID, notification.Input{Kind: "slack.connection", Code: b.State, Source: core.Object{Kind: "assistant", ID: b.AssistantID}, AssistantID: b.AssistantID}, b.State == "login_required" || b.State == "reconnecting")
}
func (s *Service) notify(ctx context.Context, b Binding) error {
	if err := s.syncConnectionNotice(ctx, b); err != nil {
		return err
	}
	return realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: newID(), OwnerID: b.OwnerID, Topic: "slack.changed", Object: core.Object{Kind: "assistant", ID: b.AssistantID}, ConversationID: b.ConversationID, Version: 1, CreatedAt: time.Now().UTC()})
}
func (s *Service) Status(ctx context.Context, p core.Principal, id core.ID) (result Status, err error) {
	result = Status{Channel: "webui", State: "disconnected", Deliveries: []DeliveryStatus{}}
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
		result.Channel = "slack"
		if b.ChannelID == "" {
			result.Channel = "webui"
		}
		result.State = b.State
		if !b.ConnectedAt.IsZero() {
			result.ConnectedAt = &b.ConnectedAt
		}
		result.LastInboundAt = b.LastInboundAt
		result.LastError = b.LastError
		if !a.Active {
			result.State = "paused"
		}
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", b.OwnerID), persistence.Eq("BindingID", b.ID)}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "key", Limit: 30})
		if err != nil {
			return err
		}
		for _, d := range rows {
			if d.OwnerID == p.OwnerID && d.BindingID == b.ID {
				result.Deliveries = append(result.Deliveries, DeliveryStatus{ID: d.ID, BindingID: d.BindingID, MessageID: d.MessageID, Sequence: d.Sequence, Part: d.Part, State: d.State, Error: d.Error, Attempts: d.Attempts, CreatedAt: d.CreatedAt})
			}
		}
		sort.Slice(result.Deliveries, func(i, j int) bool {
			a, b := result.Deliveries[i], result.Deliveries[j]
			if a.Sequence != b.Sequence {
				return a.Sequence < b.Sequence
			}
			return a.Part < b.Part
		})
		if len(result.Deliveries) > 30 {
			result.Deliveries = result.Deliveries[len(result.Deliveries)-30:]
		}
		return nil
	})
	return
}

// StartLogin reserves one Slack app for one assistant. Pairing does not switch
// chat input until the nominated Slack user proves possession of the code.
func (s *Service) StartLogin(ctx context.Context, p core.Principal, r ConnectRequest) (Login, error) {
	a, err := s.options.Authorize(ctx, p, r.AssistantID, true)
	if err != nil {
		return Login{}, err
	}
	if !a.Active {
		return Login{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	if !validID(r.UserID, "UW") || !strings.HasPrefix(r.AppToken, "xapp-") || !strings.HasPrefix(r.BotToken, "xoxb-") || len(r.AppToken) > 8192 || len(r.BotToken) > 8192 {
		return Login{}, core.Invalid("slack", "valid Slack tokens and member ID required")
	}
	g := s.gate(p.OwnerID, r.AssistantID)
	if err = g.acquire(ctx); err != nil {
		return Login{}, err
	}
	defer g.release()
	identity, err := s.options.Client.Identity(ctx, r.BotToken)
	if err != nil {
		return Login{}, core.NewError(core.Unavailable, "Slack credential verification failed: "+failure(err))
	}
	// Reject occupied identities before opening another socket for that app.
	if err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if s.options.CheckConnect != nil {
			if err := s.options.CheckConnect(ctx, p.OwnerID, a.ConversationID); err != nil {
				return err
			}
		}
		if err := s.CheckAvailable(ctx, p.OwnerID, a.ConversationID); err != nil {
			return err
		}
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("AppID", identity.AppID)}, {persistence.Eq("TeamID", identity.TeamID), persistence.Eq("UserID", r.UserID)}}})
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return core.NewError(core.Conflict, "Slack app or member already bound")
		}
		return nil
	}); err != nil {
		return Login{}, err
	}
	// Opening once also verifies the app token and its app identity before storage.
	conn, err := s.options.Client.Connect(ctx, r.AppToken, identity.AppID)
	if err != nil {
		return Login{}, core.NewError(core.Unavailable, "Slack Socket Mode verification failed: "+failure(err))
	}
	conn.Close()
	code := "molr-" + string(newID())
	b := Binding{ID: newID(), OwnerID: p.OwnerID, AssistantID: r.AssistantID, ConversationID: a.ConversationID, TeamID: identity.TeamID, AppID: identity.AppID, BotID: identity.BotID, BotUserID: identity.UserID, UserID: r.UserID, State: "pairing", PairHash: hash(code), ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	b.Secrets, err = s.seal(b, secrets{Token: r.BotToken, AppToken: r.AppToken})
	if err != nil {
		return Login{}, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.options.Authorize(ctx, p, r.AssistantID, true)
		if err != nil {
			return err
		}
		if !current.Active || current.ConversationID != a.ConversationID {
			return core.NewError(core.FailedPrecondition, "assistant unavailable")
		}
		if s.options.CheckConnect != nil {
			if err = s.options.CheckConnect(ctx, p.OwnerID, a.ConversationID); err != nil {
				return err
			}
		}
		if _, err = s.binding(ctx, p.OwnerID, a.ConversationID); err == nil {
			return core.NewError(core.Conflict, "disconnect existing Slack connection first")
		} else if !persistence.Missing(err) {
			return err
		}
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("AppID", b.AppID)}, {persistence.Eq("TeamID", b.TeamID), persistence.Eq("UserID", b.UserID)}}})
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return core.NewError(core.Conflict, "Slack app or member already bound")
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
	if err != nil {
		return Login{}, err
	}
	return Login{b.ID, code, b.ExpiresAt}, nil
}

// CheckAvailable also includes unconfirmed reservations, so two channels cannot
// both finish authorization for the same assistant.
func (s *Service) CheckAvailable(ctx context.Context, owner, chat core.ID) error {
	_, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return core.NewError(core.Conflict, "disconnect Slack first")
}
func (s *Service) Disconnect(ctx context.Context, p core.Principal, id core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
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
		if err = s.cancelDeliveries(ctx, tx, b); err != nil {
			return err
		}
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(p.OwnerID, a.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
	return err
}
func (s *Service) cancelDeliveries(ctx context.Context, tx persistence.Tx, b Binding) error {
	// Cancellation is atomic and order-independent; let the binding/state index narrow the scan.
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Unordered: true, Prefix: string(b.OwnerID) + "/", Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), {Field: "State", Op: "in", Value: []string{"pending", "waiting", "sending"}}}})
	if err != nil {
		return err
	}
	for _, d := range rows {
		if d.BindingID == b.ID && (d.State == "pending" || d.State == "waiting" || d.State == "sending") {
			d.State = "canceled"
			if err = s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
	}
	incoming, err := persistence.Select[incomingRecord](ctx, s.store, incomingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", "pending")}})
	if err != nil {
		return err
	}
	for _, r := range incoming {
		if err = s.finishIncoming(ctx, r); err != nil {
			return err
		}
	}
	return s.clearQuotes(ctx, tx, b)
}
func (s *Service) CheckWebInput(ctx context.Context, owner, chat core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) || err == nil && b.ChannelID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	return core.NewError(core.FailedPrecondition, "send messages through Slack or disconnect first")
}
func (s *Service) AuthorizeChannel(ctx context.Context, owner, chat, binding core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if err != nil {
		return err
	}
	if b.ID != binding || b.ChannelID == "" {
		return core.NewError(core.Forbidden, "channel revoked")
	}
	return s.options.Live(ctx, b.OwnerID, b.AssistantID)
}
func (s *Service) Published(ctx context.Context, owner core.ID, m conversation.Message) error {
	b, err := s.binding(ctx, owner, m.ConversationID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.ChannelID == "" {
		return nil
	}
	text := m.Content.Text
	if m.Content.Card != nil {
		text = m.Content.Card.Title + "\n" + m.Content.Card.Summary + "\n请在 Molr 中查看和处理。"
	}
	if m.Content.Card != nil {
		text += "\n" + strings.TrimRight(s.options.PublicURL, "/") + "/#/assistants/" + string(b.AssistantID)
	}
	return s.enqueueParts(ctx, b, m.ID, m.Sequence, text, m.Content.AttachmentIDs)
}

func (s *Service) enqueue(ctx context.Context, b Binding, message core.ID, seq uint64, text string) error {
	return s.enqueueParts(ctx, b, message, seq, text, nil)
}
func (s *Service) enqueueParts(ctx context.Context, b Binding, message core.ID, seq uint64, text string, attachments []core.ID) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		chunks := splitText(text)
		for part := 0; part < len(chunks)+len(attachments); part++ {
			chunk := ""
			var fileID core.ID
			if part < len(chunks) {
				chunk = chunks[part]
			} else {
				fileID = attachments[part-len(chunks)]
			}
			d := Delivery{ID: hash(string(b.ID) + "/" + string(message) + "/" + strconvPart(part)), BindingID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, MessageID: message, Sequence: seq, Part: part, Text: chunk, FileID: fileID, State: "pending", CreatedAt: time.Now().UTC()}
			if err := s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}
