package dingtalk

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/retry"
)

type Delivery struct {
	ID, BindingID, OwnerID, AssistantID, ConversationID, MessageID, FileID core.ID
	Sequence                                                               uint64
	Part, Attempts                                                         int
	Text, State, Error                                                     string
	CreatedAt, NextAttempt                                                 time.Time
}

func splitText(text string, limit int) []string {
	var parts []string
	for len(text) > 0 {
		n := min(len(text), limit)
		for n < len(text) && n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		parts = append(parts, text[:n])
		text = text[n:]
	}
	return parts
}
func (s *Service) Published(ctx context.Context, owner core.ID, m conversation.Message) error {
	b, err := s.binding(ctx, owner, m.ConversationID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.UserID == "" {
		return nil
	}
	text := m.Content.Text
	if m.Content.Card != nil {
		text = m.Content.Card.Title + "\n" + m.Content.Card.Summary + "\n" + strings.TrimRight(s.options.PublicURL, "/") + "/#/assistants/" + string(b.AssistantID)
	}
	return s.enqueue(ctx, b, m.ID, m.Sequence, text, m.Content.AttachmentIDs)
}
func (s *Service) enqueue(ctx context.Context, b Binding, message core.ID, sequence uint64, text string, attachments []core.ID) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		parts := splitText(text, 3500)
		for part := 0; part < len(parts)+len(attachments); part++ {
			d := Delivery{ID: core.ID(hash(string(b.ID) + "/" + string(message) + "/" + strconv.Itoa(part))), BindingID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, MessageID: message, Sequence: sequence, Part: part, State: "pending", CreatedAt: time.Now().UTC()}
			if part < len(parts) {
				d.Text = parts[part]
			} else {
				d.FileID = attachments[part-len(parts)]
			}
			if err := s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Service) saveDelivery(ctx context.Context, tx persistence.Tx, d Delivery) error {
	if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
		return err
	}
	if d.State == "sending" || d.State == "pending" && d.Error == "" {
		return nil
	}
	return notification.Sync(ctx, s.store, d.OwnerID, notification.Input{Kind: "dingtalk.delivery", Code: d.State + ":" + d.Error, Source: core.Object{Kind: "dingtalk_delivery", ID: d.ID}, AssistantID: d.AssistantID}, d.State != "accepted" && d.State != "canceled")
}
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
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
	type running struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	workers := map[core.ID]running{}
	var wg sync.WaitGroup
	defer func() {
		for _, w := range workers {
			w.cancel()
		}
		wg.Wait()
	}()
	deliveryCtx, cancelDelivery := context.WithCancel(ctx)
	defer cancelDelivery()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for deliveryCtx.Err() == nil {
			_ = s.deliverNext(deliveryCtx)
			if !sleep(deliveryCtx, time.Second) {
				return
			}
		}
	}()
	// Stop delivery worker before the outer wait even if a database query fails.
	defer cancelDelivery()
	for ctx.Err() == nil {
		rows, err := persistence.Scan[Binding](ctx, s.store, bindingsBucket)
		if err != nil {
			return err
		}
		active := map[core.ID]bool{}
		for _, b := range rows {
			if b.UserID == "" && time.Now().After(b.ExpiresAt) {
				_ = s.expire(ctx, b)
				continue
			}
			if b.State == "credential_required" || s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
				continue
			}
			active[b.ID] = true
			if _, ok := workers[b.ID]; !ok {
				runCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})
				workers[b.ID] = running{cancel, done}
				wg.Add(1)
				go func(b Binding) { defer wg.Done(); defer close(done); s.streamLoop(runCtx, b) }(b)
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
		if current.ID != b.ID || current.UserID != "" {
			return nil
		}
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
}
func (s *Service) streamLoop(ctx context.Context, b Binding) {
	secret, err := s.open(b)
	if err != nil {
		_ = s.connectionState(ctx, b, "credential_required", "credential_required")
		return
	}
	failures := 0
	for ctx.Err() == nil {
		err = s.options.Client.Stream(ctx, Credentials{b.ClientID, secret}, func() error { failures = 0; return s.connectionState(ctx, b, "connected", "") }, func(ctx context.Context, m Incoming) error { return s.receive(ctx, b, m) })
		if ctx.Err() != nil {
			return
		}
		reason := failure(err)
		if reason == "credential_required" || reason == "rejected" {
			_ = s.connectionState(ctx, b, "credential_required", reason)
			return
		}
		_ = s.connectionState(ctx, b, "reconnecting", "network")
		failures++
		delay := (retry.Policy{}).Delay(failures)
		var api *apiError
		if errors.As(err, &api) {
			delay = max(delay, api.RetryAfter)
		}
		if !sleep(ctx, delay) {
			return
		}
	}
}
func (s *Service) deliverNext(ctx context.Context) error {
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "pending")}})
	if err != nil {
		return err
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.BindingID == b.BindingID {
			if a.Sequence != b.Sequence {
				return a.Sequence < b.Sequence
			}
			if a.MessageID == b.MessageID {
				return a.Part < b.Part
			}
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	visited := map[core.ID]bool{}
	for _, d := range rows {
		if visited[d.BindingID] {
			continue
		}
		visited[d.BindingID] = true
		if time.Now().Before(d.NextAttempt) {
			continue
		}
		if err = s.deliver(ctx, d); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}
func (s *Service) deliver(ctx context.Context, snapshot Delivery) error {
	unlock, err := s.lock(ctx, snapshot.OwnerID, snapshot.AssistantID)
	if err != nil {
		return err
	}
	defer unlock()
	var d Delivery
	var b Binding
	send := false
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		d, err = persistence.Load[Delivery](ctx, s.store, deliveriesBucket, persistence.Key(snapshot.OwnerID, snapshot.ID))
		if err != nil {
			return err
		}
		if d.State != "pending" || time.Now().Before(d.NextAttempt) {
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
		if b.State == "credential_required" || s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			return nil
		}
		d.State = "sending"
		d.Attempts++
		send = true
		return s.saveDelivery(ctx, tx, d)
	})
	if err != nil || !send {
		return err
	}
	secret, err := s.open(b)
	if err == nil {
		creds := Credentials{b.ClientID, secret}
		if d.FileID != "" {
			err = s.sendMedia(ctx, b, creds, d.FileID)
		} else {
			err = s.options.Client.Send(ctx, creds, b.UserID, "sampleMarkdown", map[string]any{"title": "Molr", "text": d.Text})
		}
	}
	reason := ""
	if err != nil {
		reason = failure(err)
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, e := persistence.Load[Delivery](ctx, s.store, deliveriesBucket, persistence.Key(d.OwnerID, d.ID))
		if e != nil {
			return e
		}
		if current.State != "sending" {
			return nil
		}
		d.Error = reason
		d.State = "failed"
		switch reason {
		case "":
			d.State = "accepted"
		case "unknown":
			d.State = "unknown"
		case "rate_limited":
			if d.Attempts < 5 {
				d.State = "pending"
				delay := time.Minute
				var api *apiError
				if errors.As(err, &api) {
					delay = max(delay, api.RetryAfter)
				}
				d.NextAttempt = time.Now().Add(delay)
			}
		case "credential_required":
			d.State = "pending"
			if e = s.connectionState(ctx, b, "credential_required", reason); e != nil {
				return e
			}
		}
		return s.saveDelivery(ctx, tx, d)
	})
}
