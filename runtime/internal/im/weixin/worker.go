package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
	"molr.ai/runtime/internal/storage/files"
)

func strconvPart(n int) string { return strconv.Itoa(n) }
func splitText(text string) []string {
	var chunks []string
	for len(text) > 0 {
		n := len(text)
		if n > 3500 {
			n = 3500
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
		}
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	return chunks
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
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Run owns poll workers; all stop before the application closes its database.
func (s *Service) Run(ctx context.Context) error {
	if err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "sending")}})
		if err != nil {
			return err
		}
		for _, d := range rows {
			if d.State == "sending" {
				d.State = "unknown"
				d.Error = "unknown"
				if err = s.saveDelivery(ctx, tx, d); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	err := s.runBindings(ctx)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func (s *Service) pollLoop(ctx context.Context, initial Binding) {
	if lifecycle, ok := s.options.Client.(interface {
		notify(context.Context, string, string, bool)
	}); ok {
		secret, err := s.open(initial)
		if err == nil {
			lifecycle.notify(ctx, initial.BaseURL, secret.Token, true)
			defer lifecycle.notify(context.WithoutCancel(ctx), initial.BaseURL, secret.Token, false)
		}
	}
	policy := retry.Policy{}
	failures := 0
	waitRetry := func() bool {
		failures++
		return sleep(ctx, policy.Delay(failures))
	}
	for ctx.Err() == nil {
		b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
		if err != nil || b.ID != initial.ID || b.State == "login_required" {
			return
		}
		if s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			return
		}
		secret, err := s.open(b)
		if err != nil {
			s.setFailure(ctx, b, "credential_unavailable")
			if !waitRetry() {
				return
			}
			continue
		}
		r, err := s.options.Client.Poll(ctx, b.BaseURL, secret.Token, secret.Cursor)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			reason := failure(err)
			s.setFailure(ctx, b, reason)
			if reason == "login_required" {
				return
			}
			if !waitRetry() {
				return
			}
			continue
		}
		if err = s.ingest(ctx, b, r); err != nil {
			if !waitRetry() {
				return
			}
			continue
		}
		failures = 0
		// Also bounds the request rate if a backend returns immediately with no data.
		if !sleep(ctx, 250*time.Millisecond) {
			return
		}
	}
}
func (s *Service) setFailure(ctx context.Context, b Binding, reason string) {
	_ = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil || current.ID != b.ID {
			return err
		}
		state := "reconnecting"
		if reason == "login_required" {
			state = "login_required"
		}
		if current.State == state && current.LastError == reason {
			return s.syncConnectionNotice(ctx, current)
		}
		current.State = state
		current.LastError = reason
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(current.OwnerID, current.ConversationID), current); err != nil {
			return err
		}
		return s.notify(ctx, current)
	})
}
func (s *Service) ingest(ctx context.Context, b Binding, r Updates) error {
	if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
		return err
	}
	prepared := map[string]preparedMedia{}
	batch := map[string]quoteRecord{}
	for _, m := range r.Messages {
		if m.Type != 1 || m.Group != "" || m.From != b.UserID {
			continue
		}
		key := incomingKey(b, m)
		if _, err := persistence.Load[bool](ctx, s.store, incomingBucket, key); err == nil {
			continue
		} else if !persistence.Missing(err) {
			return err
		}
		if _, ok := prepared[key]; !ok {
			value := s.receiveMedia(ctx, b, m, key)
			value.original = append([]core.ID(nil), value.ids...)
			quote, ids, err := s.quotedContent(ctx, b, m, key, batch)
			if err != nil {
				return err
			}
			value.quote = quote
			value.ids = append(value.ids, ids...)
			if len(value.ids) > 20 {
				value.ids = value.ids[:20]
				value.failed = true
			}
			prepared[key] = value
			if id := messageID(m.ID); id != "" {
				batch[id] = quoteRecord{BindingID: b.ID, Text: messageText(m), Files: value.original, CreatedAt: time.Now().UTC()}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		if current.ID != b.ID {
			return core.NewError(core.Forbidden, "channel revoked")
		}
		if err = s.options.Live(ctx, b.OwnerID, b.AssistantID); err != nil {
			return err
		}
		secret, err := s.open(current)
		if err != nil {
			return err
		}
		changed := current.State != "connected"
		current.State = "connected"
		current.LastError = ""
		now := time.Now().UTC()
		current.LastPollAt = &now
		freshContext := false
		for _, m := range r.Messages {
			if m.Type != 1 || m.Group != "" || m.From != b.UserID {
				continue
			}
			key := incomingKey(b, m)
			var old bool
			err = tx.Get(ctx, incomingBucket, key, &old)
			if err == nil {
				continue
			}
			if !persistence.Missing(err) {
				return err
			}
			if m.Context != "" {
				secret.Context = m.Context
				freshContext = true
			}
			var parts []string
			unsupported := false
			for _, item := range m.Items {
				switch item.Type {
				case 1:
					if item.Text.Text != "" {
						parts = append(parts, item.Text.Text)
					}
				case 2, 4, 5:
					// Downloaded outside the SQL transaction.
				case 3:
					if item.Voice.Text != "" {
						parts = append(parts, "[微信语音转写] "+item.Voice.Text)
					} else {
						parts = append(parts, "[微信语音未提供文字转写，音频作为附件接收。请读取音频后回答；无法读取时说明限制，不要猜测内容。]")
					}
				default:
					unsupported = true
				}
			}
			if unsupported {
				parts = append(parts, "[收到暂不支持的微信消息，内容未读取。请用户改用文字或文件发送。]")
			}
			media := prepared[key]
			if media.failed {
				parts = append(parts, "[部分微信附件接收失败（下载、文件大小或存储限制），未读取失败附件的内容。请用户重新发送或缩小文件。]")
			}
			if media.quote != "" {
				parts = append(parts, media.quote)
			}
			text := strings.Join(parts, "\n")
			if strings.TrimSpace(text) == "" && len(media.ids) == 0 {
				text = "[收到暂不支持的微信消息，内容未读取。]"
			}
			if len(text) > 65536 {
				text = "[微信消息超过当前入口的长度限制，请分段发送。]"
			}
			if s.options.Receive == nil {
				return core.NewError(core.Unavailable, "channel ingress unavailable")
			}
			_, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "weixin-" + string(hash(key))}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text, AttachmentIDs: media.ids}})
			if err != nil {
				return err
			}
			if err := s.saveQuote(ctx, tx, b, messageID(m.ID), quoteRecord{BindingID: b.ID, Text: messageText(m), Files: media.original, CreatedAt: now}); err != nil {
				return err
			}
			if err = tx.Put(ctx, incomingBucket, key, true); err != nil {
				return err
			}
			current.LastInboundAt = &now
			changed = true
		}
		if r.Cursor != "" {
			secret.Cursor = r.Cursor
		}
		current.Secrets, err = s.seal(current, secret)
		if err != nil {
			return err
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(current.OwnerID, current.ConversationID), current); err != nil {
			return err
		}
		if freshContext {
			rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", "waiting")}})
			if err != nil {
				return err
			}
			for _, d := range rows {
				if d.BindingID == b.ID && d.State == "waiting" {
					d.State = "pending"
					d.Error = ""
					d.NextAttempt = time.Time{}
					if err = s.saveDelivery(ctx, tx, d); err != nil {
						return err
					}
				}
			}
		}
		if changed {
			return s.notify(ctx, current)
		}
		return nil
	})
}

func (s *Service) deliver(ctx context.Context, d Delivery) error {
	g := s.gate(d.OwnerID, d.AssistantID)
	if err := g.acquire(ctx); err != nil {
		return err
	}
	defer g.release()
	var b Binding
	var secret secrets
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		d, err = persistence.Load[Delivery](ctx, s.store, deliveriesBucket, persistence.Key(d.OwnerID, d.ID))
		if err != nil {
			return err
		}
		if d.State != "pending" {
			return core.NewError(core.FailedPrecondition, "delivery not pending")
		}
		b, err = s.binding(ctx, d.OwnerID, d.ConversationID)
		if err != nil {
			return err
		}
		if b.ID != d.BindingID {
			return core.NewError(core.Forbidden, "channel revoked")
		}
		if err = s.options.Live(ctx, b.OwnerID, b.AssistantID); err != nil {
			return err
		}
		if b.State == "login_required" {
			return core.NewError(core.FailedPrecondition, "login required")
		}
		secret, err = s.open(b)
		if err != nil {
			return err
		}
		if secret.Context == "" {
			d.State = "waiting"
			d.Error = "context_required"
		} else {
			d.State = "sending"
			d.Attempts++
		}
		if err = s.saveDelivery(ctx, tx, d); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
	if err != nil || d.State != "sending" {
		return err
	}
	var serverID string
	sendctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	if d.FileID == "" {
		serverID, err = s.options.Client.Send(sendctx, b.BaseURL, secret.Token, b.UserID, secret.Context, string(d.ID), d.Text)
	} else {
		serverID, err = s.sendAttachment(sendctx, b, secret, d)
	}
	cancel()
	d.State = "accepted"
	d.Error = ""
	if err != nil {
		d.Error = failure(err)
		switch d.Error {
		case "context_required":
			d.State = "waiting"
		case "rate_limited":
			d.State = "failed"
			if d.Attempts < 5 {
				d.State = "pending"
				d.NextAttempt = time.Now().Add(time.Minute)
			}
		case "unknown":
			d.State = "unknown"
		default:
			d.State = "failed"
		}
	}
	savectx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	return s.store.Write(savectx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		if current.ID != b.ID {
			return nil
		}
		if d.Error == "login_required" {
			current.State = "login_required"
			current.LastError = d.Error
			if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), current); err != nil {
				return err
			}
		}
		if d.State == "accepted" && serverID != "" {
			ref := quoteRecord{BindingID: b.ID, Text: d.Text, CreatedAt: time.Now().UTC()}
			if d.FileID != "" {
				ref.Files = []core.ID{d.FileID}
			}
			if err := s.saveQuote(ctx, tx, b, serverID, ref); err != nil {
				return err
			}
		}
		if err = s.saveDelivery(ctx, tx, d); err != nil {
			return err
		}
		return s.notify(ctx, current)
	})
}

func incomingKey(b Binding, m Incoming) string {
	id := messageID(m.ID)
	if id == "" {
		raw, _ := json.Marshal(m)
		id = string(hash(string(raw)))
	}
	return persistence.Key(b.OwnerID, hash(b.BotID+"/"+m.From+"/"+id))
}

type preparedMedia struct {
	ids      []core.ID
	failed   bool
	original []core.ID
	quote    string
}

func (s *Service) receiveMedia(ctx context.Context, b Binding, m Incoming, key string) (out preparedMedia) {
	client, ok := s.options.Client.(MediaClient)
	for i, item := range m.Items {
		if item.Type != 2 && item.Type != 3 && item.Type != 4 && item.Type != 5 {
			continue
		}
		if item.Type == 3 && item.Voice.Text != "" {
			continue
		}
		if !ok || s.options.Files == nil || len(out.ids) >= 20 {
			out.failed = true
			continue
		}
		mediaCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		data, err := client.Download(mediaCtx, item, s.options.MaxMediaBytes)
		if err == nil {
			var file files.File
			file, err = s.options.Files.UploadChannel(mediaCtx, b.OwnerID, b.ConversationID, b.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "weixin-" + string(hash(key+"/"+strconv.Itoa(i)))}, Name: data.Name, MediaType: data.Type, Size: int64(len(data.Data)), Body: bytes.NewReader(data.Data)})
			if err == nil {
				out.ids = append(out.ids, file.ID)
			}
		}
		cancel()
		if err != nil {
			out.failed = true
		}
	}
	return
}
func (s *Service) sendAttachment(ctx context.Context, b Binding, secret secrets, d Delivery) (string, error) {
	client, ok := s.options.Client.(MediaClient)
	if !ok || s.options.Files == nil {
		return "", &mediaError{}
	}
	file, err := s.options.Files.OpenChannel(ctx, b.OwnerID, b.ConversationID, b.ID, d.FileID)
	if err != nil {
		return "", &mediaError{}
	}
	defer file.Body.Close()
	if file.File.Size > s.options.MaxMediaBytes {
		return "", &mediaError{}
	}
	data, err := io.ReadAll(io.LimitReader(file.Body, s.options.MaxMediaBytes+1))
	if err != nil || int64(len(data)) > s.options.MaxMediaBytes {
		return "", &mediaError{}
	}
	return client.SendMedia(ctx, b.BaseURL, secret.Token, b.UserID, secret.Context, string(d.ID), MediaFile{Name: file.File.Name, Type: file.File.MediaType, Data: data})
}
