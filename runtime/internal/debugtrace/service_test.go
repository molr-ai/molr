package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func fixture(t *testing.T) (*Service, core.Principal, *time.Time) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	s := New(db, Options{Enabled: true, MinRuns: 1, MaxRuns: 3, MaxPayloadBytes: 2048, Now: func() time.Time { return now }, Authorize: func(_ context.Context, p core.Principal) error { return p.Validate() }})
	return s, p, &now
}
func setVisible(t *testing.T, s *Service, enabled bool) {
	t.Helper()
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", "owner", struct{ WorkbenchVisible bool }{enabled})
	}); err != nil {
		t.Fatal(err)
	}
}
func TestCaptureIsolationRedactionAndSnapshots(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 1, map[string]string{"api_key": "SECRET_KEY"})
	s.Record(ctx, "model.request", "succeeded", "operation", map[string]any{"Authorization": "Bearer SECRET", "Body": `{"password":"SECRET_PASSWORD","messages":[{"content":"hello"}]}`, "Input": "Bearer EXAMPLE_TOKEN"})
	s.Finish(ctx, "succeeded", "")
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(runs.Items) != 1 {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	run, err := s.Get(t.Context(), p, runs.Items[0].ID)
	if err != nil || len(run.Steps) != 3 || run.State != "succeeded" {
		t.Fatalf("run: %+v %v", run, err)
	}
	payload, err := s.Detail(t.Context(), p, run.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Status != "redacted" || strings.Contains(string(payload.Data), "SECRET") || strings.Contains(string(payload.Data), "EXAMPLE_TOKEN") || !strings.Contains(string(payload.Data), "hello") {
		t.Fatalf("unsafe payload: %s", payload.Data)
	}
	foreign := p
	foreign.OwnerID = "other"
	foreign.SubjectID = "other"
	if _, err = s.Get(t.Context(), foreign, run.ID); err == nil {
		t.Fatal("foreign owner accessed trace")
	}
	agent := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
	if _, err = s.Runs(t.Context(), agent, Query{}); err == nil {
		t.Fatal("assistant accessed private debugging")
	}
	if _, err = s.Detail(t.Context(), p, run.ID, 99); core.CodeOf(err) != core.NotFound {
		t.Fatal("missing step not rejected")
	}
	revision, err := s.Revision(t.Context(), p)
	if err != nil || revision < 3 {
		t.Fatal("revision did not advance")
	}
	setVisible(t, s, false)
	if _, err = s.Detail(t.Context(), p, run.ID, 2); err != nil {
		t.Fatal("hidden workbench lost detail access", err)
	}
	if _, err = s.Revision(t.Context(), p); err != nil {
		t.Fatal("hidden workbench lost stream access", err)
	}
	setVisible(t, s, true)
	s.options.Enabled = false
	if _, err = s.Runs(t.Context(), p, Query{}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("runtime disabled access")
	}
}
func TestLimitsRecoveryAndDistinctAttempts(t *testing.T) {
	s, p, now := fixture(t)
	first := s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(first, "context", "succeeded", "", strings.Repeat("x", 4096))
	s.Finish(first, "failed", "context_limit")
	*now = now.Add(time.Second)
	_ = s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 2, nil)
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(t.Context(), p, Query{Page: core.PageRequest{Limit: 1}})
	if err != nil || len(runs.Items) != 1 || runs.NextCursor == "" || runs.Items[0].Attempt != 2 || runs.Items[0].State != "interrupted" {
		t.Fatalf("attempt/recovery: %+v %v", runs, err)
	}
	older, err := s.Runs(t.Context(), p, Query{Page: core.PageRequest{Limit: 1, Cursor: runs.NextCursor}})
	if err != nil || len(older.Items) != 1 || !older.Items[0].Incomplete {
		t.Fatalf("limit not reported: %+v %v", older, err)
	}
	payload, err := s.Detail(t.Context(), p, older.Items[0].ID, 2)
	if err != nil || payload.Status != "too_large" || (len(payload.Data) > 0 && string(payload.Data) != "null") {
		t.Fatal("oversized payload persisted")
	}
	*now = now.AddDate(0, 0, 8)
	run := s.Begin(t.Context(), "owner", "assistant", "later", "later", "resume_requested", 1, nil)
	s.Finish(run, "succeeded", "")
	if _, err = s.Get(t.Context(), p, older.Items[0].ID); core.CodeOf(err) != core.NotFound {
		t.Fatal("expired run retained")
	}
	for i := 0; i < 6; i++ {
		*now = now.Add(time.Second)
		ctx := s.Begin(t.Context(), "owner", "assistant", core.ID("event"+strings.Repeat("x", i)), "run", "user_message", 1, nil)
		s.Finish(ctx, "succeeded", "")
	}
	rows, err := persistence.Scan[Run](t.Context(), s.store, "debug.runs")
	if err != nil || len(rows) > s.options.MaxRuns {
		t.Fatalf("run retention unbounded: %d %v", len(rows), err)
	}
}
func TestCheckpointOwnerOnlyAndObservationFailureDoesNotFailExecution(t *testing.T) {
	s, p, _ := fixture(t)
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", "owner/assistant", map[string]string{"ID": "assistant"}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.checkpoints", "owner/assistant", json.RawMessage(`{"Context":{"Inputs":[{"Role":"tool","Text":"{\"api_key\":\"secret\"}"}]}}`))
	}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.Checkpoint(t.Context(), p, "assistant")
	if err != nil || checkpoint.Status != "redacted" || strings.Contains(string(checkpoint.Data), "secret") {
		t.Fatal("checkpoint not redacted")
	}
	ctx := s.Begin(t.Context(), "owner", "assistant", "event", "execution", "user_message", 1, nil)
	s.store.(*sqlitestore.Store).FailNextCommit(core.NewError(core.Unavailable, "test"))
	s.Record(ctx, "model.start", "running", "", nil)
	s.Finish(ctx, "succeeded", "")
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || !runs.Items[0].Incomplete {
		t.Fatal("capture failure was hidden")
	}
}

func TestResetControlGatesMutationAndNotifiesOnlyOnSuccess(t *testing.T) {
	s, p, _ := fixture(t)
	var control Control = s
	calls := 0
	sequence := uint64(0)
	req := ResetRequest{Mutation: core.Mutation{IdempotencyKey: "reset"}, ExpectedSequence: &sequence}
	s.options.ResetContext = func(_ context.Context, principal core.Principal, id core.ID, m core.Mutation, expected uint64) error {
		calls++
		if principal != p || id != "assistant" || m.IdempotencyKey != "reset" || expected != 0 {
			t.Fatal("reset arguments changed")
		}
		return core.NewError(core.Conflict, "checkpoint changed")
	}
	if core.CodeOf(control.ResetContext(t.Context(), p, "assistant", req)) != core.Conflict {
		t.Fatal("reset error hidden")
	}
	revision, err := control.Revision(t.Context(), p)
	if err != nil || revision != 0 {
		t.Fatal("failed reset emitted revision")
	}
	s.options.ResetContext = func(context.Context, core.Principal, core.ID, core.Mutation, uint64) error { calls++; return nil }
	if err := control.ResetContext(t.Context(), p, "assistant", req); err != nil {
		t.Fatal(err)
	}
	revision, err = control.Revision(t.Context(), p)
	if err != nil || revision != 1 {
		t.Fatal("successful reset did not notify")
	}
	s.options.Enabled = false
	if core.CodeOf(control.ResetContext(t.Context(), p, "assistant", req)) != core.Forbidden || calls != 2 {
		t.Fatal("disabled workbench reached reset callback")
	}
}

func TestDreamDebugRecordsSurviveNormalRetention(t *testing.T) {
	s, p, clock := fixture(t)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "dream-event", "execution", "dream_due", 1, map[string]any{"Kind": "dream_due"})
	s.Record(ctx, "model.start", "succeeded", "model-op", map[string]string{"Input": "retained dream input"})
	s.Finish(ctx, "succeeded", "")
	before, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(before.Items) != 1 {
		t.Fatal(before, err)
	}
	id := before.Items[0].ID
	*clock = clock.AddDate(0, 0, 30)
	for i := 0; i < 5; i++ {
		normal := s.Begin(t.Context(), p.OwnerID, "assistant", core.ID(fmt.Sprintf("normal-%d", i)), "execution", "user_message", 1, nil)
		s.Record(normal, "model.start", "succeeded", "model-op", map[string]string{"Input": "normal"})
		s.Finish(normal, "succeeded", "")
	}
	retained, err := s.Get(t.Context(), p, id)
	if err != nil || retained.Trigger != "dream_due" {
		t.Fatal("dream trace was pruned", retained, err)
	}
	payload, err := s.Detail(t.Context(), p, id, 2)
	if err != nil || !strings.Contains(string(payload.Data), "retained dream input") {
		t.Fatal(payload, err)
	}
}

func TestCheckpointRecordingScopeAndDeploymentGate(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.RecordCheckpoint(ctx, p.OwnerID, "assistant", "event", "execution", map[string]any{"Sequence": 1})
	s.RecordCheckpoint(ctx, p.OwnerID, "other-assistant", "other-event", "control", map[string]any{"Sequence": 2})
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(runs.Items) != 2 {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	for _, summary := range runs.Items {
		run, err := s.Get(t.Context(), p, summary.ID)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, step := range run.Steps {
			if step.Kind != "checkpoint.saved" {
				continue
			}
			count++
			payload, err := s.Detail(t.Context(), p, run.ID, step.Sequence)
			if err != nil {
				t.Fatal(err)
			}
			want := `"Sequence":1`
			if run.AssistantID == "other-assistant" {
				want = `"Sequence":2`
			}
			if !strings.Contains(string(payload.Data), want) {
				t.Fatalf("wrong run: %+v %s", run, payload.Data)
			}
		}
		if count != 1 {
			t.Fatalf("save count: %d", count)
		}
	}
	s.options.Enabled = false
	s.RecordCheckpoint(t.Context(), p.OwnerID, "assistant", "new-event", "new-execution", nil)
	s.options.Enabled = true
	after, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(after.Items) != 2 {
		t.Fatal("disabled deployment created trace")
	}
}

func TestCaptureIgnoresVisibilityAndStopsOnlyWithDeployment(t *testing.T) {
	s, p, _ := fixture(t)
	// The fixture has no settings row: new accounts record before visiting settings.
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.start", "started", "op", map[string]string{"Text": "before showing"})
	setVisible(t, s, true)
	s.Record(ctx, "model.response", "succeeded", "op", map[string]string{"Text": "while shown"})
	setVisible(t, s, false)
	s.Record(ctx, "model.complete", "succeeded", "op", map[string]string{"Text": "after hiding"})
	s.Finish(ctx, "succeeded", "")
	setVisible(t, s, true)
	runs, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(runs.Items) != 1 {
		t.Fatal(runs, err)
	}
	run, err := s.Get(t.Context(), p, runs.Items[0].ID)
	if err != nil || run.Incomplete || len(run.Steps) != 5 {
		t.Fatal(run, err)
	}
	for i, want := range []string{"before showing", "while shown", "after hiding"} {
		payload, err := s.Detail(t.Context(), p, run.ID, i+2)
		if err != nil || !strings.Contains(string(payload.Data), want) {
			t.Fatal(payload, err)
		}
	}
	s.options.Enabled = false
	before, err := persistence.Scan[Payload](t.Context(), s.store, "debug.payloads")
	if err != nil {
		t.Fatal(err)
	}
	s.Record(ctx, "model.response", "succeeded", "op", "disabled deployment")
	blocked := s.Begin(t.Context(), p.OwnerID, "assistant", "new-event", "new-execution", "user_message", 1, nil)
	if blocked.Value(contextKey{}) != nil {
		t.Fatal("disabled deployment started recording")
	}
	s.RecordCheckpoint(t.Context(), p.OwnerID, "assistant", "checkpoint", "checkpoint", nil)
	after, err := persistence.Scan[Payload](t.Context(), s.store, "debug.payloads")
	if err != nil || len(after) != len(before) {
		t.Fatal("disabled deployment wrote payloads", err)
	}
	if _, err := s.Detail(t.Context(), p, run.ID, 2); core.CodeOf(err) != core.Forbidden {
		t.Fatal("disabled deployment allowed reading")
	}
}
