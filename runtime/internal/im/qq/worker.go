package qq

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
)

func splitText(text string) []string {
	var chunks []string
	for len(text) > 0 {
		n := len(text)
		if n > 4000 {
			n = 4000
			for !utf8.RuneStart(text[n]) {
				n--
			}
		}
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	return chunks
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
func (s *Service) receive(ctx context.Context, initial Binding, m Incoming) error {
	if m.AppID != initial.AppID || m.OpenID == "" || len(m.OpenID) > 128 || m.MessageID == "" || len(m.MessageID) > 512 {
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
		if b.ID != initial.ID || s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			return nil
		}
		key := string(hash(b.AppID + "/" + m.MessageID))
		var seen bool
		err = tx.Get(ctx, incomingBucket, key, &seen)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if b.OpenID == "" {
			if time.Now().After(b.ExpiresAt) || subtle.ConstantTimeCompare([]byte(hash(strings.TrimSpace(m.Content))), []byte(b.PairHash)) != 1 {
				return nil
			}
			if s.options.CheckBinding != nil {
				if err = s.options.CheckBinding(ctx, b.OwnerID, b.ConversationID); err != nil {
					return err
				}
			}
			b.OpenID = m.OpenID
			b.PairHash = ""
			b.ExpiresAt = time.Time{}
			b.ConnectedAt = time.Now().UTC()
			b.State = "connected"
		} else {
			if b.OpenID != m.OpenID {
				return nil
			}
			text := m.Content
			if m.Attachments {
				text += "\n[QQ 附件尚未读取，请在 Molr 上传文件。 / QQ attachment not read; upload it in Molr.]"
			}
			if len(text) > 60000 {
				text = splitText(text)[0] + "\n[消息过长，已截断。]"
			}
			if strings.TrimSpace(text) == "" {
				return nil
			}
			if s.options.Receive == nil {
				return core.NewError(core.Unavailable, "receiver unavailable")
			}
			if _, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "qq-" + key}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text}}); err != nil {
				return err
			}
			now := time.Now().UTC()
			b.LastInboundAt = &now
			b.ReplyID = m.MessageID
			b.ReplyAt = now
			b.ReplySeq = 0
		}
		if err = tx.Put(ctx, incomingBucket, key, true); err != nil {
			return err
		}
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
}
func (s *Service) setState(ctx context.Context, initial Binding, state, reason string) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if b.ID != initial.ID || b.State == state && b.LastError == reason {
			return nil
		}
		if state == "connected" && b.OpenID == "" {
			state = "pairing"
		}
		b.State = state
		b.LastError = reason
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		return s.notify(ctx, b)
	})
}
func (s *Service) listen(ctx context.Context, b Binding) {
	policy := retry.Policy{}
	failures := 0
	for ctx.Err() == nil {
		c, err := s.credentials(b)
		if err != nil {
			_ = s.setState(ctx, b, "credentials_required", "credentials_required")
			return
		}
		err = s.options.Client.Listen(ctx, c, func(m Incoming) error {
			receiveCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			return s.receive(receiveCtx, b, m)
		}, func(connected bool) {
			if ctx.Err() != nil {
				return
			}
			if connected {
				_ = s.setState(ctx, b, "connected", "")
			} else {
				_ = s.setState(ctx, b, "reconnecting", "connection_lost")
			}
		})
		if ctx.Err() != nil {
			return
		}
		if failure(err) == "credentials_required" {
			_ = s.setState(ctx, b, "credentials_required", "credentials_required")
			return
		}
		_ = s.setState(ctx, b, "reconnecting", "connection_lost")
		failures++
		if !sleep(ctx, policy.Delay(failures)) {
			return
		}
	}
}
func (s *Service) recover(ctx context.Context) error {
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

// Run owns all listeners and the delivery worker, and waits for them before returning.
func (s *Service) Run(ctx context.Context) error {
	if err := s.recover(ctx); err != nil {
		return err
	}
	err := s.runBindings(ctx)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func (s *Service) deliver(ctx context.Context, d Delivery) error {
	unlock, err := s.lock(ctx, d.OwnerID, d.AssistantID)
	if err != nil {
		return err
	}
	defer unlock()
	var b Binding
	send := false
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := persistence.Load[Delivery](ctx, s.store, deliveriesBucket, persistence.Key(d.OwnerID, d.ID))
		if err != nil {
			return err
		}
		d = current
		if d.State != "pending" {
			return nil
		}
		b, err = s.binding(ctx, d.OwnerID, d.ConversationID)
		if persistence.Missing(err) || err == nil && b.ID != d.BindingID {
			d.State = "canceled"
			return s.saveDelivery(ctx, tx, d)
		}
		if err != nil {
			return err
		}
		if s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil || b.State == "credentials_required" {
			return nil
		}

		if time.Now().Before(b.NextSend) {
			d.NextAttempt = b.NextSend
			return s.saveDelivery(ctx, tx, d)
		}

		if d.FirstAttempt.IsZero() {
			d.FirstAttempt = time.Now().UTC()
		}
		// Use a conservative five-minute reply window until QQ's conflicting docs are resolved.
		if d.ReplyID == "" && b.ReplyID != "" && time.Since(b.ReplyAt) < 4*time.Minute && b.ReplySeq < 4 {
			b.ReplySeq++
			d.ReplyID = b.ReplyID
			d.ReplySeq = b.ReplySeq
		}
		b.NextSend = time.Now().Add(3 * time.Second)
		if err = tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		d.State = "sending"
		d.Attempts++
		send = true
		return s.saveDelivery(ctx, tx, d)
	})
	if err != nil || !send {
		return err
	}
	c, err := s.credentials(b)
	var remote string
	if err == nil {
		remote, err = s.options.Client.Send(ctx, c, d.ReplyID, d.ReplySeq, d.Text)
	}
	// A canceled network call may have succeeded remotely; unknown results are not retried.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.store.Write(finishCtx, func(ctx context.Context, tx persistence.Tx) error {
		if err == nil {
			d.State = "accepted"
			d.Error = ""
			d.RemoteID = remote
		} else {
			d.Error = failure(err)
			d.State = "failed"
			if d.Error == "unknown" {
				d.State = "unknown"
			}
			if d.Error == "rate_limited" {
				if d.Attempts < 5 {
					d.State = "pending"
					delay := time.Minute * time.Duration(d.Attempts)
					var remoteError *apiError
					if errors.As(err, &remoteError) && remoteError.retryAfter > delay {
						delay = remoteError.retryAfter
					}
					d.NextAttempt = time.Now().Add(delay)
				}
			}
			if d.Error == "credentials_required" {
				if e := s.setState(ctx, b, "credentials_required", d.Error); e != nil {
					return e
				}
			}
		}
		return s.saveDelivery(ctx, tx, d)
	})
}
