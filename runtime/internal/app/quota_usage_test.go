package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"testing"
	"time"
)

func TestUsageViewsMatchAllowanceAcrossPeriods(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	now := time.Now().UTC()
	old := now.AddDate(0, -2, 0)
	rows := []struct {
		id, state string
		at        time.Time
	}{{"unknown", "unknown", old}, {"pending", "running", old}, {"done", "succeeded", now}, {"prepared", "prepared", now}, {"old", "succeeded", old}}
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, row := range rows {
			id := core.ID(row.id)
			if err := tx.Put(ctx, "agent.operations", persistence.Key(f.alice.OwnerID, id), map[string]any{"ID": id, "OwnerID": f.alice.OwnerID, "Action": "conversation_read", "State": row.state, "CreatedAt": row.at, "Arguments": "private quota fixture"}); err != nil {
				return err
			}
		}
		for _, state := range []string{"ready", "failed"} {
			if err := tx.Put(ctx, "storage.files", persistence.Key(f.alice.OwnerID, core.ID(state)), map[string]any{"OwnerID": f.alice.OwnerID, "State": state, "Size": 50}); err != nil {
				return err
			}
		}
		return nil
	}))
	identity := f.b.Services.Auth.(*auth.Manager)
	view, err := accountQuotas(ctx, f.b.store, identity, f.alice)
	need(t, err)
	found := false
	for _, v := range view.Resources {
		if v.Kind == "tool" && v.ID == "conversation_read" {
			found = true
			if v.Calls != 2 || v.Active != 1 || v.Unknown != 0 || v.Carried != 1 {
				t.Fatalf("wrong period: %+v", v)
			}
		}
	}
	if !found {
		t.Fatal("missing allowance")
	}
	call := tools.Call{Name: "conversation_read", Execution: core.Execution{Principal: f.alice, OperationID: "new"}}
	if err := checkGroupToolQuota(ctx, f.b.store, call); core.ReasonOf(err) != "group_quota_exceeded" {
		t.Fatal("view and admission disagree", err)
	}
	all, err := adminUsage(ctx, f.b.store, identity, f.admin)
	need(t, err)
	for _, u := range all.Users {
		if u.UserID == f.alice.OwnerID {
			if u.ToolCalls != view.Summary.ToolCalls || u.Unknown != 1 || u.AttachmentBytes != 50 {
				t.Fatal(u, view.Summary)
			}
		}
	}
	raw, err := json.Marshal(all)
	need(t, err)
	if strings.Contains(string(raw), "private quota fixture") {
		t.Fatal("private payload exposed")
	}
	if _, err := adminUsage(ctx, f.b.store, identity, f.alice); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
}

func TestContextUsageAccountScopeAndGlobalPrivacy(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	identity := f.b.Services.Auth.(*auth.Manager)
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		data := map[string]any{"Inputs": []any{map[string]any{"Role": "user", "Text": "PRIVATE-context-usage-fixture"}}}
		for _, owner := range []core.ID{f.alice.OwnerID, f.admin.OwnerID} {
			if err := contextstore.Save(ctx, tx, contextstore.Reference{OwnerID: owner, AssistantID: "usage-assistant", Kind: "test", ID: "usage"}, data); err != nil {
				return err
			}
		}
		return nil
	}))
	personal, err := accountQuotas(ctx, f.b.store, identity, f.alice)
	need(t, err)
	adminAccount, err := accountQuotas(ctx, f.b.store, identity, f.admin)
	need(t, err)
	global, err := adminUsage(ctx, f.b.store, identity, f.admin)
	need(t, err)
	if personal.ContextStorage.Bytes <= 0 || personal.ContextStorage.Messages != 1 || personal.ContextStorage != adminAccount.ContextStorage || global.ContextStorage != personal.ContextStorage {
		t.Fatal(personal.ContextStorage, adminAccount.ContextStorage, global.ContextStorage)
	}
	raw, err := json.Marshal(global)
	need(t, err)
	if strings.Contains(string(raw), "PRIVATE-context") || strings.Contains(string(raw), "usage-assistant") {
		t.Fatal("context details exposed")
	}
	if _, err := adminUsage(ctx, f.b.store, identity, f.alice); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
}

func TestLifetimeUserMessagesCountSuccessfulSends(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	identity := f.b.Services.Auth.(*auth.Manager)
	assertCounts := func(want map[core.ID]int64) {
		t.Helper()
		global, err := adminUsage(ctx, f.b.store, identity, f.admin)
		need(t, err)
		if len(global.Users) != len(want) {
			t.Fatalf("users = %d, want %d", len(global.Users), len(want))
		}
		for _, row := range global.Users {
			if row.UserMessages != want[row.UserID] {
				t.Fatalf("%s messages = %d, want %d", row.Username, row.UserMessages, want[row.UserID])
			}
		}
		for _, p := range []core.Principal{f.alice, f.bob, f.admin} {
			view, err := accountQuotas(ctx, f.b.store, identity, p)
			need(t, err)
			if view.Summary.UserMessages != want[p.OwnerID] {
				t.Fatalf("personal messages = %d, want %d", view.Summary.UserMessages, want[p.OwnerID])
			}
		}
		raw, err := json.Marshal(global)
		need(t, err)
		if strings.Contains(string(raw), "PRIVATE-message-count") {
			t.Fatal("message content exposed")
		}
	}
	want := map[core.ID]int64{f.alice.OwnerID: 0, f.bob.OwnerID: 0, f.admin.OwnerID: 0}
	assertCounts(want)
	a := f.assistant(t, f.alice, "count-alice")
	a2 := f.assistant(t, f.alice, "count-alice-two")
	admin := f.assistant(t, f.admin, "count-admin")
	for _, item := range []struct {
		p    core.Principal
		chat core.ID
	}{{f.alice, a.ConversationID}, {f.alice, a2.ConversationID}, {f.admin, admin.ConversationID}} {
		req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "count-send"}, ConversationID: item.chat, Content: conversation.Content{Kind: conversation.Text, Text: "PRIVATE-message-count"}}
		first, err := f.b.Services.Conversations.SendUser(ctx, item.p, req)
		need(t, err)
		again, err := f.b.Services.Conversations.SendUser(ctx, item.p, req)
		need(t, err)
		if first.ID != again.ID {
			t.Fatal("send retry created another message")
		}
		want[item.p.OwnerID]++
	}
	execution := f.execution(t, f.alice, a, "count-reply")
	_, err := f.b.Services.Conversations.(conversation.Publisher).Publish(ctx, execution, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: "count-reply"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "PRIVATE-message-count-reply"}})
	need(t, err)
	_, err = f.b.Services.Conversations.SendUser(ctx, f.bob, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "forbidden-count"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "PRIVATE-message-count-forbidden"}})
	if err == nil {
		t.Fatal("foreign send succeeded")
	}
	_, err = f.b.Services.Assistants.Clone(ctx, f.alice, assistant.CloneRequest{Mutation: core.Mutation{IdempotencyKey: "count-clone"}, SourceID: a.ID, Name: "Count clone", CopyMessages: true})
	need(t, err)
	_, err = f.b.Services.Assistants.Dismiss(ctx, f.alice, assistant.LifecycleRequest{Mutation: core.Mutation{IdempotencyKey: "count-dismiss", ExpectedVersion: a2.Version}, ID: a2.ID})
	need(t, err)
	assertCounts(want)
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		_, err := tx.DeleteMatching(ctx, "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", f.alice.OwnerID)}})
		return err
	}))
	assertCounts(want)
}
