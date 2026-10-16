package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
	"molr.ai/runtime/internal/storage/files"
)

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
func (s *Service) recoverDeliveries(ctx context.Context) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "sending")}})
		if err != nil {
			return err
		}
		for _, d := range rows {
			d.State = "unknown"
			d.Error = "unknown"
			if err = s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Service) Run(ctx context.Context) error {
	if err := s.recoverDeliveries(ctx); err != nil {
		return err
	}
	var wg sync.WaitGroup
	type worker struct {
		cancel context.CancelFunc
		done   chan struct{}
		bot    int64
	}
	workers := map[core.ID]worker{}
	defer func() {
		for _, w := range workers {
			w.cancel()
		}
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			s.deliverNext(ctx)
			if !sleep(ctx, time.Second) {
				return
			}
		}
	}()
	for ctx.Err() == nil {
		s.expireLogins()
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{Where: []persistence.Filter{{Field: "State", Op: "in", Value: []string{"connected", "reconnecting", "blocked"}}}})
		if err != nil {
			return err
		}
		active := map[core.ID]Binding{}
		for _, b := range rows {
			if s.options.Live(ctx, b.OwnerID, b.AssistantID) == nil {
				active[b.ID] = b
			}
		}
		for id, w := range workers {
			if _, ok := active[id]; !ok {
				w.cancel()
			}
			select {
			case <-w.done:
				delete(workers, id)
			default:
			}
		}
		busy := map[int64]bool{}
		for _, w := range workers {
			busy[w.bot] = true
		}
		for id, b := range active {
			if _, ok := workers[id]; ok || busy[b.BotID] {
				continue
			}
			wc, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			workers[id] = worker{cancel, done, b.BotID}
			busy[b.BotID] = true
			wg.Add(1)
			go func() { defer wg.Done(); defer close(done); s.pollLoop(wc, b) }()
		}
		if !sleep(ctx, time.Second) {
			return nil
		}
	}
	return nil
}
func (s *Service) pollLoop(ctx context.Context, initial Binding) {
	failures := 0
	for ctx.Err() == nil {
		b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
		if err != nil || b.ID != initial.ID || b.State == "token_invalid" || b.State == "poll_conflict" {
			return
		}
		if s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			return
		}
		token, err := s.open(b)
		var updates []Update
		if err == nil {
			updates, err = s.options.Client.Poll(ctx, token, b.Offset, 20)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = s.ingest(ctx, b, token, updates)
		}
		if err != nil {
			reason := failure(err)
			s.setFailure(ctx, b, reason)
			if reason == "token_invalid" || reason == "poll_conflict" {
				return
			}
			failures++
			delay := (retry.Policy{}).Delay(failures)
			var api *apiError
			if errors.As(err, &api) && time.Duration(api.RetryAfter)*time.Second > delay {
				delay = time.Duration(api.RetryAfter) * time.Second
			}
			if !sleep(ctx, delay) {
				return
			}
			continue
		}
		failures = 0
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
		if reason == "token_invalid" || reason == "poll_conflict" || reason == "blocked" {
			state = reason
		}
		if current.State == state && current.LastError == reason {
			return nil
		}
		current.State = state
		current.LastError = reason
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), current); err != nil {
			return err
		}
		return s.notify(ctx, current)
	})
}
func incomingKey(b Binding, u Update) string {
	return persistence.Key(b.OwnerID, hash(strconv.FormatInt(b.BotID, 10)+"/"+strconv.FormatInt(u.ID, 10)))
}
func accepts(b Binding, m *Message) bool {
	return m != nil && m.From != nil && !m.From.IsBot && m.From.ID == b.UserID && m.Chat.Type == "private" && m.Chat.ID == b.ChatID && m.ThreadID == 0
}

type prepared struct {
	text  string
	files []core.ID
}

func (s *Service) prepare(ctx context.Context, b Binding, token, key string, m *Message) prepared {
	out := prepared{text: m.Text}
	if m.Caption != "" {
		out.text = m.Caption
	}
	if r := m.Reply; r != nil {
		quote := r.Text
		if quote == "" {
			quote = r.Caption
		}
		if quote != "" {
			out.text = "[Quoted Telegram message — context only]\n" + quote + "\n[Current message]\n" + out.text
		}
	}
	var media *Media
	switch {
	case len(m.Photo) > 0:
		v := m.Photo[len(m.Photo)-1]
		v.Name = "photo.jpg"
		v.Type = "image/jpeg"
		media = &v
	case m.Document != nil:
		media = m.Document
	case m.Voice != nil:
		media = m.Voice
	case m.Audio != nil:
		media = m.Audio
	case m.Video != nil:
		media = m.Video
	}
	if media != nil {
		mediactx, cancel := context.WithTimeout(ctx, 60*time.Second)
		var err error
		if s.options.Files == nil {
			err = errors.New("files unavailable")
		} else {
			var data MediaFile
			data, err = s.options.Client.Download(mediactx, token, *media, min(s.options.MaxMediaBytes, downloadLimit))
			if err == nil {
				var file files.File
				file, err = s.options.Files.UploadChannel(mediactx, b.OwnerID, b.ConversationID, b.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "telegram-" + string(hash(key))}, Name: data.Name, MediaType: data.Type, Size: int64(len(data.Data)), Body: bytes.NewReader(data.Data)})
				if err == nil {
					out.files = append(out.files, file.ID)
				}
			}
		}
		cancel()
		if err != nil {
			out.text += "\n[Telegram attachment could not be received; its contents have not been read.]"
		}
	}
	if strings.TrimSpace(out.text) == "" && len(out.files) == 0 {
		out.text = "[Unsupported Telegram message; its contents have not been read.]"
	}
	if len(out.text) > 65536 {
		out.text = "[Telegram text exceeds the channel limit. Please split the message.]"
	}
	return out
}
func (s *Service) ingest(ctx context.Context, b Binding, token string, updates []Update) error {
	if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
		return err
	}
	preparedMessages := map[string]prepared{}
	for _, u := range updates {
		if !accepts(b, u.Message) {
			continue
		}
		key := incomingKey(b, u)
		if _, err := persistence.Load[bool](ctx, s.store, incomingBucket, key); err == nil {
			continue
		} else if !persistence.Missing(err) {
			return err
		}
		if _, ok := preparedMessages[key]; !ok {
			preparedMessages[key] = s.prepare(ctx, b, token, key, u.Message)
		}
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
			return err
		}
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		changed := current.State == "reconnecting"
		if changed {
			current.State = "connected"
			current.LastError = ""
		}
		for _, u := range updates {
			if u.ID >= current.Offset {
				current.Offset = u.ID + 1
			}
			if !accepts(b, u.Message) {
				continue
			}
			key := incomingKey(b, u)
			if _, err := persistence.Load[bool](ctx, s.store, incomingBucket, key); err == nil {
				continue
			} else if !persistence.Missing(err) {
				return err
			}
			content, ok := preparedMessages[key]
			if !ok {
				return core.NewError(core.Internal, "message preparation missing")
			}
			if s.options.Receive == nil {
				return core.NewError(core.Unavailable, "channel ingress unavailable")
			}
			if _, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "telegram-" + string(hash(key))}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: content.text, AttachmentIDs: content.files}}); err != nil {
				return err
			}
			if err = tx.Put(ctx, incomingBucket, key, true); err != nil {
				return err
			}
			now := time.Now().UTC()
			current.LastInboundAt = &now
			current.State = "connected"
			current.LastError = ""
			changed = true
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), current); err != nil {
			return err
		}
		if changed {
			return s.notify(ctx, current)
		}
		return nil
	})
}
func (s *Service) deliverNext(ctx context.Context) {
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "pending")}})
	if err != nil {
		return
	}
	sortDeliveries(rows)
	blocked := map[core.ID]bool{}
	for _, d := range rows {
		if blocked[d.BindingID] {
			continue
		}
		blocked[d.BindingID] = true
		if time.Now().Before(d.NextAttempt) {
			continue
		}
		s.deliver(ctx, d)
		if !sleep(ctx, 40*time.Millisecond) {
			return
		}
	}
}
func (s *Service) sendAttachment(ctx context.Context, b Binding, token string, d Delivery) (int64, error) {
	if s.options.Files == nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	file, err := s.options.Files.OpenChannel(ctx, b.OwnerID, b.ConversationID, b.ID, d.FileID)
	if err != nil {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	defer file.Body.Close()
	if file.File.Size > s.options.MaxMediaBytes {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	data, err := io.ReadAll(io.LimitReader(file.Body, s.options.MaxMediaBytes+1))
	if err != nil || int64(len(data)) > s.options.MaxMediaBytes {
		return 0, &apiError{Reason: "attachment_failed"}
	}
	return s.options.Client.SendMedia(ctx, token, b.ChatID, MediaFile{Name: file.File.Name, Type: file.File.MediaType, Data: data})
}
func (s *Service) deliver(ctx context.Context, d Delivery) {
	g := s.gate(d.OwnerID, d.AssistantID)
	if err := g.acquire(ctx); err != nil {
		return
	}
	defer g.release()
	var b Binding
	var token string
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		d, err = persistence.Load[Delivery](ctx, s.store, deliveriesBucket, persistence.Key(d.OwnerID, d.ID))
		if err != nil {
			return err
		}
		if d.State != "pending" || time.Now().Before(d.NextAttempt) {
			return core.NewError(core.FailedPrecondition, "delivery not ready")
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
		if b.State == "token_invalid" || b.State == "blocked" || b.State == "poll_conflict" {
			return core.NewError(core.FailedPrecondition, "connection unavailable")
		}
		token, err = s.open(b)
		if err != nil {
			return err
		}
		d.State = "sending"
		d.Attempts++
		return s.saveDelivery(ctx, tx, d)
	})
	if err != nil {
		return
	}
	sendctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	if d.FileID == "" {
		d.TelegramMessageID, err = s.options.Client.Send(sendctx, token, b.ChatID, d.Text)
	} else {
		d.TelegramMessageID, err = s.sendAttachment(sendctx, b, token, d)
	}
	cancel()
	d.State = "accepted"
	d.Error = ""
	if err != nil {
		d.Error = failure(err)
		d.State = "failed"
		switch d.Error {
		case "unknown":
			d.State = "unknown"
		case "rate_limited":
			if d.Attempts < 5 {
				var api *apiError
				delay := time.Minute
				if errors.As(err, &api) && api.RetryAfter > 0 {
					delay = time.Duration(api.RetryAfter) * time.Second
				}
				d.State = "pending"
				d.NextAttempt = time.Now().Add(delay)
			}
		}
	}
	savectx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	_ = s.store.Write(savectx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		if current.ID != b.ID {
			return nil
		}
		if d.Error == "token_invalid" || d.Error == "blocked" || d.Error == "poll_conflict" {
			current.State = d.Error
			current.LastError = d.Error
			if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), current); err != nil {
				return err
			}
		}
		if err = s.saveDelivery(ctx, tx, d); err != nil {
			return err
		}
		return s.notify(ctx, current)
	})
}

// Expired pending tokens must not remain in memory when the browser is closed.
func (s *Service) expireLogins() {
	select {
	case s.loginGate <- struct{}{}:
		defer s.loginGate.release()
	default:
		return
	}
	for id, l := range s.logins {
		if time.Now().After(l.ExpiresAt) {
			delete(s.logins, id)
		}
	}
}
