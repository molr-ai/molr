package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
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
