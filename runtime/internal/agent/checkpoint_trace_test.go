package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestCheckpointTraceCommitsAtomicallyAndRejectsStaleSaves(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", string(f.Owner.OwnerID), map[string]bool{"WorkbenchVisible": true})
	}))
	repo := &checkpoints{store: db, trace: trace}
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: f.Turn.Event.AssistantID, ExecutionID: f.Turn.Principal.ExecutionID, LastEventID: f.Turn.Event.ID, FormatVersion: 1, Sequence: 1, SavedAt: time.Now().UTC(), Context: json.RawMessage(`{"Inputs":[]}`)}
	// A standalone control save must not leave a run or revision after rollback.
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := repo.Save(ctx, cp, 0); err != nil {
			return err
		}
		return errors.New("rollback parent")
	})
	if err == nil {
		t.Fatal("rollback did not fail")
	}
	runs, err := trace.Runs(t.Context(), f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	if len(runs.Items) != 0 {
		t.Fatal("rolled-back control save left a run")
	}
	// Test both nested rollback and an actual failed commit with an existing run.
	ctx := trace.Begin(t.Context(), cp.OwnerID, cp.AssistantID, cp.LastEventID, cp.ExecutionID, "user_message", 1, nil)
	revision, err := trace.Revision(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	for _, failCommit := range []bool{false, true} {
		if failCommit {
			db.FailNextCommit(errors.New("commit failure"))
		}
		err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := repo.Save(ctx, cp, 0); err != nil {
				return err
			}
			if !failCommit {
				return errors.New("rollback parent")
			}
			return nil
		})
		if err == nil {
			t.Fatal("failed save succeeded")
		}
		got, err := trace.Revision(ctx, f.Owner)
		testkit.RequireNoError(t, err)
		if got != revision {
			t.Fatal("rolled-back save advanced debug revision")
		}
		if _, err := repo.Load(ctx, cp.OwnerID, cp.AssistantID); !persistence.Missing(err) {
			t.Fatal("checkpoint escaped rollback")
		}
	}
	testkit.RequireNoError(t, repo.Save(ctx, cp, 0))
	testkit.RequireCode(t, repo.Save(ctx, cp, 0), core.Conflict)
	runs, err = trace.Runs(ctx, f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	run, err := trace.Get(ctx, f.Owner, runs.Items[0].ID)
	testkit.RequireNoError(t, err)
	count := 0
	for _, step := range run.Steps {
		if step.Kind == "checkpoint.saved" {
			count++
			payload, err := trace.Detail(ctx, f.Owner, run.ID, step.Sequence)
			testkit.RequireNoError(t, err)
			var summary struct {
				ContextOmitted bool
				ContextBytes   int
				Sequence       uint64
				Checkpoint     json.RawMessage
			}
			testkit.RequireNoError(t, json.Unmarshal(payload.Data, &summary))
			if !summary.ContextOmitted || summary.ContextBytes != len(cp.Context) || summary.Sequence != cp.Sequence || len(summary.Checkpoint) != 0 {
				t.Fatalf("duplicated checkpoint or missing metadata: %s", payload.Data)
			}
			restored, err := repo.Load(ctx, cp.OwnerID, cp.AssistantID)
			testkit.RequireNoError(t, err)
			if string(restored.Context) != string(cp.Context) {
				t.Fatal("trace optimization changed recovery checkpoint")
			}
		}
	}
	if count != 1 {
		t.Fatalf("successful version recorded %d times", count)
	}
	// Stop/reset/migration saves without an execution context still notify readers.
	cp.Sequence++
	testkit.RequireNoError(t, repo.Save(checkpointCause(t.Context(), "context_reset"), cp, 1))
	runs, err = trace.Runs(t.Context(), f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	if len(runs.Items) != 2 {
		t.Fatalf("control save missing: %+v", runs)
	}
}

func TestCheckpointLoadTracePrecedesNewEvent(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", string(f.Owner.OwnerID), map[string]bool{"WorkbenchVisible": true})
	}))
	e := f.Runtime.(*Engine)
	e.options.Trace = trace
	e.checkpoints = &checkpoints{store: db, trace: trace}
	first, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	next := nextStateTurn(f, "next-scheduled-event", event.ScheduleDue)
	_, err = e.Handle(t.Context(), next)
	testkit.RequireNoError(t, err)
	runs, err := trace.Runs(t.Context(), f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	found := false
	for _, summary := range runs.Items {
		if summary.EventID != next.Event.ID {
			continue
		}
		run, err := trace.Get(t.Context(), f.Owner, summary.ID)
		testkit.RequireNoError(t, err)
		for _, step := range run.Steps {
			if step.Kind != "context.build" {
				continue
			}
			found = true
			payload, err := trace.Detail(t.Context(), f.Owner, run.ID, step.Sequence)
			testkit.RequireNoError(t, err)
			var saved struct {
				Checkpoint Checkpoint
				Exists     bool
			}
			testkit.RequireNoError(t, json.Unmarshal(payload.Data, &saved))
			if !saved.Exists || saved.Checkpoint.Sequence != first.Checkpoint.Sequence || saved.Checkpoint.LastEventID != f.Turn.Event.ID || strings.Contains(string(saved.Checkpoint.Context), string(next.Event.ID)) {
				t.Fatalf("load trace is not the restored version: %s", payload.Data)
			}
		}
	}
	if !found {
		t.Fatal("missing load trace for scheduled event")
	}
}

func TestCheckpointTraceKeepsSmallSaveEventsAndFullRecoveryState(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", string(f.Owner.OwnerID), map[string]bool{"WorkbenchVisible": true})
	}))
	repo := &checkpoints{store: db, trace: trace}
	raw, _ := json.Marshal(map[string]any{"Inputs": []any{map[string]string{"Role": "user", "Text": strings.Repeat("context text ", 100000)}}})
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: f.Turn.Event.AssistantID, ExecutionID: f.Turn.Principal.ExecutionID, LastEventID: f.Turn.Event.ID, FormatVersion: 1, SavedAt: time.Now().UTC(), Context: raw}
	ctx := trace.Begin(t.Context(), cp.OwnerID, cp.AssistantID, cp.LastEventID, cp.ExecutionID, "user_message", 1, nil)
	for i := uint64(0); i < 21; i++ {
		cp.Sequence = i + 1
		testkit.RequireNoError(t, repo.Save(ctx, cp, i))
	}
	trace.Finish(ctx, "succeeded", "")
	rows, err := trace.Runs(ctx, f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	run, err := trace.Get(ctx, f.Owner, rows.Items[0].ID)
	testkit.RequireNoError(t, err)
	bytes := 0
	count := 0
	for _, step := range run.Steps {
		if step.Kind == "checkpoint.saved" {
			payload, err := trace.Detail(ctx, f.Owner, run.ID, step.Sequence)
			testkit.RequireNoError(t, err)
			bytes += len(payload.Data)
			count++
		}
	}
	if count != 21 || bytes > 8192 {
		t.Fatalf("checkpoint trace amplification: %d saves / %d bytes", count, bytes)
	}
	latest, err := repo.Load(ctx, cp.OwnerID, cp.AssistantID)
	testkit.RequireNoError(t, err)
	if latest.Sequence != 21 || string(latest.Context) != string(raw) {
		t.Fatal("latest recovery checkpoint changed")
	}
	t.Logf("21 save events: %d bytes; previous full contexts alone: %d bytes", bytes, 21*len(raw))
}
