package dream

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"testing"
	"time"
)

type allow struct{}

func (allow) Require(_ context.Context, p core.Principal, q policy.CheckRequest) error {
	if p.OwnerID != "owner" || (p.Kind == core.ActorAssistant && q.Target.Kind == "assistant" && q.Target.ID != p.SubjectID) {
		return core.NewError(core.Forbidden, "denied")
	}
	return nil
}

type fixture struct {
	s                  *Coordinator
	db                 *sqlitestore.Store
	memories           memory.Service
	p, ap              core.Principal
	now                time.Time
	root               string
	beforeMemoryCommit func() error
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	ap := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	var f *fixture
	clock := func() time.Time {
		if f != nil {
			return f.now
		}
		return now
	}
	root := t.TempDir()
	files := memory.NewFiles(memory.FileOptions{BeforeCommit: func() error {
		if f != nil && f.beforeMemoryCommit != nil {
			return f.beforeMemoryCommit()
		}
		return nil
	}, JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return []memory.Root{{Scope: memory.Global, Directory: filepath.Join(root, string(owner), "global")}, {Scope: memory.Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "work")}}, nil
	}})
	m := memory.NewService(db, memory.Options{Files: files, Authorizer: allow{}, Now: clock, CheckAssistant: func(context.Context, core.Principal, core.ID) error { return nil }})
	s := New(db, Options{Files: files, Authorizer: allow{}, Memories: m, Now: clock})
	f = &fixture{s: s, db: db, memories: m, p: p, ap: ap, now: now, root: root}
	put(t, db, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active, ConversationID: "chat", CreatedAt: now.AddDate(0, 0, -2)})
	return f
}
func put(t *testing.T, s persistence.Store, b, k string, v any) {
	t.Helper()
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, b, k, v) }); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) message(t *testing.T, n int) {
	ref := core.ID(fmt.Sprintf("msg-%03d", n))
	put(t, f.db, "im.messages", persistence.Key("owner", ref), conversation.Message{ID: ref, ConversationID: "chat", Sender: conversation.User, CreatedAt: f.now.Add(-12 * time.Hour), Content: conversation.Content{Kind: conversation.Text, Text: fmt.Sprintf("以后项目汇报先写结论 %d", n)}})
}
func (f *fixture) request(t *testing.T, key string) Run {
	t.Helper()
	r, err := f.s.Request(t.Context(), f.p, Request{Mutation: core.Mutation{IdempotencyKey: key}, AssistantID: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func ev(r Run) event.AssistantEvent {
	return event.AssistantEvent{ID: r.EventID, OwnerID: r.OwnerID, AssistantID: r.AssistantID, Kind: event.DreamDue, OperationID: r.ID, OccurrenceAt: r.End, CreatedAt: r.CreatedAt}
}
func output(_ Run, _ string) string { return "已整理本批经历。" }
func TestBoundedReplayAndOfflineCatchup(t *testing.T) {
	f := setup(t)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	r := f.request(t, "first")
	if len(r.Sources) != 80 {
		t.Fatal(len(r.Sources))
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), "first batch"); err != nil {
		t.Fatal(err)
	}
	second := f.request(t, "second")
	if len(second.Sources) != 3 || second.ID == r.ID {
		t.Fatal(second)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(second), "second batch"); err != nil {
		t.Fatal(err)
	}
	third := f.request(t, "third")
	if third.State != "up_to_date" {
		t.Fatal(third)
	}
	last := f.request(t, "last")
	if last.State != "up_to_date" {
		t.Fatal(last)
	}
}

func TestSourceChangedAndAssistantIsolation(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	other := f.ap
	other.SubjectID = "other"
	if _, err := f.s.Prepare(t.Context(), other, ev(r)); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if err := f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "im.messages", "owner/msg-001")
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); core.CodeOf(err) != core.Conflict {
		t.Fatal("deleted source accepted", err)
	}
}

func TestNightlyScheduleUsesConfiguredTimezone(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	c, err := f.s.Get(t.Context(), f.p, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "configure", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 13, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	r, _ := f.s.Runs(t.Context(), f.p, "assistant")
	if len(r) != 0 {
		t.Fatal("triggered before local hour")
	}
	if err = f.s.Tick(t.Context(), f.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r, _ = f.s.Runs(t.Context(), f.p, "assistant")
	if len(r) != 1 || r[0].Timezone != "Asia/Shanghai" {
		t.Fatal(r)
	}
}

func TestFailedDreamManualRetry(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	if err := f.s.Failed(t.Context(), ev(r), core.NewError(core.Unavailable, "provider unavailable")); err != nil {
		t.Fatal(err)
	}
	next := f.request(t, "retry")
	if next.ID != r.ID || next.EventID == r.EventID || next.State != "queued" {
		t.Fatal(next)
	}
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(next)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(next), output(next, "explicit")); err != nil {
		t.Fatal(err)
	}
}

func TestSensitiveRecordsExcluded(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	put(t, f.db, "im.messages", "owner/secret", conversation.Message{ID: "secret", ConversationID: "chat", Sender: conversation.User, CreatedAt: f.now.Add(-12 * time.Hour), Content: conversation.Content{Kind: conversation.Text, Text: "API_KEY=do-not-retain-this-secret"}})
	r := f.request(t, "first")
	if len(r.Sources) != 1 {
		t.Fatal("credential record included", r.Sources)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), "password=do-not-retain"); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("credential result accepted", err)
	}
}

func TestSavedSummaryRecoveryAndIsolation(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "saved-summary")
	if saved, err := f.s.SavedOutput(t.Context(), f.ap, ev(r)); err != nil || saved != "" {
		t.Fatal(saved, err)
	}
	const summary = "Retain the committed summary."
	if err := f.s.Commit(t.Context(), f.ap, ev(r), summary); err != nil {
		t.Fatal(err)
	}
	// Recreate the coordinator to prove recovery reads durable state.
	recovered := New(f.db, f.s.options)
	if saved, err := recovered.SavedOutput(t.Context(), f.ap, ev(r)); err != nil || saved != summary {
		t.Fatal(saved, err)
	}
	other := f.ap
	other.SubjectID = "other"
	if _, err := recovered.SavedOutput(t.Context(), other, ev(r)); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
}
