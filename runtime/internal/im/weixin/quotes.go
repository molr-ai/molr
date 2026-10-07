package weixin

import (
	"context"
	"crypto/md5" // Partial-quote checksum in the Weixin protocol.
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
)

type Reference struct {
	ID      json.RawMessage `json:"svr_id"`
	Item    *Item           `json:"message_item"`
	Title   string          `json:"title"`
	Partial *PartialQuote   `json:"partial_text"`
}
type PartialQuote struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	StartIndex int    `json:"startindex"`
	EndIndex   int    `json:"endindex"`
	MD5        string `json:"quotemd5"`
}
type quoteRecord struct {
	BindingID core.ID
	Text      string
	Files     []core.ID
	CreatedAt time.Time
}

const quotesBucket = "weixin.quotes"

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
		cursor, err := persistence.Load[uint64](ctx, s.store, "weixin.quote_cursors", scope)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		slot := scope + "/" + strconv.FormatUint(cursor%4096, 10)
		old, err := persistence.Load[string](ctx, s.store, "weixin.quote_slots", slot)
		if err == nil {
			if err := tx.Delete(ctx, quotesBucket, old); err != nil {
				return err
			}
		} else if !persistence.Missing(err) {
			return err
		}
		if err := tx.Put(ctx, "weixin.quote_slots", slot, key); err != nil {
			return err
		}
		if err := tx.Put(ctx, "weixin.quote_cursors", scope, cursor+1); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	record.Text = boundedText(record.Text)
	return tx.Put(ctx, quotesBucket, key, record)
}
func messageText(m Incoming) string {
	parts := []string{}
	for _, item := range m.Items {
		if item.Type == 1 {
			parts = append(parts, item.Text.Text)
		} else if item.Type == 3 && item.Voice.Text != "" {
			parts = append(parts, "[微信语音转写] "+item.Voice.Text)
		}
	}
	return strings.Join(parts, "\n")
}
func nth(text, value string, index, from int) int {
	if value == "" || index < 0 || index > len(text) || from > len(text) {
		return -1
	}
	for n := 0; n <= index; n++ {
		i := strings.Index(text[from:], value)
		if i < 0 {
			return -1
		}
		from += i
		if n == index {
			return from
		}
		from += len(value)
	}
	return -1
}
func partialText(text string, p *PartialQuote) (string, bool) {
	if p == nil {
		return text, true
	}
	start := nth(text, p.Start, p.StartIndex, 0)
	if start < 0 {
		return text, false
	}
	for _, from := range []int{0, start + len(p.Start)} {
		end := nth(text, p.End, p.EndIndex, from)
		if end < start {
			continue
		}
		candidate := text[start : end+len(p.End)]
		if p.MD5 == "" {
			return candidate, true
		}
		sum := md5.Sum([]byte(candidate))
		if strings.EqualFold(hex.EncodeToString(sum[:]), p.MD5) {
			return candidate, true
		}
	}
	return text, false
}

func (s *Service) quotedContent(ctx context.Context, b Binding, m Incoming, key string, batch map[string]quoteRecord) (string, []core.ID, error) {
	var ref *Reference
	for _, item := range m.Items {
		if item.Ref != nil {
			ref = item.Ref
			break
		}
	}
	if ref == nil {
		return "", nil, nil
	}
	id := messageID(ref.ID)
	if id == "" && ref.Item != nil {
		id = messageID(ref.Item.ID)
	}
	record, found := batch[id]
	if !found && id != "" {
		var err error
		record, err = persistence.Load[quoteRecord](ctx, s.store, quotesBucket, quoteKey(b, id))
		if err != nil && !persistence.Missing(err) {
			return "", nil, err
		}
		found = err == nil
	}
	found = found && record.BindingID == b.ID && time.Since(record.CreatedAt) < 30*24*time.Hour
	text := boundedText(ref.Title)
	if found {
		text = record.Text
	} else if ref.Item != nil {
		if body := messageText(Incoming{Items: []Item{*ref.Item}}); body != "" {
			text = boundedText(body)
		}
	}
	ids := []core.ID{}
	unavailable := false
	if found && len(record.Files) > 0 {
		for i, id := range record.Files {
			if s.options.Files == nil {
				unavailable = true
				continue
			}
			// Copy only a cached file belonging to this binding. Channel ingress still
			// requires its own scoped upload receipt; external IDs cannot grant access.
			d, err := s.options.Files.OpenChannel(ctx, b.OwnerID, b.ConversationID, b.ID, id)
			if err != nil {
				unavailable = true
				continue
			}
			f, err := s.options.Files.UploadChannel(ctx, b.OwnerID, b.ConversationID, b.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "quote-" + string(hash(key+"/"+strconv.Itoa(i)))}, Name: d.File.Name, MediaType: d.File.MediaType, Size: d.File.Size, Body: d.Body})
			d.Body.Close()
			if err != nil {
				unavailable = true
				continue
			}
			ids = append(ids, f.ID)
		}
	} else if ref.Item != nil {
		item := *ref.Item
		item.Ref = nil
		prepared := s.receiveMedia(ctx, b, Incoming{Items: []Item{item}}, key+"/quote")
		ids = prepared.ids
		unavailable = prepared.failed
	}
	if text == "" && len(ids) == 0 {
		text = "引用内容未缓存或已不可用，不能确定原消息内容。"
	}
	if len(ids) > 0 {
		text += "\n[引用附件已关联到本条消息；需要读取附件后才能描述内容。]"
	}
	if unavailable {
		text += "\n[引用附件已不可用，内容未读取。]"
	}
	if ref.Partial != nil {
		var resolved bool
		text, resolved = partialText(text, ref.Partial)
		if !resolved {
			text = "[局部引用未能验证，以下为可用的原消息内容]\n" + text
		}
	}
	return "[微信引用消息]\n" + boundedText(text) + "\n[引用结束]", ids, nil
}

func (s *Service) clearQuotes(ctx context.Context, tx persistence.Tx, b Binding) error {
	scope := persistence.Key(b.OwnerID, b.ID)
	slots, err := tx.Query(ctx, "weixin.quote_slots", persistence.Query{Prefix: scope + "/"})
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
		if err := tx.Delete(ctx, "weixin.quote_slots", slot.Key); err != nil {
			return err
		}
	}
	err = tx.Delete(ctx, "weixin.quote_cursors", scope)
	if persistence.Missing(err) {
		return nil
	}
	return err
}
