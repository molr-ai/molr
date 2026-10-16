package slack

import (
	"context"
	"errors"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"
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

func (s *Service) deliverNext(ctx context.Context) {
	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("State", "pending")}, {persistence.Eq("State", "waiting")}, {persistence.Eq("State", "sending")}}})
	if err != nil {
		return
	}
	sortDeliveries(rows)
	blocked := map[core.ID]bool{}
	for _, d := range rows {
		if d.State == "waiting" || d.State == "sending" {
			blocked[d.BindingID] = true
			continue
		}
		if d.State != "pending" || blocked[d.BindingID] {
			continue
		}
		blocked[d.BindingID] = true
		if time.Now().Before(d.NextAttempt) {
			continue
		}
		s.deliver(ctx, d)
	}
}
func (s *Service) deliver(ctx context.Context, d Delivery) {
	g := s.gate(d.OwnerID, d.AssistantID)
	if err := g.acquire(ctx); err != nil {
		return
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
		if b.ChannelID == "" {
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
		return
	}
	var serverID string
	sendctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	if d.FileID == "" {
		serverID, err = s.options.Client.Send(sendctx, secret.Token, b.ChannelID, d.Text)
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
				delay := time.Minute
				var api *apiError
				if errors.As(err, &api) && api.RetryAfter > delay {
					delay = api.RetryAfter
				}
				d.NextAttempt = time.Now().Add(delay)
			}
		case "unknown":
			d.State = "unknown"
		default:
			d.State = "failed"
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

func (s *Service) sendAttachment(ctx context.Context, b Binding, secret secrets, d Delivery) (string, error) {
	if s.options.Files == nil {
		return "", &apiError{Code: "attachment_failed"}
	}
	file, err := s.options.Files.OpenChannel(ctx, b.OwnerID, b.ConversationID, b.ID, d.FileID)
	if err != nil {
		return "", &apiError{Code: "attachment_failed"}
	}
	defer file.Body.Close()
	if file.File.Size > s.options.MaxMediaBytes {
		return "", &apiError{Code: "attachment_failed"}
	}
	data, err := io.ReadAll(io.LimitReader(file.Body, s.options.MaxMediaBytes+1))
	if err != nil || int64(len(data)) > s.options.MaxMediaBytes {
		return "", &apiError{Code: "attachment_failed"}
	}
	return s.options.Client.Upload(ctx, secret.Token, b.ChannelID, Media{Name: file.File.Name, Type: file.File.MediaType, Data: data})
}
