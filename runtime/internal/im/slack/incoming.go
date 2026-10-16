package slack

import (
	"bytes"
	"context"
	"errors"
	"html"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
	"molr.ai/runtime/internal/storage/files"
	"strconv"
	"strings"
	"sync"
	"time"
)

type incomingRecord struct {
	OwnerID, BindingID, ConversationID core.ID
	Event                              Event
	State                              string
	CreatedAt                          time.Time
}

// Run joins all socket and delivery workers before storage closes.
func (s *Service) Run(ctx context.Context) error {
	if err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
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
	}); err != nil {
		return err
	}
	var wg sync.WaitGroup
	type worker struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	workers := map[core.ID]worker{}
	runctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		for _, w := range workers {
			w.cancel()
		}
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for runctx.Err() == nil {
			s.deliverNext(runctx)
			if !sleep(runctx, time.Second) {
				return
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for runctx.Err() == nil {
			s.receiveNext(runctx)
			if !sleep(runctx, 250*time.Millisecond) {
				return
			}
		}
	}()
	for ctx.Err() == nil {
		rows, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		active := map[core.ID]bool{}
		for _, b := range rows {
			if b.ChannelID == "" && time.Now().After(b.ExpiresAt) {
				_ = s.expire(ctx, b)
				continue
			}
			if b.State == "login_required" || s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
				continue
			}
			active[b.ID] = true
			if _, ok := workers[b.ID]; !ok {
				wc, stop := context.WithCancel(runctx)
				done := make(chan struct{})
				workers[b.ID] = worker{stop, done}
				wg.Add(1)
				go func(b Binding) { defer wg.Done(); defer close(done); s.listen(wc, b) }(b)
			}
		}
		for id, w := range workers {
			if !active[id] {
				w.cancel()
			}
			select {
			case <-w.done:
				delete(workers, id)
			default:
			}
		}
		if !sleep(ctx, time.Second) {
			return nil
		}
	}
	return nil
}
func (s *Service) expire(ctx context.Context, b Binding) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.ID != b.ID || current.ChannelID != "" || time.Now().Before(current.ExpiresAt) {
			return nil
		}
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
}
func (s *Service) setState(ctx context.Context, b Binding, state, reason string) {
	_ = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		v, err := s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		if v.ID != b.ID {
			return nil
		}
		if state == "connected" && v.ChannelID == "" {
			state = "pairing"
		}
		if v.State == state && v.LastError == reason {
			return nil
		}
		v.State = state
		v.LastError = reason
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(v.OwnerID, v.ConversationID), v); err != nil {
			return err
		}
		return s.notify(ctx, v)
	})
}
func (s *Service) listen(ctx context.Context, b Binding) {
	failures := 0
	for ctx.Err() == nil {
		secret, err := s.open(b)
		if err == nil {
			var conn Socket
			conn, err = s.options.Client.Connect(ctx, secret.AppToken, b.AppID)
			if err == nil {
				connectedAt := time.Now()
				s.setState(ctx, b, "connected", "")
				for ctx.Err() == nil {
					var e Envelope
					e, err = conn.Read()
					if err != nil {
						break
					}
					if e.Type == "disconnect" {
						break
					}
					if e.ID == "" {
						continue
					}
					if e.Type == "events_api" {
						if err = s.accept(ctx, b, e.Payload); err != nil {
							break
						}
					}
					if err = conn.Ack(e.ID); err != nil {
						break
					}
				}
				conn.Close()
				if time.Since(connectedAt) > time.Minute {
					failures = 0
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			reason := failure(err)
			state := "reconnecting"
			if reason == "login_required" || reason == "missing_scope" {
				state = "login_required"
			}
			s.setState(ctx, b, state, reason)
			if state == "login_required" {
				return
			}
		}
		failures++
		delay := (retry.Policy{}).Delay(failures)
		if err == nil {
			delay = time.Second
		} else {
			var api *apiError
			if errors.As(err, &api) && api.Code == "ratelimited" {
				if delay < time.Minute {
					delay = time.Minute
				}
				if api.RetryAfter > delay {
					delay = api.RetryAfter
				}
			}
		}
		if !sleep(ctx, delay) {
			return
		}
	}
}

// Persist the envelope before acknowledging; attachments are processed later.
func (s *Service) accept(ctx context.Context, initial Binding, e Event) error {
	m := e.Message
	if _, ok := slackTimestamp(m.TS); !ok {
		return nil
	}
	if e.Type != "event_callback" || e.TeamID != initial.TeamID || e.AppID != initial.AppID || e.ID == "" || len(e.ID) > 128 || m.Type != "message" || m.ChannelType != "im" || !validID(m.Channel, "D") || m.User != initial.UserID || m.User == initial.BotUserID || m.BotID != "" || (m.Subtype != "" && m.Subtype != "file_share") {
		return nil
	}
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
		if err = s.options.Live(ctx, b.OwnerID, b.AssistantID); err != nil {
			return err
		}
		if b.ChannelID == "" {
			if time.Now().After(b.ExpiresAt) || hash(strings.TrimSpace(m.Text)) != b.PairHash {
				return nil
			}
			if s.options.CheckConnect != nil {
				if err = s.options.CheckConnect(ctx, b.OwnerID, b.ConversationID); err != nil {
					return err
				}
			}
			b.ChannelID = m.Channel
			b.BoundTS = m.TS
			b.PairHash = ""
			b.State = "connected"
			b.LastError = ""
			b.ConnectedAt = time.Now().UTC()
			if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
				return err
			}
			return s.notify(ctx, b)
		}
		if m.Channel != b.ChannelID {
			return nil
		}
		stamp, valid := slackTimestamp(m.TS)
		bound, ok := slackTimestamp(b.BoundTS)
		if !valid || !ok || stamp <= bound {
			return nil
		}
		key := persistence.Key(b.OwnerID, hash(string(b.ID)+"/"+e.ID))
		if _, err := persistence.Load[incomingRecord](ctx, s.store, incomingBucket, key); err == nil {
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, incomingBucket, key, incomingRecord{b.OwnerID, b.ID, b.ConversationID, e, "pending", time.Now().UTC()})
	})
}
func (s *Service) receiveNext(ctx context.Context) {
	bindings, err := persistence.Select[Binding](ctx, s.store, bindingsBucket, persistence.Query{})
	if err != nil {
		return
	}
	for _, b := range bindings {
		if b.ChannelID == "" || s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			continue
		}
		rows, err := persistence.Select[incomingRecord](ctx, s.store, incomingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", "pending")}, OrderBy: "CreatedAtKey", Limit: 30})
		if err != nil {
			continue
		}
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			if s.ingest(ctx, b, r) != nil {
				break
			}
		}
	}
}
func (s *Service) finishIncoming(ctx context.Context, r incomingRecord) error {
	r.State = "done"
	id := r.Event.ID
	r.Event = Event{ID: id}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, incomingBucket, persistence.Key(r.OwnerID, hash(string(r.BindingID)+"/"+id)), r)
	})
}
func (s *Service) ingest(ctx context.Context, b Binding, r incomingRecord) error {
	if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
		return err
	}
	secret, err := s.open(b)
	if err != nil {
		return err
	}
	m := r.Event.Message
	text := html.UnescapeString(m.Text)
	ids := []core.ID{}
	failed := false
	for i, f := range m.Files {
		if len(ids) >= 20 {
			failed = true
			break
		}
		if s.options.Files == nil {
			failed = true
			continue
		}
		mc, cancel := context.WithTimeout(ctx, 60*time.Second)
		media, err := s.options.Client.Download(mc, secret.Token, f, s.options.MaxMediaBytes)
		if err == nil {
			var file files.File
			file, err = s.options.Files.UploadChannel(mc, b.OwnerID, b.ConversationID, b.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "slack-" + string(hash(string(b.ID)+"/"+r.Event.ID+"/"+strconv.Itoa(i)))}, Name: media.Name, MediaType: media.Type, Size: int64(len(media.Data)), Body: bytes.NewReader(media.Data)})
			if err == nil {
				ids = append(ids, file.ID)
			}
		}
		cancel()
		if err != nil {
			failed = true
		}
	}
	if failed {
		text += "\n[部分 Slack 附件接收失败，内容未读取。请重新发送或缩小文件。]"
	}
	original := append([]core.ID(nil), ids...)
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		quote, err := persistence.Load[quoteRecord](ctx, s.store, quotesBucket, quoteKey(b, m.ThreadTS))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err != nil || quote.BindingID != b.ID || time.Since(quote.CreatedAt) > 30*24*time.Hour {
			text += "\n[Slack 引用消息不可用，不能推测原文。]"
		} else {
			text += "\n[Slack 引用消息]\n" + quote.Text + "\n[引用结束]"
			for _, id := range quote.Files {
				if len(ids) >= 20 {
					break
				}
				if s.options.Files != nil && s.options.Files.ValidateChannelFile(ctx, b.OwnerID, b.ConversationID, b.ID, id) == nil {
					ids = append(ids, id)
				} else {
					text += "\n[引用附件不可用，内容未读取。]"
				}
			}
		}
	}
	if strings.TrimSpace(text) == "" && len(ids) == 0 {
		text = "[收到暂不支持的 Slack 消息，内容未读取。]"
	}
	if len(text) > 65536 {
		text = "[Slack 消息超过长度限制，请分段发送。]"
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID); err != nil {
			return err
		}
		key := persistence.Key(b.OwnerID, hash(string(b.ID)+"/"+r.Event.ID))
		current, err := persistence.Load[incomingRecord](ctx, s.store, incomingBucket, key)
		if err != nil {
			return err
		}
		if current.State != "pending" {
			return nil
		}
		if s.options.Receive == nil {
			return core.NewError(core.Unavailable, "channel ingress unavailable")
		}
		_, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "slack-" + string(hash(key))}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text, AttachmentIDs: ids}})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err = s.saveQuote(ctx, tx, b, m.TS, quoteRecord{BindingID: b.ID, Text: html.UnescapeString(m.Text), Files: original, CreatedAt: now}); err != nil {
			return err
		}
		b, err = s.binding(ctx, b.OwnerID, b.ConversationID)
		if err != nil {
			return err
		}
		b.LastInboundAt = &now
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		if err = s.finishIncoming(ctx, r); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
}

// Keep Slack timestamps exact; do not compare event time to the host clock.
func slackTimestamp(value string) (int64, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[0]) < 1 || len(parts[0]) > 12 || len(parts[1]) != 6 {
		return 0, false
	}
	for _, part := range parts {
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, false
			}
		}
	}
	sec, e := strconv.ParseInt(parts[0], 10, 64)
	micro, f := strconv.ParseInt(parts[1], 10, 64)
	return sec*1000000 + micro, e == nil && f == nil
}
