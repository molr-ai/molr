package slack

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strconv"
	"time"
	"unicode/utf8"
)

type quoteRecord struct {
	BindingID core.ID
	Text      string
	Files     []core.ID
	CreatedAt time.Time
}

const quotesBucket = "slack.quotes"

func quoteKey(b Binding, id string) string {
	return persistence.Key(b.OwnerID, hash(string(b.ID)+"/"+id))
}
func boundedText(s string) string {
	if len(s) <= 16384 {
		return s
	}
	n := 16384
	for !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
func (s *Service) saveQuote(ctx context.Context, tx persistence.Tx, b Binding, id string, record quoteRecord) error {
	if id == "" {
		return nil
	}
	key := quoteKey(b, id)
	// A fixed-size ring bounds lookup metadata without duplicating attachment bytes.
	if _, err := persistence.Load[quoteRecord](ctx, s.store, quotesBucket, key); persistence.Missing(err) {
		scope := persistence.Key(b.OwnerID, b.ID)
		cursor, err := persistence.Load[uint64](ctx, s.store, "slack.quote_cursors", scope)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		slot := scope + "/" + strconv.FormatUint(cursor%4096, 10)
		old, err := persistence.Load[string](ctx, s.store, "slack.quote_slots", slot)
		if err == nil {
			if err := tx.Delete(ctx, quotesBucket, old); err != nil {
				return err
			}
		} else if !persistence.Missing(err) {
			return err
		}
		if err := tx.Put(ctx, "slack.quote_slots", slot, key); err != nil {
			return err
		}
		if err := tx.Put(ctx, "slack.quote_cursors", scope, cursor+1); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	record.Text = boundedText(record.Text)
	return tx.Put(ctx, quotesBucket, key, record)
}

func (s *Service) clearQuotes(ctx context.Context, tx persistence.Tx, b Binding) error {
	scope := persistence.Key(b.OwnerID, b.ID)
	slots, err := tx.Query(ctx, "slack.quote_slots", persistence.Query{Prefix: scope + "/"})
	if err != nil {
		return err
	}
	for _, slot := range slots {
		key, err := persistence.Decode[string](slot)
		if err != nil {
			return err
		}
		if err := tx.Delete(ctx, quotesBucket, key); err != nil {
			return err
		}
		if err := tx.Delete(ctx, "slack.quote_slots", slot.Key); err != nil {
			return err
		}
	}
	err = tx.Delete(ctx, "slack.quote_cursors", scope)
	if persistence.Missing(err) {
		return nil
	}
	return err
}
