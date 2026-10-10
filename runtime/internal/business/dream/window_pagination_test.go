package dream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/trigger"
)

func TestWindowsIncludeActiveAssistantAfterDismissedFirstPage(t *testing.T) {
	f := setup(t)
	err := f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 200; i++ {
			id := core.ID(fmt.Sprintf("a-%03d", i))
			if err := tx.Put(ctx, "business.assistants", persistence.Key("owner", id), assistant.Assistant{ID: id, OwnerID: "owner", State: assistant.Dismissed, CreatedAt: f.now}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, configs, "owner/assistant", Config{OwnerID: "owner", AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600, Version: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	for _, id := range []core.ID{"assistant", ""} {
		t.Run("assistant="+string(id), func(t *testing.T) {
			windows, err := f.s.Windows(t.Context(), f.p, id, from, to)
			if err != nil {
				t.Fatal(err)
			}
			if len(windows) != 1 || windows[0].AssistantID != "assistant" || !windows[0].Start.Equal(from.Add(3*time.Hour)) {
				t.Errorf("windows=%v, want the active assistant's 03:00 window", windows)
			}
			rule := trigger.Rule{StartAt: from.Add(3*time.Hour + 5*time.Minute), Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=1", MissedPolicy: trigger.Coalesce}
			conflicts, err := f.s.Conflicts(t.Context(), f.p, id, rule, from, to)
			if err != nil {
				t.Fatal(err)
			}
			if len(conflicts) != 1 {
				t.Errorf("conflicts=%v, want one overlapping dream window", conflicts)
			}
		})
	}
}

type denyWindowAssistant struct{ id core.ID }

func (d denyWindowAssistant) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if q.Target.ID == d.id {
		return core.NewError(core.Forbidden, "denied")
	}
	return (allow{}).Require(ctx, p, q)
}

func TestWindowsTraverseAllActiveAssistantPages(t *testing.T) {
	f := setup(t)
	err := f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, configs, "owner/assistant", Config{OwnerID: "owner", AssistantID: "assistant", Enabled: false, Timezone: "UTC"}); err != nil {
			return err
		}
		for i := 0; i < 401; i++ {
			id := core.ID(fmt.Sprintf("active-%03d", i))
			if err := tx.Put(ctx, "business.assistants", persistence.Key("owner", id), assistant.Assistant{ID: id, OwnerID: "owner", State: assistant.Active}); err != nil {
				return err
			}
			if err := tx.Put(ctx, configs, persistence.Key("owner", id), Config{OwnerID: "owner", AssistantID: id, Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	windows, err := f.s.Windows(t.Context(), f.p, "", from, from.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[core.ID]bool{}
	for _, w := range windows {
		if seen[w.AssistantID] {
			t.Fatalf("duplicate window for %s", w.AssistantID)
		}
		seen[w.AssistantID] = true
	}
	if len(windows) != 401 || !seen["active-400"] {
		t.Fatalf("got %d windows, last assistant included=%v", len(windows), seen["active-400"])
	}
	selected, err := f.s.Windows(t.Context(), f.p, "active-400", from, from.Add(24*time.Hour))
	if err != nil || len(selected) != 1 || selected[0].AssistantID != "active-400" {
		t.Fatal("selected assistant beyond first page", selected, err)
	}
	f.s.options.Authorizer = denyWindowAssistant{id: "active-400"}
	for _, id := range []core.ID{"active-400", ""} {
		if rows, err := f.s.Windows(t.Context(), f.p, id, from, from.Add(24*time.Hour)); core.CodeOf(err) != core.Forbidden || rows != nil {
			t.Fatalf("authorization on later page: rows=%d err=%v", len(rows), err)
		}
	}

}

func TestWindowsKeepActualSessionsAndOwnerScope(t *testing.T) {
	f := setup(t)
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	put(t, f.db, configs, "owner/assistant", Config{OwnerID: "owner", AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600})
	put(t, f.db, "business.assistants", "owner/dismissed", assistant.Assistant{ID: "dismissed", OwnerID: "owner", State: assistant.Dismissed})
	put(t, f.db, configs, "owner/dismissed", Config{OwnerID: "owner", AssistantID: "dismissed", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600})
	put(t, f.db, "business.assistants", "other/foreign", assistant.Assistant{ID: "foreign", OwnerID: "other", State: assistant.Active})
	put(t, f.db, configs, "other/foreign", Config{OwnerID: "other", AssistantID: "foreign", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600})
	put(t, f.db, sessions, "owner/automatic", Session{ID: "automatic", OwnerID: "owner", AssistantID: "assistant", WindowStart: from.Add(3 * time.Hour), Deadline: from.Add(3*time.Hour + 10*time.Minute), State: "completed"})
	put(t, f.db, sessions, "owner/manual", Session{ID: "manual", OwnerID: "owner", AssistantID: "dismissed", WindowStart: from.Add(5 * time.Hour), Deadline: from.Add(5*time.Hour + 10*time.Minute), State: "completed", Manual: true})
	all, err := f.s.Windows(t.Context(), f.p, "", from, to)
	if err != nil || len(all) != 2 || all[0].SessionID != "automatic" || all[0].State != "completed" || all[1].SessionID != "manual" {
		t.Fatal("actual sessions must replace or supplement reserved windows", all, err)
	}
	dismissed, err := f.s.Windows(t.Context(), f.p, "dismissed", from, to)
	if err != nil || len(dismissed) != 1 || dismissed[0].SessionID != "manual" {
		t.Fatal("dismissed assistant's actual history", dismissed, err)
	}
	for _, id := range []core.ID{"foreign", "missing"} {
		if _, err := f.s.Windows(t.Context(), f.p, id, from, to); core.CodeOf(err) != core.NotFound {
			t.Fatalf("%s: want not found, got %v", id, err)
		}
	}
}
