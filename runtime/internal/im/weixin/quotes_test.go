package weixin

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
)

func TestQuotesRestoreInboundOutboundAndPartialText(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	source := incoming("18446744073709551615", "第一段：苹果。第二段：香蕉。")
	quoted := incoming("2", "修改这一段")
	partial := "第二段：香蕉。"
	sum := md5.Sum([]byte(partial))
	quoted.Items[0].Ref = &Reference{ID: json.RawMessage(`"18446744073709551615"`), Partial: &PartialQuote{Start: "第二段", End: "。", EndIndex: 1, MD5: hex.EncodeToString(sum[:])}}
	// Same-batch references are resolved before the batch transaction commits.
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{source, quoted}}))
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	text := messages.Items[1].Content.Text
	if !strings.Contains(text, "修改这一段") || !strings.Contains(text, partial) || strings.Contains(text, "苹果") {
		t.Fatal(text)
	}
	f.client.serverID = "18446744073709551614"
	f.publish(t, "reply", "这个是助理的答复")
	f.s.deliverNext(ctx)
	again := incoming("3", "继续")
	again.Items[0].Ref = &Reference{ID: json.RawMessage(`"18446744073709551614"`)}
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{again}}))
	messages, err = f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if !strings.Contains(messages.Items[len(messages.Items)-1].Content.Text, "这个是助理的答复") {
		t.Fatal("outbound quote lost")
	}
}
func TestQuotesInlineMediaAndBindingIsolation(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	source := incoming("1", "a photo")
	source.Items = append(source.Items, Item{Type: 2})
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{source}}))
	for _, ref := range []*Reference{
		{ID: json.RawMessage(`"1"`)},
		{Title: "微信内嵌引用", Item: &Item{Type: 2}},
	} {
		quoted := incoming(`"quote-`+string(newID())+`"`, "看这张")
		quoted.Items[0].Ref = ref
		must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{quoted}}))
	}
	messages, err := f.chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	for _, m := range messages.Items[1:] {
		if len(m.Content.AttachmentIDs) != 1 || !strings.Contains(m.Content.Text, "引用附件") {
			t.Fatal(m.Content)
		}
	}
	// A restarted adapter uses the persisted mapping.
	options := f.s.options
	options.MasterKey = bytes.Repeat([]byte{1}, 32)
	restarted, err := New(f.db, options)
	must(t, err)
	ref := incoming("4", "继续")
	ref.Items[0].Ref = &Reference{ID: json.RawMessage(`"1"`)}
	text, ids, err := restarted.quotedContent(ctx, b, ref, "restart", nil)
	must(t, err)
	if !strings.Contains(text, "a photo") || len(ids) != 1 {
		t.Fatal("quote did not survive restart")
	}
	foreign := b
	foreign.ID = "other-binding"
	text, ids, err = restarted.quotedContent(ctx, foreign, ref, "foreign", nil)
	must(t, err)
	if strings.Contains(text, "a photo") || len(ids) != 0 || !strings.Contains(text, "未缓存") {
		t.Fatal("cross-binding quote exposed", text)
	}
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, quotesBucket, quoteKey(b, "1"), quoteRecord{BindingID: b.ID, Text: "expired", CreatedAt: time.Now().Add(-31 * 24 * time.Hour)})
	}))
	text, _, err = restarted.quotedContent(ctx, b, ref, "expired", nil)
	must(t, err)
	if strings.Contains(text, "expired") {
		t.Fatal("expired quote used")
	}
}
func TestPartialQuoteDoesNotGuessOnChecksumMismatch(t *testing.T) {
	text := "重复开头，重复结尾。重复开头，另一个结尾。"
	value, ok := partialText(text, &PartialQuote{Start: "重复开头", End: "结尾", StartIndex: 1, EndIndex: 1})
	if !ok || value != "重复开头，另一个结尾" {
		t.Fatal(value)
	}
	value, ok = partialText(text, &PartialQuote{Start: "重复开头", End: "结尾", MD5: strings.Repeat("0", 32)})
	if ok || value != text {
		t.Fatal("guessed a mismatched selection")
	}
}
func TestRecognizeExistingBindingPreservesContextAndPendingReplies(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	must(t, f.s.ingest(ctx, b, Updates{Messages: []Incoming{incoming("1", "hello")}, Cursor: "cursor"}))
	f.publish(t, "reply", "pending")
	login, err := f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	if login.Status != "already_connected" || login.QRCode != "" {
		t.Fatal(login)
	}
	f.s.setFailure(ctx, b, "unknown")
	login, err = f.s.StartLogin(ctx, f.owner, "assistant")
	must(t, err)
	if len(f.client.qrTokens) != 1 || f.client.qrTokens[0] != "SECRET-BOT-TOKEN" {
		t.Fatal("existing token not scoped to target")
	}
	f.client.result = LoginResult{Status: "binded_redirect"}
	result, err := f.s.PollLogin(ctx, f.owner, "assistant", PollRequest{ID: login.ID})
	must(t, err)
	if result.Status != "already_connected" {
		t.Fatal(result)
	}
	current, err := f.s.binding(ctx, "owner", "chat")
	must(t, err)
	secret, err := f.s.open(current)
	must(t, err)
	if current.ID != b.ID || current.State != "connected" || secret.Context != "SECRET-CONTEXT" || secret.Cursor != "cursor" {
		t.Fatal("recognition replaced binding or context")
	}
	status, err := f.s.Status(ctx, f.owner, "assistant")
	must(t, err)
	if status.Deliveries[0].State != "pending" {
		t.Fatal("recognition canceled pending reply")
	}
}
func TestAlreadyBoundResponseNeedsMatchingLiveCredential(t *testing.T) {
	for _, scenario := range []string{"no-local-binding", "other-bot", "stale-credential", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ctx := t.Context()
			if scenario != "no-local-binding" {
				b := f.bind(t)
				f.s.setFailure(ctx, b, "unknown")
			}
			login, err := f.s.StartLogin(ctx, f.owner, "assistant")
			must(t, err)
			f.client.result = LoginResult{Status: "binded_redirect"}
			if scenario == "other-bot" {
				f.client.result.BotID = "other"
			}
			if scenario == "stale-credential" {
				f.client.checkError = &apiError{Code: -14}
			}
			if scenario == "revoked" {
				f.live = false
			}
			result, err := f.s.PollLogin(ctx, f.owner, "assistant", PollRequest{ID: login.ID})
			if err == nil && result.Status != "conflict" {
				t.Fatal("unverified credential reported connected", result)
			}
		})
	}
}

func TestQuoteCacheBoundAndDisconnectCleanup(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	b := f.bind(t)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := savesQuote(f.s, ctx, tx, b, "old"); err != nil {
			return err
		}
		if err := tx.Put(ctx, "weixin.quote_cursors", persistence.Key(b.OwnerID, b.ID), uint64(4096)); err != nil {
			return err
		}
		return savesQuote(f.s, ctx, tx, b, "new")
	}))
	if _, err := persistence.Load[quoteRecord](ctx, f.db, quotesBucket, quoteKey(b, "old")); !persistence.Missing(err) {
		t.Fatal("ring did not evict old entry")
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	rows, err := persistence.Scan[quoteRecord](ctx, f.db, quotesBucket)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("quote cache survived unbinding")
	}
}
func savesQuote(s *Service, ctx context.Context, tx persistence.Tx, b Binding, id string) error {
	return s.saveQuote(ctx, tx, b, id, quoteRecord{BindingID: b.ID, Text: id, CreatedAt: time.Now().UTC()})
}

func TestVoiceIngressWithoutTranscriptKeepsAudioAttachment(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	m := incoming("1", "")
	m.Items = []Item{{Type: 3}}
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{m}}))
	messages, err := f.chat.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "chat"})
	must(t, err)
	if len(messages.Items[0].Content.AttachmentIDs) != 1 || !strings.Contains(messages.Items[0].Content.Text, "音频作为附件") {
		t.Fatal("voice dropped")
	}
	before := f.client.downloads
	m.ID = json.RawMessage("2")
	m.Items[0].Voice.Text = "微信已有转写"
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{m}}))
	if f.client.downloads != before {
		t.Fatal("transcribed voice downloaded twice")
	}
}
