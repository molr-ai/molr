package app

import (
	"context"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type channelReadStore struct {
	persistence.Store
	reads map[string]int
}
type channelReadTx struct {
	persistence.Tx
	reads map[string]int
}

func (s channelReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, channelReadTx{tx, s.reads}) })
}
func (tx channelReadTx) Store() persistence.Store { return channelReadStore{tx.Tx.Store(), tx.reads} }
func (tx channelReadTx) Get(ctx context.Context, bucket, key string, out any) error {
	tx.reads[bucket]++
	return tx.Tx.Get(ctx, bucket, key, out)
}
func (tx channelReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	tx.reads[bucket]++
	return tx.Tx.Query(ctx, bucket, q)
}

func TestChannelContextSharesReadsAndRefreshesAuthorization(t *testing.T) {
	b, owner, e := taskFixture(t)
	e.TaskID = "context-task"
	currentTask := task.Task{ID: e.TaskID, OwnerID: owner.OwnerID, AssistantID: e.AssistantID, State: task.Running}
	saveTask := func() {
		t.Helper()
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "business.tasks", persistence.Key(owner.OwnerID, e.TaskID), currentTask)
		}))
	}
	saveTask()
	reads := map[string]int{}
	store := channelReadStore{b.store, reads}
	docs, err := b.channelContext(t.Context(), store, e)
	need(t, err)
	if len(docs) != 0 || reads["business.assistants"] != 1 || reads["agent.jobs"] != 1 || reads["business.tasks"] != 1 {
		t.Fatal(docs, reads)
	}
	// Each context boundary loads fresh state, including task cancellation.
	currentTask.State = task.Paused
	saveTask()
	if _, err = b.channelContext(t.Context(), store, e); core.CodeOf(err) != core.Forbidden {
		t.Fatal("paused task accepted", err)
	}
	currentTask.State = task.Running
	saveTask()
	forged := e
	forged.Principal.ExecutionID = "forged"
	if _, err = b.channelContext(t.Context(), store, forged); core.CodeOf(err) != core.Forbidden {
		t.Fatal("forged execution accepted", err)
	}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "shell-job"))
	}))
	if _, err = b.channelContext(t.Context(), store, e); core.CodeOf(err) != core.Forbidden {
		t.Fatal("ended execution accepted", err)
	}
}

func TestChannelContextOrderSlackAndBindingChanges(t *testing.T) {
	b, owner, e := taskFixture(t)
	a, err := assistant.NewRepository(b.store).Get(t.Context(), owner.OwnerID, e.AssistantID)
	need(t, err)
	a.ConversationID = "context-chat"
	names := []string{"weixin", "qq", "feishu", "telegram", "dingtalk", "slack"}
	labels := []string{"Weixin", "QQ", "Feishu", "Telegram", "DingTalk", "Slack"}
	key := persistence.Key(owner.OwnerID, a.ConversationID)
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, a.ID), a); err != nil {
			return err
		}
		for _, name := range names {
			binding := map[string]any{"ID": "binding", "OwnerID": owner.OwnerID, "AssistantID": a.ID, "ConversationID": a.ConversationID, "State": "reconnecting", "ChannelID": "confirmed", "OpenID": "confirmed"}
			if name == "dingtalk" {
				binding["UserID"] = "confirmed"
			}
			if err := tx.Put(ctx, name+".bindings", key, binding); err != nil {
				return err
			}
		}
		return nil
	}))
	for i, name := range names {
		docs, err := b.channelContext(t.Context(), b.store, e)
		need(t, err)
		want := 2
		if name == "slack" {
			want = 1
		}
		if len(docs) != want || !strings.Contains(docs[0].Content, "Current chat channel: "+labels[i]) || !strings.Contains(docs[len(docs)-1].Content, "Current chat channel: Slack") {
			t.Fatal(name, docs)
		}
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Delete(ctx, name+".bindings", key) }))
	}
	docs, err := b.channelContext(t.Context(), b.store, e)
	need(t, err)
	if len(docs) != 0 {
		t.Fatal("disconnected channel retained", docs)
	}
	// A failed channel query stops composition, including the independent Slack source.
	for _, name := range []string{"weixin", "slack"} {
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, name+".bindings", key, "invalid binding")
		}))
		if docs, err := b.channelContext(t.Context(), b.store, e); err == nil || len(docs) != 0 {
			t.Fatal("storage failure hidden", name, docs, err)
		}
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Delete(ctx, name+".bindings", key) }))
	}
	a.State = assistant.Dismissed
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, a.ID), a)
	}))
	if _, err := b.channelContext(t.Context(), b.store, e); core.CodeOf(err) != core.Forbidden {
		t.Fatal("retired assistant accepted", err)
	}
}
