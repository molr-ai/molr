package dream

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func commitResult(t *testing.T, f *fixture, r Run, result Result) {
	t.Helper()
	raw, _ := json.Marshal(result)
	if err := f.s.Commit(t.Context(), f.ap, ev(r), string(raw)); err != nil {
		t.Fatal(err)
	}
}
func formCore(t *testing.T, f *fixture) (Run, memory.Memory, memory.Memory) {
	t.Helper()
	f.message(t, 1)
	r := f.request(t, "form")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	var result Result
	json.Unmarshal([]byte(output(r, "explicit")), &result)
	result.Core = &CoreUpdate{Text: "项目汇报先写结论。", Topics: []string{result.Memories[0].Path}, Reason: "明确的长期工作约定"}
	commitResult(t, f, r, result)
	topic, err := f.memories.Get(t.Context(), f.p, memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: result.Memories[0].Path})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := f.memories.Get(t.Context(), f.p, memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: "MEMORY.md"})
	if err != nil {
		t.Fatal(err)
	}
	return r, topic, summary
}
func TestCorePromotionMaintenanceAndDemotion(t *testing.T) {
	f := setup(t)
	_, topic, summary := formCore(t, f)
	if len(summary.Dependencies) != 1 || summary.Dependencies[0].Version != topic.Version || len(summary.Sources) != 1 {
		t.Fatal(summary)
	}
	f.now = f.now.AddDate(0, 0, 1)
	r := f.request(t, "quiet-day")
	if !r.Maintenance || len(r.Sources) != 0 {
		t.Fatal("quiet day must maintain existing cognition", r)
	}
	input, err := f.s.Prepare(t.Context(), f.ap, ev(r))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, topic.Path) || !strings.Contains(input, summary.Text) {
		t.Fatal(input)
	}
	commitResult(t, f, r, Result{Summary: "缩小核心记忆范围，保留主题知识", Core: &CoreUpdate{ExpectedVersion: summary.Version, Text: "", Reason: "约定只需在项目主题中查阅"}})
	demoted, err := f.memories.Get(t.Context(), f.p, summary.Ref)
	if err != nil || demoted.State != memory.Superseded {
		t.Fatal(demoted, err)
	}
	if _, err = f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo-demotion"}}); err != nil {
		t.Fatal(err)
	}
	restored, _ := f.memories.Get(t.Context(), f.p, summary.Ref)
	if restored.Version != summary.Version {
		t.Fatal("core demotion cannot be restored", restored)
	}
}
func TestCorrectionUpdatesTopicAndCoreWithoutPending(t *testing.T) {
	f := setup(t)
	_, topic, summary := formCore(t, f)
	f.now = f.now.AddDate(0, 0, 1)
	f.message(t, 2)
	r := f.request(t, "correction")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	result := Result{Summary: "用户纠正汇报约定", Memories: []Candidate{{Action: "update", Path: topic.Path, ExpectedVersion: topic.Version, Text: "项目汇报先写风险", Evidence: "explicit", Sources: []core.Object{r.Sources[0].Source}, Reason: "新的明确纠正"}}, Core: &CoreUpdate{ExpectedVersion: summary.Version, Text: "项目汇报先写风险", Topics: []string{topic.Path}, Reason: "同步用户纠正"}}
	commitResult(t, f, r, result)
	changed, _ := f.memories.Get(t.Context(), f.p, summary.Ref)
	if changed.Text != result.Core.Text || changed.State != memory.Active {
		t.Fatal(changed)
	}
	pending, _ := f.memories.List(t.Context(), f.p, memory.Query{State: memory.Pending})
	if len(pending.Items) != 0 {
		t.Fatal(pending)
	}
	// A later owner edit must survive undo, while the unchanged core is restored.
	updated, _ := f.memories.Get(t.Context(), f.p, topic.Ref)
	owner, err := f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Ref: topic.Ref, Text: "用户手工保留", Mutation: memory.Mutation{IdempotencyKey: "edit", ExpectedVersion: updated.Version}})
	if err != nil {
		t.Fatal(err)
	}
	undo, err := f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo"}})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := f.memories.Get(t.Context(), f.p, topic.Ref)
	if retained.Version != owner.Version || len(undo.UndoRetained) != 1 {
		t.Fatal(retained, undo)
	}
}
func TestCoreCannotPromoteInferenceOrUnknownTopic(t *testing.T) {
	for _, inferred := range []bool{false, true} {
		t.Run(fmt.Sprint(inferred), func(t *testing.T) {
			f := setup(t)
			f.message(t, 1)
			r := f.request(t, "invalid-core")
			result := Result{Summary: "review", Core: &CoreUpdate{Text: "unproven", Topics: []string{".agents/memory/unknown.md"}}}
			if inferred {
				var base Result
				json.Unmarshal([]byte(output(r, "inferred")), &base)
				result.Memories = base.Memories
				result.Core.Topics = []string{base.Memories[0].Path}
			}
			raw, _ := json.Marshal(result)
			if err := f.s.Commit(t.Context(), f.ap, ev(r), string(raw)); core.CodeOf(err) != core.InvalidArgument {
				t.Fatal(err)
			}
			page, _ := f.memories.List(t.Context(), f.p, memory.Query{})
			if len(page.Items) != 0 {
				t.Fatal("invalid core partially wrote topics")
			}
		})
	}
}
func TestComparisonFindsTopicBeyondFirstPage(t *testing.T) {
	f := setup(t)
	for i := 0; i < 205; i++ {
		m := memory.Memory{Ref: memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: fmt.Sprintf(".agents/memory/topic-%03d.md", i)}, OwnerID: "owner", State: memory.Active, Text: "unrelated topic", Evidence: "explicit"}
		if i == 204 {
			m.Text = "项目汇报先写结论"
		}
		if _, err := f.s.options.Files.Put(t.Context(), m, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	f.message(t, 1)
	r := f.request(t, "compare")
	input, err := f.s.Prepare(t.Context(), f.ap, ev(r))
	if err != nil || !strings.Contains(input, "topic-204.md") {
		t.Fatal("comparison ignored later pages", err)
	}
}
func TestSourceMemoryChangedAfterPrepareRejectsCore(t *testing.T) {
	f := setup(t)
	_, topic, summary := formCore(t, f)
	f.now = f.now.AddDate(0, 0, 1)
	r := f.request(t, "maintenance")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Ref: topic.Ref, Text: "new user decision", Mutation: memory.Mutation{IdempotencyKey: "edit", ExpectedVersion: topic.Version}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Result{Summary: "review", Core: &CoreUpdate{ExpectedVersion: summary.Version, Text: summary.Text, Topics: []string{topic.Path}}})
	if err := f.s.Commit(t.Context(), f.ap, ev(r), string(raw)); err == nil {
		t.Fatal("stale topic accepted")
	}
}
func TestForgetRemovesUndoImages(t *testing.T) {
	f := setup(t)
	_, topic, _ := formCore(t, f)
	f.now = f.now.AddDate(0, 0, 1)
	f.message(t, 2)
	r := f.request(t, "update")
	commitResult(t, f, r, Result{Summary: "new scope", Memories: []Candidate{{Action: "update", Path: topic.Path, ExpectedVersion: topic.Version, Text: "项目汇报先写结论和风险", Evidence: "explicit", Sources: topic.Sources}}})
	current, _ := f.memories.Get(t.Context(), f.p, topic.Ref)
	if err := f.memories.Delete(t.Context(), f.p, memory.DeleteRequest{Ref: topic.Ref, Forget: true, Mutation: memory.Mutation{IdempotencyKey: "forget", ExpectedVersion: current.Version}}); err != nil {
		t.Fatal(err)
	}
	restored, err := f.s.options.Files.RestoreDream(t.Context(), f.p.OwnerID, r.ID, topic.Ref, current.Version, nil)
	if err != nil || restored {
		t.Fatal("forgotten memory can be restored", err)
	}
}
func TestDurablePlanSurvivesFinalTransactionFailure(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "failure")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	f.beforeMemoryCommit = func() error {
		f.beforeMemoryCommit = nil
		f.db.FailNextCommit(fmt.Errorf("database commit failure"))
		return nil
	}
	raw := output(r, "explicit")
	if err := f.s.Commit(t.Context(), f.ap, ev(r), raw); err == nil {
		t.Fatal("expected failure")
	}
	saved, err := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key(r.OwnerID, r.ID))
	if err != nil || saved.Output == "" || len(saved.MemoryVersions) != 1 {
		t.Fatal("commit plan lost", saved, err)
	}
	recovered, err := f.s.SavedOutput(t.Context(), f.ap, ev(r))
	if err != nil || recovered != raw {
		t.Fatal("recovery lost exact provider output", err)
	}
	if err = f.s.Commit(t.Context(), f.ap, ev(r), raw); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedDreamCanFinishAgentBookkeepingAfterDeadline(t *testing.T) {
	f := setup(t)
	r, _, _ := formCore(t, f)
	f.now = r.Deadline.Add(time.Minute)
	deadline, err := f.s.Deadline(t.Context(), f.ap, ev(r))
	if err != nil || !deadline.IsZero() {
		t.Fatal("completed files require bookkeeping only", deadline, err)
	}
	if _, err = f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	saved, err := f.s.SavedOutput(t.Context(), f.ap, ev(r))
	if err != nil || saved == "" {
		t.Fatal(err)
	}
	if err = f.s.Commit(t.Context(), f.ap, ev(r), saved); err != nil {
		t.Fatal(err)
	}
}

func TestForgetClearsMaintenanceReviewAndGeneratedCore(t *testing.T) {
	f := setup(t)
	_, topic, summary := formCore(t, f)
	f.now = f.now.AddDate(0, 0, 1)
	r := f.request(t, "quiet-day")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	commitResult(t, f, r, Result{Summary: "继续采用项目汇报先写结论", Core: &CoreUpdate{ExpectedVersion: summary.Version, Text: "项目汇报先写结论", Topics: []string{topic.Path}, Reason: "维护长期约定"}})
	if err := f.memories.Delete(t.Context(), f.p, memory.DeleteRequest{Ref: topic.Ref, Forget: true, Mutation: memory.Mutation{IdempotencyKey: "forget", ExpectedVersion: topic.Version}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.memories.Get(t.Context(), f.p, summary.Ref); core.CodeOf(err) != core.NotFound {
		t.Fatal("forgotten fact retained in core", err)
	}
	run, err := f.s.GetRun(t.Context(), f.p, r.ID)
	if err != nil || run.Summary != "" || len(run.Decisions) != 0 {
		t.Fatal("maintenance review retained forgotten fact", run, err)
	}
}

func TestPrepareRetryKeepsOriginalComparisonVersions(t *testing.T) {
	f := setup(t)
	_, topic, _ := formCore(t, f)
	f.now = f.now.AddDate(0, 0, 1)
	r := f.request(t, "maintenance")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(r)); err != nil {
		t.Fatal(err)
	}
	_, err := f.memories.Update(t.Context(), f.p, memory.UpdateRequest{Ref: topic.Ref, Text: "后来修订的约定", Mutation: memory.Mutation{IdempotencyKey: "external", ExpectedVersion: topic.Version}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Prepare(t.Context(), f.ap, ev(r)); core.CodeOf(err) != core.Conflict {
		t.Fatal("retry silently changed the evidence behind a cached model request", err)
	}
}

func TestDreamProtectsExternalEditsWithoutConfirmationFlag(t *testing.T) {
	for _, target := range []string{"topic", "core"} {
		t.Run(target, func(t *testing.T) {
			f := setup(t)
			_, topic, summary := formCore(t, f)
			original := topic
			if target == "core" {
				original = summary
			}
			edited := original
			edited.Text = "用户通过外部编辑器保留的内容"
			edited, err := f.s.options.Files.Put(t.Context(), edited, original.Version, nil)
			if err != nil {
				t.Fatal(err)
			}
			if edited.Confirmed {
				t.Fatal("test must exercise external edits without confirmation")
			}
			f.now = f.now.AddDate(0, 0, 1)
			f.message(t, 2)
			r := f.request(t, "protect")
			input, err := f.s.Prepare(t.Context(), f.ap, ev(r))
			if err != nil {
				t.Fatal(err)
			}
			var payload struct{ Protected []memory.Ref }
			if err = json.Unmarshal([]byte(input), &payload); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, ref := range payload.Protected {
				if ref == edited.Ref {
					found = true
				}
			}
			if !found {
				t.Fatal("model not told about protected edit")
			}
			result := Result{Summary: "保留用户手动修订"}
			if target == "topic" {
				result.Memories = []Candidate{{Action: "update", Path: edited.Path, ExpectedVersion: edited.Version, Text: "模型尝试覆盖", Evidence: "explicit", Sources: []core.Object{r.Sources[0].Source}}}
			} else {
				result.Core = &CoreUpdate{ExpectedVersion: edited.Version, Text: "模型尝试覆盖", Topics: []string{topic.Path}}
			}
			commitResult(t, f, r, result)
			current, _ := f.memories.Get(t.Context(), f.p, edited.Ref)
			if current.Version != edited.Version {
				t.Fatal("external edit overwritten")
			}
		})
	}
}
