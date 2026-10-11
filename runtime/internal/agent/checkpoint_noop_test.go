package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestCheckpointSaveIfChanged(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	repo := &checkpoints{store: db, trace: trace}
	ctx := trace.Begin(t.Context(), f.Owner.OwnerID, "assistant", f.Turn.Event.ID, f.Turn.Principal.ExecutionID, "user_message", 1, nil)
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, LastEventID: f.Turn.Event.ID, FormatVersion: 1, Sequence: 1, SavedAt: time.Now().UTC(), Context: json.RawMessage(`{"Inputs":[{"Role":"user","Text":"same"}],"Current":{"Step":0}}`)}
	saved, err := repo.SaveIfChanged(ctx, cp, 0)
	testkit.RequireNoError(t, err)
	if !saved {
		t.Fatal("initial state not saved")
	}
	revision, err := trace.Revision(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	originalTime := cp.SavedAt
	cp.Sequence++
	cp.SavedAt = cp.SavedAt.Add(time.Second)
	// Different JSON ordering/whitespace and repository recreation are still a no-op.
	cp.Context = json.RawMessage(`{ "Current": {"Step":0}, "Inputs": [{"Text":"same", "Role":"user"}] }`)
	repo = &checkpoints{store: db, trace: trace}
	saved, err = repo.SaveIfChanged(ctx, cp, 1)
	testkit.RequireNoError(t, err)
	if saved {
		t.Fatal("unchanged state saved")
	}
	// An unchanged save outside an active trace must not create a workbench run.
	saved, err = repo.SaveIfChanged(t.Context(), cp, 1)
	testkit.RequireNoError(t, err)
	if saved {
		t.Fatal("standalone unchanged state saved")
	}
	unchangedRuns, err := trace.Runs(ctx, f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	if len(unchangedRuns.Items) != 1 {
		t.Fatal("no-op created a standalone workbench run")
	}
	stored, err := repo.Load(ctx, cp.OwnerID, cp.AssistantID)
	testkit.RequireNoError(t, err)
	gotRevision, err := trace.Revision(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	if stored.Sequence != 1 || !stored.SavedAt.Equal(originalTime) || gotRevision != revision {
		t.Fatal("no-op advanced checkpoint or workbench")
	}
	_, err = repo.SaveIfChanged(ctx, cp, 0)
	testkit.RequireCode(t, err, core.Conflict)
	// Force-saving the same recovery state still emits exactly one checkpoint.
	testkit.RequireNoError(t, repo.Save(ctx, cp, 1))
	for _, change := range []struct {
		name  string
		apply func(*Checkpoint)
	}{
		{"cursor", func(c *Checkpoint) {
			c.Context = json.RawMessage(`{"Inputs":[{"Role":"user","Text":"same"}],"Current":{"Step":1}}`)
		}},
		{"execution", func(c *Checkpoint) { c.ExecutionID = "another-execution" }},
		{"event", func(c *Checkpoint) { c.LastEventID = "another-event" }},
		{"context", func(c *Checkpoint) {
			c.Context = json.RawMessage(`{"Inputs":[{"Role":"user","Text":"changed"}],"Current":{"Step":1}}`)
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			expected := cp.Sequence
			cp.Sequence++
			change.apply(&cp)
			saved, err := repo.SaveIfChanged(ctx, cp, expected)
			testkit.RequireNoError(t, err)
			if !saved {
				t.Fatal("changed recovery state not saved")
			}
		})
	}
	runs, err := trace.Runs(ctx, f.Owner, debugtrace.Query{})
	testkit.RequireNoError(t, err)
	run, err := trace.Get(ctx, f.Owner, runs.Items[0].ID)
	testkit.RequireNoError(t, err)
	var versions []uint64
	for _, step := range run.Steps {
		if step.Kind == "checkpoint.saved" {
			payload, err := trace.Detail(ctx, f.Owner, run.ID, step.Sequence)
			testkit.RequireNoError(t, err)
			var data struct{ Sequence uint64 }
			testkit.RequireNoError(t, json.Unmarshal(payload.Data, &data))
			versions = append(versions, data.Sequence)
		}
	}
	if len(versions) != 6 {
		t.Fatalf("save events: %v", versions)
	}
	for i, v := range versions {
		if v != uint64(i+1) {
			t.Fatalf("noncontiguous versions: %v", versions)
		}
	}
	// Both failed commits and enclosing rollbacks preserve checkpoint/trace identity.
	for _, commitFailure := range []bool{false, true} {
		revision, err = trace.Revision(ctx, f.Owner)
		testkit.RequireNoError(t, err)
		next := cp
		next.Sequence++
		next.Context = json.RawMessage(`{"Inputs":[]}`)
		if commitFailure {
			db.FailNextCommit(errors.New("commit rejected"))
		}
		err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			_, err := repo.SaveIfChanged(ctx, next, cp.Sequence)
			if err != nil {
				return err
			}
			if !commitFailure {
				return errors.New("rollback")
			}
			return nil
		})
		if err == nil {
			t.Fatal("transaction unexpectedly succeeded")
		}
		stored, err = repo.Load(ctx, cp.OwnerID, cp.AssistantID)
		testkit.RequireNoError(t, err)
		gotRevision, err = trace.Revision(ctx, f.Owner)
		testkit.RequireNoError(t, err)
		if stored.Sequence != cp.Sequence || gotRevision != revision {
			t.Fatal("failed save escaped transaction")
		}
	}
}

func TestProgressNoopPreservesRecoveryReferenceAndForcedObservation(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	e.checkpoints = &checkpoints{store: db, trace: trace}
	ctx := trace.Begin(t.Context(), f.Owner.OwnerID, "assistant", f.Turn.Event.ID, f.Turn.Principal.ExecutionID, "user_message", 1, nil)
	p := turnProgress{Inputs: []model.Input{{Role: model.User, Text: "same context"}}}
	testkit.RequireNoError(t, e.saveProgress(ctx, f.Turn, &p, progressIfChanged))
	first := p.Revision
	revision, err := trace.Revision(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	for range 3 {
		testkit.RequireNoError(t, e.saveProgress(ctx, f.Turn, &p, progressIfChanged))
	}
	gotRevision, err := trace.Revision(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	if p.Revision != first || gotRevision != revision {
		t.Fatal("no-op produced progress or trace")
	}
	ref, err := persistence.Load[progressReference](ctx, db, progressBucket, persistence.Key(f.Owner.OwnerID, f.Turn.Event.ID))
	testkit.RequireNoError(t, err)
	if ref.Revision != first {
		t.Fatal("no-op changed progress reference")
	}
	testkit.RequireNoError(t, e.saveProgress(ctx, f.Turn, &p, progressAlways))
	if p.Revision != first+1 {
		t.Fatal("forced observation was skipped")
	}
	// Execution cursor changes must save even if the visible messages are identical.
	p.Step++
	testkit.RequireNoError(t, e.saveProgress(ctx, f.Turn, &p, progressIfChanged))
	if p.Revision != first+2 {
		t.Fatal("cursor change was skipped")
	}
	cp, err := e.checkpoints.Load(ctx, f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	state, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	restored, err := stateProgress(cp, state)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, e.saveProgress(ctx, f.Turn, &restored, progressIfChanged))
	if restored.Revision != p.Revision {
		t.Fatal("restored unchanged state saved again")
	}
	stale := p
	stale.Revision--
	testkit.RequireCode(t, e.saveProgress(ctx, f.Turn, &stale, progressIfChanged), core.Conflict)
	testkit.RequireNoError(t, e.Stop(ctx, f.Owner, "assistant"))
	testkit.RequireCode(t, e.saveProgress(ctx, f.Turn, &p, progressIfChanged), core.Canceled)
}

func BenchmarkUnchangedCheckpoint(b *testing.B) {
	for _, size := range []int{4096, 256 << 10, 1 << 20} {
		for _, skip := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/skip=%t", size, skip), func(b *testing.B) {
				db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "checkpoint.db"))
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				trace := debugtrace.New(db, debugtrace.Options{Enabled: true})
				repo := &checkpoints{store: db, trace: trace}
				ctx := trace.Begin(b.Context(), "owner", "assistant", "event", "execution", "user_message", 1, nil)
				raw, _ := json.Marshal(map[string]any{"Inputs": []model.Input{{Role: model.User, Text: strings.Repeat("x", size)}}})
				cp := Checkpoint{OwnerID: "owner", AssistantID: "assistant", ExecutionID: "execution", LastEventID: "event", FormatVersion: 1, Sequence: 1, SavedAt: time.Now().UTC(), Context: raw}
				if err := repo.Save(ctx, cp, 0); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				b.ResetTimer()
				for b.Loop() {
					next := cp
					next.Sequence++
					next.SavedAt = time.Now().UTC()
					if skip {
						saved, err := repo.SaveIfChanged(ctx, next, cp.Sequence)
						if err != nil || saved {
							b.Fatal(saved, err)
						}
					} else {
						if err := repo.Save(ctx, next, cp.Sequence); err != nil {
							b.Fatal(err)
						}
						cp = next
					}
				}
			})
		}
	}
}
