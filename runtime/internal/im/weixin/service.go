package weixin

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

const bindingsBucket = "weixin.bindings"
const deliveriesBucket = "weixin.deliveries"
const incomingBucket = "weixin.incoming"

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
	PublicURL     string
	Files         files.ChannelStore
	MaxMediaBytes int64
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
	ID             core.ID
	OwnerID        core.ID
	AssistantID    core.ID
	ConversationID core.ID
	BotID          string
	UserID         string
	BaseURL        string
	Secrets        []byte
	State          string
	LastError      string
	ConnectedAt    time.Time
	LastInboundAt  *time.Time
	LastPollAt     *time.Time
}
type secrets struct{ Token, Context, Cursor string }
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
	Status    string
	QRCode    string
	ExpiresAt time.Time
}
type pendingLogin struct {
	Login
	OwnerID, AssistantID core.ID
	BindingID            core.ID
	Code, BaseURL        string
	Polling              bool
}
type PollRequest struct {
	ID         core.ID
	VerifyCode string
}

func New(store persistence.Store, o Options) (*Service, error) {
	if len(o.MasterKey) != 32 || o.Authorize == nil || o.Live == nil {
		return nil, core.Invalid("weixin", "missing dependencies")
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
	if o.MaxMediaBytes > mediaLimit {
		o.MaxMediaBytes = mediaLimit
	}
	o.MasterKey = nil
	return &Service{store: store, options: o, cipher: aead, logins: map[core.ID]*pendingLogin{}}, nil
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
// retains the Weixin output format until explicitly disconnected.
func (s *Service) Connected(ctx context.Context, owner, chat core.ID) (bool, error) {
	_, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return false, nil
	}
	return err == nil, err
}
func (s *Service) syncConnectionNotice(ctx context.Context, b Binding) error {
	return notification.Sync(ctx, s.store, b.OwnerID, notification.Input{Kind: "weixin.connection", Code: b.State, Source: core.Object{Kind: "assistant", ID: b.AssistantID}, AssistantID: b.AssistantID}, b.State == "login_required" || b.State == "reconnecting")
}
func (s *Service) notify(ctx context.Context, b Binding) error {
	if err := s.syncConnectionNotice(ctx, b); err != nil {
		return err
	}
	return realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: newID(), OwnerID: b.OwnerID, Topic: "weixin.changed", Object: core.Object{Kind: "assistant", ID: b.AssistantID}, ConversationID: b.ConversationID, Version: 1, CreatedAt: time.Now().UTC()})
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
		result.Channel = "weixin"
		result.State = b.State
		result.ConnectedAt = &b.ConnectedAt
		result.LastInboundAt = b.LastInboundAt
		result.LastError = b.LastError
		if !a.Active {
			result.State = "paused"
		}
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Prefix: string(b.OwnerID) + "/", Where: []persistence.Filter{persistence.Eq("BindingID", b.ID)}, OrderBy: "CreatedAt", Descending: true, Limit: 30})
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
func (s *Service) StartLogin(ctx context.Context, p core.Principal, id core.ID) (Login, error) {
	a, err := s.options.Authorize(ctx, p, id, true)
	if err != nil {
		return Login{}, err
	}
	if !a.Active {
		return Login{}, core.NewError(core.FailedPrecondition, "assistant inactive")
	}
	g := s.gate(p.OwnerID, id)
	if err := g.acquire(ctx); err != nil {
		return Login{}, err
	}
	defer g.release()
	existing, err := s.binding(ctx, p.OwnerID, a.ConversationID)
	if err != nil && !persistence.Missing(err) {
		return Login{}, err
	}
	if err == nil && existing.State == "connected" {
		return Login{Status: "already_connected"}, nil
	}
	tokens := []string{}
	if err == nil {
		credential, err := s.open(existing)
		if err != nil {
			return Login{}, err
		}
		tokens = append(tokens, credential.Token)
	}
	s.mu.Lock()
	for key, l := range s.logins {
		if time.Now().After(l.ExpiresAt) || l.Status == "expired" || (l.Status == "confirmed" || l.Status == "already_connected") || l.Status == "conflict" || l.Status == "verify_code_blocked" {
			delete(s.logins, key)
			continue
		}
		if l.OwnerID == p.OwnerID && l.AssistantID == id {
			value := l.Login
			s.mu.Unlock()
			return value, nil
		}
	}
	if len(s.logins) >= 100 {
		s.mu.Unlock()
		return Login{}, core.NewError(core.RateLimited, "too many pending logins")
	}
	s.mu.Unlock()
	q, err := s.options.Client.QR(ctx, tokens)
	if err != nil {
		return Login{}, core.NewError(core.Unavailable, "weixin QR unavailable")
	}
	if _, err = s.options.Authorize(ctx, p, id, true); err != nil {
		return Login{}, err
	}
	l := &pendingLogin{Login: Login{ID: newID(), Status: "wait", QRCode: q.Content, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}, OwnerID: p.OwnerID, AssistantID: id, Code: q.Code, BaseURL: baseURL, BindingID: existing.ID}
	s.mu.Lock()
	s.logins[l.ID] = l
	s.mu.Unlock()
	return l.Login, nil
}
func (s *Service) PollLogin(ctx context.Context, p core.Principal, id core.ID, r PollRequest) (Login, error) {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return Login{}, err
	}
	if len(r.VerifyCode) > 32 {
		return Login{}, core.Invalid("verify_code", "too long")
	}
	s.mu.Lock()
	l := s.logins[r.ID]
	if l == nil || l.OwnerID != p.OwnerID || l.AssistantID != id {
		s.mu.Unlock()
		return Login{}, core.NewError(core.NotFound, "login unavailable")
	}
	if time.Now().After(l.ExpiresAt) {
		l.Status = "expired"
		out := l.Login
		s.mu.Unlock()
		return out, nil
	}
	if (l.Status == "confirmed" || l.Status == "already_connected") || l.Status == "conflict" || l.Status == "expired" || l.Status == "verify_code_blocked" {
		out := l.Login
		s.mu.Unlock()
		return out, nil
	}
	if l.Polling {
		out := l.Login
		s.mu.Unlock()
		return out, nil
	}
	l.Polling = true
	snapshot := *l
	s.mu.Unlock()
	result, err := s.options.Client.Login(ctx, snapshot.BaseURL, snapshot.Code, strings.TrimSpace(r.VerifyCode))
	var recognized Binding
	if err == nil && result.Status == "binded_redirect" && snapshot.BindingID != "" {
		target, e := s.options.Authorize(ctx, p, id, true)
		if e == nil {
			recognized, e = s.binding(ctx, p.OwnerID, target.ConversationID)
		}
		if e == nil && recognized.ID == snapshot.BindingID && (result.BotID == "" || result.BotID == recognized.BotID) && (result.UserID == "" || result.UserID == recognized.UserID) {
			credential, e := s.open(recognized)
			if e == nil {
				e = s.options.Client.Check(ctx, recognized.BaseURL, credential.Token, recognized.UserID)
			}
			if e != nil {
				err = e
			}
		} else {
			recognized = Binding{}
		}
	}

	g := s.gate(p.OwnerID, id)
	if gateErr := g.acquire(ctx); gateErr != nil {
		s.mu.Lock()
		if s.logins[r.ID] == l {
			l.Polling = false
		}
		s.mu.Unlock()
		return Login{}, gateErr
	}
	defer g.release()
	s.mu.Lock()
	defer s.mu.Unlock()
	l = s.logins[r.ID]
	if l == nil {
		return Login{}, core.NewError(core.NotFound, "login canceled")
	}
	l.Polling = false
	if err != nil {
		return l.Login, core.NewError(core.Unavailable, "weixin login unavailable")
	}
	if time.Now().After(l.ExpiresAt) {
		l.Status = "expired"
		return l.Login, nil
	}
	switch result.Status {
	case "wait", "scaned", "need_verifycode", "verify_code_blocked", "expired":
		l.Status = result.Status
	case "scaned_but_redirect":
		target := result.Redirect
		if !strings.Contains(target, "://") {
			target = "https://" + target
		}
		target, err = validBase(target)
		if err != nil {
			return l.Login, core.NewError(core.Unavailable, "invalid login redirect")
		}
		l.BaseURL = target
		l.Status = "scaned"
	case "binded_redirect":
		l.Status = "conflict"
		if recognized.ID != "" {
			err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				target, e := s.options.Authorize(ctx, p, id, true)
				if e != nil {
					return e
				}
				if !target.Active {
					return core.NewError(core.Forbidden, "assistant inactive")
				}
				current, e := s.binding(ctx, p.OwnerID, target.ConversationID)
				if e != nil {
					return e
				}
				if current.ID != snapshot.BindingID {
					return core.NewError(core.Conflict, "binding changed")
				}
				if e := s.options.Live(ctx, current.OwnerID, current.AssistantID); e != nil {
					return e
				}
				current.State = "connected"
				current.LastError = ""
				if e := tx.Put(ctx, bindingsBucket, persistence.Key(current.OwnerID, current.ConversationID), current); e != nil {
					return e
				}
				return s.notify(ctx, current)
			})
			if err != nil {
				return l.Login, err
			}
			l.Status = "already_connected"
			l.QRCode = ""
			l.Code = ""
		}
	case "confirmed":
		if err = s.confirm(ctx, p, id, result); err != nil {
			if core.CodeOf(err) == core.Conflict {
				l.Status = "conflict"
				return l.Login, nil
			}
			return l.Login, err
		}
		l.Status = "confirmed"
		l.QRCode = ""
		l.Code = ""
	default:
		return l.Login, core.NewError(core.Unavailable, "unexpected login status")
	}
	return l.Login, nil
}
func (s *Service) confirm(ctx context.Context, p core.Principal, id core.ID, r LoginResult) error {
	origin, err := validBase(r.BaseURL)
	if err != nil || r.Token == "" || r.BotID == "" || r.UserID == "" || len(r.Token) > 8192 || len(r.UserID) > 256 || len(r.BotID) > 256 {
		return core.NewError(core.Unavailable, "incomplete login result")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := s.options.Authorize(ctx, p, id, true)
		if err != nil {
			return err
		}
		if !a.Active {
			return core.NewError(core.FailedPrecondition, "assistant inactive")
		}
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("BotID", r.BotID)}, {persistence.Eq("UserID", r.UserID)}}})
		if err != nil {
			return err
		}
		for _, b := range rows {
			if (b.BotID == r.BotID || b.UserID == r.UserID) && (b.OwnerID != p.OwnerID || b.AssistantID != id) {
				return core.NewError(core.Conflict, "weixin identity already bound")
			}
		}
		old, err := s.binding(ctx, p.OwnerID, a.ConversationID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil {
			if err = s.cancelDeliveries(ctx, tx, old); err != nil {
				return err
			}
		}
		b := Binding{ID: newID(), OwnerID: p.OwnerID, AssistantID: id, ConversationID: a.ConversationID, BotID: r.BotID, UserID: r.UserID, BaseURL: origin, State: "connected", ConnectedAt: time.Now().UTC()}
		b.Secrets, err = s.seal(b, secrets{Token: r.Token})
		if err != nil {
			return err
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(p.OwnerID, a.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
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
	if err == nil {
		s.mu.Lock()
		for key, l := range s.logins {
			if l.OwnerID == p.OwnerID && l.AssistantID == id {
				delete(s.logins, key)
			}
		}
		s.mu.Unlock()
	}
	return err
}
func (s *Service) cancelDeliveries(ctx context.Context, tx persistence.Tx, b Binding) error {
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Prefix: string(b.OwnerID) + "/", Where: []persistence.Filter{persistence.Eq("BindingID", b.ID)}})
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
	return s.clearQuotes(ctx, tx, b)
}
func (s *Service) CheckWebInput(ctx context.Context, owner, chat core.ID) error {
	_, err := s.binding(ctx, owner, chat)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return core.NewError(core.FailedPrecondition, "send messages through Weixin or disconnect first")
}
func (s *Service) AuthorizeChannel(ctx context.Context, owner, chat, binding core.ID) error {
	b, err := s.binding(ctx, owner, chat)
	if err != nil {
		return err
	}
	if b.ID != binding {
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

// CancelLogin removes only the pending authorization, never an existing binding.
func (s *Service) CancelLogin(ctx context.Context, p core.Principal, id, loginID core.ID) error {
	if _, err := s.options.Authorize(ctx, p, id, true); err != nil {
		return err
	}
	g := s.gate(p.OwnerID, id)
	if err := g.acquire(ctx); err != nil {
		return err
	}
	defer g.release()
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.logins[loginID]
	if l == nil {
		return nil
	}
	if l.OwnerID != p.OwnerID || l.AssistantID != id {
		return core.NewError(core.NotFound, "login unavailable")
	}
	delete(s.logins, loginID)
	return nil
}
