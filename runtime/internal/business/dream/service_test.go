package dream

import (
	"context"
	"encoding/json"
	"errors"
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
	"strings"
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
	}, JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID) ([]memory.Root, error) {
		return []memory.Root{{Scope: memory.Global, Directory: filepath.Join(root, string(owner), "global")}, {Scope: memory.Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "work")}}, nil
	}})
	m := memory.NewService(db, memory.Options{Files: files, Authorizer: allow{}, Now: clock, CheckAssistant: func(context.Context, core.Principal, core.ID) error { return nil }})
	s := New(db, Options{Files: files, Authorizer: allow{}, Memories: m, Now: clock})
	f = &fixture{s: s, db: db, memories: m, p: p, ap: ap, now: now}
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
func output(r Run, evidence string) string {
	b, _ := json.Marshal(Result{Summary: "用户要求项目汇报先写结论。", Memories: []Candidate{{Text: "项目汇报先写结论", Action: "create", Path: ".agents/memory/用户偏好/项目汇报.md", Context: "项目汇报", Evidence: evidence, Sources: []core.Object{r.Sources[0].Source}}}})
	return string(b)
}
func TestFormationRecallForgetAndIdempotency(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	again := f.request(t, "duplicate-trigger")
	if r.ID != again.ID {
		t.Fatal("duplicate pending run")
	}
	prompt, err := f.s.Prepare(t.Context(), f.ap, ev(r))
	if err != nil || !strings.Contains(prompt, "以后项目汇报") {
		t.Fatal(err, prompt)
	}
	var evidence struct {
		Day      string
		Sources  []Evidence
		Existing []memory.Memory
	}
	if err := json.Unmarshal([]byte(prompt), &evidence); err != nil || evidence.Day != r.Day || len(evidence.Sources) != 1 || evidence.Sources[0].Source != r.Sources[0].Source {
		t.Fatal("Prepare must return the batch evidence as JSON, without system instructions", err, prompt)
	}
	if err = f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); err != nil {
		t.Fatal(err)
	}
	page, err := f.memories.List(t.Context(), f.ap, memory.Query{State: memory.Active})
	rows := page.Items
	if err != nil || len(rows) != 1 || rows[0].Path != ".agents/memory/用户偏好/项目汇报.md" {
		t.Fatal(err, rows)
	}
	m := rows[0]
	if err = f.memories.Delete(t.Context(), f.p, memory.DeleteRequest{Forget: true, Mutation: memory.Mutation{IdempotencyKey: "forget", ExpectedVersion: m.Version}, Ref: m.Ref}); err != nil {
		t.Fatal(err)
	}
	reviews, err := f.s.Runs(t.Context(), f.p, "assistant")
	if err != nil || reviews[0].Summary != "" {
		t.Fatal("forgotten fact retained in review", err, reviews)
	}
	next := f.request(t, "next")
	if next.State != "up_to_date" {
		t.Fatal("forgotten sources replayed", next)
	}
	suppressed, err := f.s.options.Files.Suppressed(t.Context(), memory.Memory{OwnerID: "owner", Text: "不同措辞", Sources: m.Sources, Ref: memory.Ref{AssistantID: "assistant", Path: ".agents/memory/记忆.md"}})
	if err != nil || !suppressed {
		t.Fatal("paraphrase not suppressed", err)
	}
}
func TestUncertainAndForgedSources(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	bad := strings.Replace(output(r, "explicit"), "msg-001", "other-assistant-message", 1)
	if err := f.s.Commit(t.Context(), f.ap, ev(r), bad); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	page, err := f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("invalid result partially committed", err)
	}
	if err = f.s.Commit(t.Context(), f.ap, ev(r), output(r, "inferred")); err != nil {
		t.Fatal(err)
	}
	active, err := f.memories.List(t.Context(), f.ap, memory.Query{State: memory.Active})
	rows := active.Items
	if err != nil || len(rows) != 0 {
		t.Fatal("inference became fact", rows, err)
	}
	page, _ = f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 1 || page.Items[0].State != memory.Pending {
		t.Fatal(page)
	}
}
func TestBoundedReplayAndOfflineCatchup(t *testing.T) {
	f := setup(t)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	r := f.request(t, "first")
	if len(r.Sources) != 80 {
		t.Fatal(len(r.Sources))
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), `{"Summary":"first batch","Memories":[]}`); err != nil {
		t.Fatal(err)
	}
	second := f.request(t, "second")
	if len(second.Sources) != 3 || second.ID == r.ID {
		t.Fatal(second)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(second), `{"Summary":"second batch","Memories":[]}`); err != nil {
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
func TestConflictConfirmationPreservesOwnerEdit(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	old, err := f.memories.Create(t.Context(), f.p, memory.CreateRequest{Mutation: memory.Mutation{IdempotencyKey: "old"}, Text: "项目汇报先写风险", Ref: memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: ".agents/memory/old.md"}})
	if err != nil {
		t.Fatal(err)
	}
	r := f.request(t, "first")
	var result Result
	json.Unmarshal([]byte(output(r, "explicit")), &result)
	result.Memories[0].Replaces = &old.Ref
	result.Memories[0].ExpectedVersion = old.Version
	b, _ := json.Marshal(result)
	if err = f.s.Commit(t.Context(), f.ap, ev(r), string(b)); err != nil {
		t.Fatal(err)
	}
	page, _ := f.memories.List(t.Context(), f.p, memory.Query{State: memory.Pending})
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	candidate := page.Items[0]
	committed, err := f.s.GetRun(t.Context(), f.p, r.ID)
	if err != nil || len(committed.Decisions) != 1 {
		t.Fatal("missing committed decision", committed, err)
	}
	decision := committed.Decisions[0]
	if decision.Action != "pending" || (decision.Replaces == nil || *decision.Replaces != old.Ref) || decision.PreviousText != old.Text || decision.Text != candidate.Text {
		t.Fatal("replacement must retain its original text and pending status", decision)
	}
	old, err = f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Mutation: memory.Mutation{IdempotencyKey: "owner-edit", ExpectedVersion: old.Version}, Ref: old.Ref, Text: "项目汇报先写成本"})
	if err != nil {
		t.Fatal(err)
	}
	committed, err = f.s.GetRun(t.Context(), f.p, r.ID)
	if err != nil || committed.Decisions[0].PreviousText != "项目汇报先写风险" {
		t.Fatal("later owner edit changed dream history", committed, err)
	}
	active := memory.Active
	_, err = f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Mutation: memory.Mutation{IdempotencyKey: "confirm", ExpectedVersion: candidate.Version}, Ref: candidate.Ref, Text: candidate.Text, Sources: candidate.Sources, State: &active})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("overwrote owner revision", err)
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

func TestUndoPreservesLaterUserEdits(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	if err := f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); err != nil {
		t.Fatal(err)
	}
	page, _ := f.memories.List(t.Context(), f.p, memory.Query{})
	m := page.Items[0]
	_, err := f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Mutation: memory.Mutation{IdempotencyKey: "edit", ExpectedVersion: m.Version}, Ref: m.Ref, Text: "用户已修订的偏好", Sources: m.Sources})
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.s.Undo(t.Context(), f.p, UndoRequest{Mutation: core.Mutation{IdempotencyKey: "undo"}, ID: r.ID})
	if err != nil || out.State != "undone" || len(out.UndoRetained) != 1 {
		t.Fatal(out, err)
	}
	page, _ = f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 1 || page.Items[0].Text != "用户已修订的偏好" {
		t.Fatal(page)
	}
	_, err = f.s.Undo(t.Context(), f.p, UndoRequest{Mutation: core.Mutation{IdempotencyKey: "undo"}, ID: r.ID})
	if err != nil {
		t.Fatal(err)
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
	if err := f.s.Commit(t.Context(), f.ap, ev(r), `{"Summary":"password=do-not-retain","Memories":[]}`); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("credential result accepted", err)
	}
}

func TestDreamRecoversFilesAfterFinalDatabaseFailure(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "recover")
	text := output(r, "explicit")
	f.beforeMemoryCommit = func() error {
		f.beforeMemoryCommit = nil
		f.db.FailNextCommit(errors.New("final record failure"))
		return nil
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), text); err == nil {
		t.Fatal("expected final DB failure")
	}
	page, err := f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("committed file missing", page, err)
	}
	hash := page.Items[0].Version
	if err = f.s.Commit(t.Context(), f.ap, ev(r), text); err != nil {
		t.Fatal("recovery failed", err)
	}
	page, err = f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Version != hash {
		t.Fatal("recovery rewrote file", page, err)
	}
}
func TestDreamPartialRecoveryRetainsUserEditedFile(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "partial")
	var result Result
	if err := json.Unmarshal([]byte(output(r, "explicit")), &result); err != nil {
		t.Fatal(err)
	}
	second := result.Memories[0]
	second.Text = "另一个独立的长期偏好"
	second.Path = ".agents/memory/用户偏好/其他长期偏好.md"
	result.Memories = append(result.Memories, second)
	raw, _ := json.Marshal(result)
	count := 0
	f.beforeMemoryCommit = func() error {
		count++
		if count == 2 {
			return errors.New("second file unavailable")
		}
		return nil
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), string(raw)); err == nil {
		t.Fatal("expected partial commit")
	}
	page, err := f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	m := page.Items[0]
	changed, err := f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Ref: m.Ref, Text: "用户保留的编辑", Mutation: memory.Mutation{IdempotencyKey: "edit-partial", ExpectedVersion: m.Version}})
	if err != nil {
		t.Fatal(err)
	}
	f.beforeMemoryCommit = nil
	if err = f.s.Commit(t.Context(), f.ap, ev(r), string(raw)); core.CodeOf(err) != core.Conflict {
		t.Fatal("expected edit conflict", err)
	}
	got, err := f.memories.Get(t.Context(), f.p, m.Ref)
	if err != nil || got.Version != changed.Version {
		t.Fatal("recovery replaced user edit", got, err)
	}
}

func TestUndoRemovesUnchangedFileAndClearsOutput(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "undo-file")
	if err := f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "undone" || result.Summary != "" || result.Output != "" || len(result.Decisions) != 0 {
		t.Fatal("undo retained generated review", result)
	}
	page, err := f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("undo retained original file", page, err)
	}
	if _, err = f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo"}}); err != nil {
		t.Fatal("undo replay", err)
	}
}

func TestSemanticPathsAndExplicitUpdate(t *testing.T) {
	for _, tc := range []struct {
		name, action     string
		confirmed, stale bool
		want             core.Code
	}{
		{name: "same path create conflicts", action: "create", want: core.Conflict},
		{name: "explicit update", action: "update"},
		{name: "stale version conflicts", action: "update", stale: true, want: core.Conflict},
		{name: "confirmed requires proposal", action: "update", confirmed: true, want: core.Conflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.message(t, 1)
			r := f.request(t, "path-test")
			ref := memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: ".agents/memory/用户偏好/项目汇报.md"}
			old, err := f.s.options.Files.Put(t.Context(), memory.Memory{Ref: ref, OwnerID: "owner", Text: "旧的汇报方式", State: memory.Active, Confirmed: tc.confirmed}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			candidate := Candidate{Action: tc.action, Path: ref.Path, Text: "项目汇报先写结论", Evidence: "explicit", Sources: []core.Object{r.Sources[0].Source}}
			if tc.action == "update" {
				candidate.ExpectedVersion = old.Version
			}
			if tc.stale {
				candidate.ExpectedVersion = strings.Repeat("0", 64)
			}
			encoded, _ := json.Marshal(Result{Summary: "更新汇报约定", Memories: []Candidate{candidate}})
			err = f.s.Commit(t.Context(), f.ap, ev(r), string(encoded))
			if tc.want != "" {
				if core.CodeOf(err) != tc.want {
					t.Fatalf("got %v", err)
				}
				current, _ := f.memories.Get(t.Context(), f.p, ref)
				if current.Version != old.Version {
					t.Fatal("conflicting operation changed file")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			updated, err := f.memories.Get(t.Context(), f.p, ref)
			if err != nil || updated.Text != candidate.Text || updated.ReplacesVersion != "" {
				t.Fatal(updated, err)
			}
			if err = f.s.Commit(t.Context(), f.ap, ev(r), string(encoded)); err != nil {
				t.Fatal("retry", err)
			}
			if _, err = f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo-update"}}); err != nil {
				t.Fatal(err)
			}
			retained, err := f.memories.Get(t.Context(), f.p, ref)
			if err != nil || retained.Version != updated.Version {
				t.Fatal("undo must retain overwritten files", err)
			}
		})
	}
}
