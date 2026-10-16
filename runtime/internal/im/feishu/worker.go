package feishu

import (
	"context"
	"encoding/json"
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
		if n > 12000 {
			n = 12000
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
func incomingText(m Incoming) string {
	if len(m.Content) > 200000 {
		return "[飞书消息过长，内容未读取。请分段发送。]"
	}
	switch m.Type {
	case "text":
		var r struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(m.Content), &r) == nil {
			return r.Text
		}
	case "post":
		type post struct {
			Title   string                               `json:"title"`
			Content [][]struct{ Tag, Text, Href string } `json:"content"`
		}
		var p post
		if json.Unmarshal([]byte(m.Content), &p) == nil && p.Content == nil {
			var locales map[string]post
			if json.Unmarshal([]byte(m.Content), &locales) == nil {
				for _, locale := range []string{"zh_cn", "en_us", "ja_jp"} {
					if v, ok := locales[locale]; ok {
						p = v
						break
					}
				}
			}
		}
		var lines []string
		if p.Title != "" {
			lines = append(lines, p.Title)
		}
		for _, row := range p.Content {
			var line strings.Builder
			for _, node := range row {
				switch node.Tag {
				case "text", "a", "at", "md", "code_block":
					line.WriteString(node.Text)
					if node.Tag == "a" && node.Href != "" {
						line.WriteString(" (" + node.Href + ")")
					}
				case "img", "media":
					line.WriteString("[附件尚未读取]")
				}
			}
			lines = append(lines, line.String())
		}
		if text := strings.Join(lines, "\n"); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return "[收到暂不支持的飞书消息，内容未读取。此入口目前支持文字，请发送文字描述。]"
}
func (s *Service) receive(ctx context.Context, initial Binding, m Incoming) error {
	// An app-scoped Open ID is mandatory even before the first tenant is pinned.
	if m.AppID != initial.AppID || m.OpenID != initial.OpenID || m.SenderType != "user" || m.ChatType != "p2p" || m.MessageID == "" || m.Tenant == "" || len(m.MessageID) > 256 || len(m.Tenant) > 256 {
		return nil
	}
	text := incomingText(m)
	if len(text) > 65536 {
		text = "[飞书消息过长，内容未读取。请分段发送。]"
	}
	if strings.TrimSpace(text) == "" {
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
		if b.ID != initial.ID || b.AppID != m.AppID || b.OpenID != m.OpenID || b.Tenant != "" && b.Tenant != m.Tenant {
			return nil
		}
		if err = s.options.Live(ctx, b.OwnerID, b.AssistantID); err != nil {
			return nil
		}
		// Independent of binding generation: delayed redelivery after reconnect must not replay a command.
		key := string(hash(b.AppID + "/" + m.Tenant + "/" + m.MessageID))
		var seen struct{ ID string }
		err = tx.Get(ctx, incomingBucket, key, &seen)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if s.options.Receive == nil {
			return core.NewError(core.Unavailable, "channel receiver unavailable")
		}
		_, err = s.options.Receive.ReceiveChannel(ctx, b.OwnerID, b.ID, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "feishu-" + key}, ConversationID: b.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: text}})
		if err != nil {
			return err
		}
		if err = tx.Put(ctx, incomingBucket, key, struct{ ID string }{m.MessageID}); err != nil {
			return err
		}
		now := time.Now().UTC()
		b.LastInboundAt = &now
		b.Tenant = m.Tenant
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
			receiveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
			d.State = "pending"
			d.Error = "unknown"
			if d.FirstAttempt.IsZero() || time.Since(d.FirstAttempt) >= 55*time.Minute {
				d.State = "unknown"
			}
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
		if !d.FirstAttempt.IsZero() && time.Since(d.FirstAttempt) >= 55*time.Minute {
			d.State = "unknown"
			d.Error = "unknown"
			return s.saveDelivery(ctx, tx, d)
		}
		if d.FirstAttempt.IsZero() {
			d.FirstAttempt = time.Now().UTC()
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
		remote, err = s.options.Client.Send(ctx, c, string(d.ID)[:32], d.Text)
	}
	// A canceled network call can have succeeded remotely. The persisted UUID remains unchanged.
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
			if d.Error == "unknown" || d.Error == "rate_limited" {
				if d.Attempts < 5 {
					d.State = "pending"
					delay := time.Minute * time.Duration(d.Attempts)
					var remoteError *apiError
					if errors.As(err, &remoteError) && remoteError.retryAfter > delay {
						delay = remoteError.retryAfter
					}
					d.NextAttempt = time.Now().Add(delay)
				} else if d.Error == "unknown" {
					d.State = "unknown"
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
