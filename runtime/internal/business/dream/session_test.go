package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionDeadlinePreservesCommittedMemoryAndBoundsBackfill(t *testing.T) {
	f := setup(t)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	first := f.request(t, "first")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Deadline(t.Context(), f.ap, ev(first)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, first, "first-topic", ".agents/memory/约定.md", "项目汇报先写结论", "")
	if err := f.s.Commit(t.Context(), f.ap, ev(first), output(first, "explicit")); err != nil {
		t.Fatal(err)
	}
	second := f.request(t, "second")
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), f.db, "agent.events")
	if err != nil || len(events) != 2 {
		t.Fatal("next batch event was deduplicated", events, err)
	}
	if !second.Deadline.Equal(first.Deadline) || second.SessionID != first.SessionID || second.Batch != 2 {
		t.Fatal("batch reset deadline", second)
	}
	f.now = first.Deadline
	if err := f.s.expire(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(second), output(second, "explicit")); core.CodeOf(err) != core.DeadlineExceeded {
		t.Fatal("late memory committed", err)
	}
	page, _ := f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 1 {
		t.Fatal("partial committed batch lost", page)
	}
	retry := f.request(t, "retry-expired")
	if retry.State != "timed_out" || retry.EventID != second.EventID {
		t.Fatal("timeout reset by retry", retry)
	}
	old, _ := persistence.Load[backlog](t.Context(), f.db, backlogs, "owner/assistant")
	if len(old.Sources) != 3 {
		t.Fatal(old)
	}
	if public, err := f.s.GetRun(t.Context(), f.p, first.ID); err != nil || public.Output != "" || public.Sources[0].Text != "" {
		t.Fatal("public run exposed raw snapshots", public, err)
	}
	raw, _ := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key("owner", first.ID))
	if raw.Output == "" || raw.Sources[0].Text == "" {
		t.Fatal("debug evidence discarded")
	}
	f.now = f.now.AddDate(0, 0, 1)
	f.message(t, 100)
	carry := f.request(t, "next-day")
	if _, err := f.s.Prepare(t.Context(), f.ap, ev(carry)); err != nil {
		t.Fatal(err)
	}
	if !carry.Backfill || len(carry.Sources) != 3 || carry.Day != first.Day {
		t.Fatal("older date must run first", carry)
	}
	// Expiring while backfilling must preserve the untouched fresh suffix only.
	f.now = carry.Deadline
	if err := f.s.expire(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	old, _ = persistence.Load[backlog](t.Context(), f.db, backlogs, "owner/assistant")
	if len(old.Sources) != 1 || old.Sources[0].Source.ID != "msg-100" {
		t.Fatal("fresh work lost or old backlog carried a second time", old)
	}

}
func TestSessionFrozenDeadlineAndCancel(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "first")
	c, _ := f.s.Get(t.Context(), f.p, "assistant")
	_, err := f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "duration", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := f.s.Deadline(t.Context(), f.ap, ev(r))
	if err != nil || !deadline.Equal(r.Deadline) {
		t.Fatal("configuration extended active dream", deadline, err)
	}
	f.s = New(f.db, f.s.options)
	deadline, err = f.s.Deadline(t.Context(), f.ap, ev(r))
	if err != nil || !deadline.Equal(r.Deadline) {
		t.Fatal("restart reset deadline")
	}
	if err := f.s.Cancel(t.Context(), f.p, "assistant"); err != nil {
		t.Fatal(err)
	}
	if active, err := f.s.Active(t.Context(), f.ap, ev(r)); err != nil || active {
		t.Fatal(active, err)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(r), output(r, "explicit")); core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}
func TestWindowsAndRecurringConflictsUseSharedRules(t *testing.T) {
	f := setup(t)
	c, _ := f.s.Get(t.Context(), f.p, "assistant")
	_, err := f.s.Configure(t.Context(), f.p, ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "window", ExpectedVersion: c.Version}, AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: "UTC", MaxDurationSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	ws, err := f.s.Windows(t.Context(), f.p, "assistant", from, to)
	if err != nil || len(ws) != 3 || ws[0].End.Sub(ws[0].Start) != 10*time.Minute {
		t.Fatal(ws, err)
	}
	rule := trigger.Rule{StartAt: from.Add(3*time.Hour + 5*time.Minute), Timezone: "UTC", Recurrence: "FREQ=DAILY;COUNT=3", MissedPolicy: trigger.Coalesce}
	conflicts, err := f.s.Conflicts(t.Context(), f.p, "assistant", rule, from, to)
	if err != nil || len(conflicts) != 3 {
		t.Fatal(conflicts, err)
	}
	rule.StartAt = from.Add(3*time.Hour + 10*time.Minute)
	conflicts, err = f.s.Conflicts(t.Context(), f.p, "assistant", rule, from, to)
	if err != nil || len(conflicts) != 0 {
		t.Fatal("end boundary not exclusive", conflicts, err)
	}
	foreign := f.p
	foreign.OwnerID = "foreign"
	if _, err := f.s.Windows(context.Background(), foreign, "assistant", from, to); err == nil {
		t.Fatal("unauthorized window read")
	}
}

func TestMissedAutomaticWindowDoesNotPreventManualDream(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	c, err := f.s.Get(t.Context(), f.p, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	missed, err := f.s.startSession(t.Context(), c, false, f.now, f.now.Add(-time.Hour))
	if err != nil || missed.State != "missed_window" {
		t.Fatal(missed, err)
	}
	manual := f.request(t, "manual-after-missed")
	if manual.State != "queued" || manual.SessionID == missed.SessionID || !manual.Deadline.Equal(f.now.Add(10*time.Minute)) {
		t.Fatal(manual)
	}
	again := f.request(t, "repeat-manual")
	if again.ID != manual.ID {
		t.Fatal("duplicate manual", again)
	}
}

func TestSessionProcessesDatesInOrderAcrossBatchesAndRestart(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			f := setup(t)
			loc, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 9, 30, 3, 0, 0, 0, loc)
			c, err := f.s.Get(t.Context(), f.p, "assistant")
			if err != nil {
				t.Fatal(err)
			}
			c, err = f.s.Configure(t.Context(), f.p, ConfigureRequest{AssistantID: "assistant", Enabled: true, Hour: 3, Timezone: loc.String(), Mutation: core.Mutation{IdempotencyKey: "configure", ExpectedVersion: c.Version}})
			if err != nil {
				t.Fatal(err)
			}
			// The newer date needs two batches and crosses UTC midnight while
			// remaining one date in the configured timezone.
			oldDay := dayStart(f.now, loc).AddDate(0, 0, -2)
			freshDay := oldDay.AddDate(0, 0, 1)
			old := []Evidence{}
			for i := 0; i < 83; i++ {
				at := freshDay.Add(time.Duration(i) * 10 * time.Minute)
				if i < 2 {
					at = oldDay.Add(time.Duration(i+1) * time.Hour)
				}
				ref := core.ID(fmt.Sprintf("ordered-%d", i))
				text := fmt.Sprintf("report preference %d", i)
				put(t, f.db, "im.messages", persistence.Key("owner", ref), conversation.Message{ID: ref, ConversationID: "chat", Sender: conversation.User, CreatedAt: at, Content: conversation.Content{Kind: conversation.Text, Text: text}})
				if i < 2 {
					old = append(old, Evidence{Source: core.Object{Kind: "message", ID: ref}, Version: string(id(text)), At: at, Kind: string(conversation.User), Text: text})
				}
			}
			old[0], old[1] = old[1], old[0] // Input order must not determine processing order.
			put(t, f.db, backlogs, "owner/assistant", backlog{OwnerID: "owner", AssistantID: "assistant", EligibleOn: dayStart(f.now, loc).UTC(), Sources: old})
			var first Run
			if automatic {
				if err := f.s.Tick(t.Context(), f.now); err != nil {
					t.Fatal(err)
				}
				all, err := persistence.Scan[Run](t.Context(), f.db, runs)
				if err != nil || len(all) != 1 {
					t.Fatal(all, err)
				}
				first = all[0]
			} else {
				first = f.request(t, "start")
			}
			if !first.Backfill || first.Day != "2026-09-28" || len(first.Sources) != 2 || first.Sources[0].Source.ID != "ordered-0" {
				t.Fatal(first)
			}
			payload, err := f.s.Prepare(t.Context(), f.ap, ev(first))
			if err != nil {
				t.Fatal(err)
			}
			var input struct {
				Day, Timezone string
				Backfill      bool
			}
			if err := json.Unmarshal([]byte(payload), &input); err != nil || input.Day != first.Day || input.Timezone != loc.String() || !input.Backfill {
				t.Fatal(input, err)
			}
			doc := writeFile(t, f, first, "old-preference", "MEMORY.md", "旧日期的偏好", "")
			if err := f.s.Commit(t.Context(), f.ap, ev(first), "older date complete"); err != nil {
				t.Fatal(err)
			}
			f.s = New(f.db, f.s.options)
			completed := []Run{first}
			for i, count := range []int{80, 1} {
				next := f.request(t, fmt.Sprintf("after-restart-%d", i))
				if next.Backfill || next.Day != "2026-09-29" || len(next.Sources) != count || next.SessionID != first.SessionID || next.Deadline != first.Deadline || next.Batch != i+2 {
					t.Fatal(next)
				}
				if _, err := f.s.Prepare(t.Context(), f.ap, ev(next)); err != nil {
					t.Fatal(err)
				}
				doc = writeFile(t, f, next, fmt.Sprintf("new-preference-%d", i), "MEMORY.md", fmt.Sprintf("新日期的偏好 %d", i), doc.FileVersion)
				if err := f.s.Commit(t.Context(), f.ap, ev(next), "newer date complete"); err != nil {
					t.Fatal(err)
				}
				completed = append(completed, next)
			}
			for _, run := range completed {
				saved, err := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key("owner", run.ID))
				if err != nil || !strings.Contains(saved.ReviewPath, "/"+run.Day+"/") {
					t.Fatal(saved, err)
				}
			}
			session, err := persistence.Load[Session](t.Context(), f.db, sessions, persistence.Key("owner", first.SessionID))
			if err != nil || session.State != "completed" || session.Unprocessed != 0 {
				t.Fatal(session, err)
			}
			latest, err := f.s.options.Files.ReadFile(t.Context(), "owner", memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: "MEMORY.md"})
			if err != nil || latest.Text != doc.Text {
				t.Fatal(latest, err)
			}
		})
	}
}

func TestBatchSeparatesLocalDatesAndBoundsBytes(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	sources := []Evidence{}
	for _, at := range []time.Time{start, start.Add(24 * time.Hour), start.AddDate(0, 0, 1)} {
		sources = append(sources, Evidence{At: at, Text: strings.Repeat("a", 50<<10)})
	}
	v := Session{ID: "session", Timezone: loc.String(), Sources: sources, FreshCount: 1, Start: start.AddDate(0, 0, 1), End: start.AddDate(0, 0, 2)}
	s := &Coordinator{}
	for i, day := range []string{"2026-11-01", "2026-11-01", "2026-11-02"} {
		v.Offset, v.Batch = i, i
		r := s.batch(v, start)
		if r.Day != day || len(r.Sources) != 1 || r.Backfill != (i < 2) || r.Batch != i+1 {
			t.Fatal(r)
		}
		if i < 2 && r.End.Sub(r.Start) != 25*time.Hour {
			t.Fatal("DST date truncated", r.Start, r.End)
		}
	}
}

func TestBackfillReviewPreservesEarlierReviewAndFreshBacklog(t *testing.T) {
	f := setup(t)
	for i := 0; i < 83; i++ {
		f.message(t, i)
	}
	first := f.request(t, "first")
	if err := f.s.Commit(t.Context(), f.ap, ev(first), "completed before interruption"); err != nil {
		t.Fatal(err)
	}
	remaining := f.request(t, "remaining")
	if err := f.s.Failed(t.Context(), ev(remaining), core.NewError(core.Unavailable, "interrupted")); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.AddDate(0, 0, 1)
	f.message(t, 100)
	carry := f.request(t, "next-day")
	if !carry.Backfill || carry.Day != first.Day || len(carry.Sources) != 3 {
		t.Fatal(carry)
	}
	if err := f.s.Commit(t.Context(), f.ap, ev(carry), "backfill complete"); err != nil {
		t.Fatal(err)
	}
	savedFirst, err := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key("owner", first.ID))
	if err != nil {
		t.Fatal(err)
	}
	savedCarry, err := persistence.Load[Run](t.Context(), f.db, runs, persistence.Key("owner", carry.ID))
	if err != nil || savedCarry.ReviewPath == savedFirst.ReviewPath {
		t.Fatal(savedCarry, err)
	}
	review, err := os.ReadFile(filepath.Join(f.root, "owner", "work", savedFirst.ReviewPath))
	if err != nil || !strings.Contains(string(review), "completed before interruption") {
		t.Fatal(review, err)
	}
	fresh := f.request(t, "fresh")
	if fresh.Backfill || len(fresh.Sources) != 1 {
		t.Fatal(fresh)
	}
	f.now = fresh.Deadline
	if err := f.s.expire(t.Context(), f.now); err != nil {
		t.Fatal(err)
	}
	old, err := persistence.Load[backlog](t.Context(), f.db, backlogs, "owner/assistant")
	if err != nil || len(old.Sources) != 1 || old.Sources[0].Source.ID != "msg-100" {
		t.Fatal(old, err)
	}
}

func TestBatchDoesNotMixDatesBelowSizeLimits(t *testing.T) {
	start := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	v := Session{ID: "session", Timezone: "UTC", FreshCount: 1}
	for i := 0; i < 3; i++ {
		v.Sources = append(v.Sources, Evidence{At: start.AddDate(0, 0, i), Text: "short evidence"})
	}
	s := &Coordinator{}
	for i := range v.Sources {
		v.Offset, v.Batch = i, i
		r := s.batch(v, start)
		if len(r.Sources) != 1 || r.Day != start.AddDate(0, 0, i).Format("2006-01-02") || r.Backfill != (i < 2) {
			t.Fatal("mixed evidence dates", r)
		}
	}
}
