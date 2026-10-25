package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

type checkpointReadCounter struct {
	persistence.Store
	chains int
}

type checkpointCountTx struct {
	persistence.Tx
	owner *checkpointReadCounter
}

func (s *checkpointReadCounter) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, checkpointCountTx{tx, s}) })
}

func (s *checkpointReadCounter) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, checkpointCountTx{tx, s}) })
}

func (tx checkpointCountTx) Ancestors(ctx context.Context, bucket, key, parent string, limit int) ([]persistence.Record, error) {
	tx.owner.chains++
	return tx.Tx.Ancestors(ctx, bucket, key, parent, limit)
}

func TestProgressSavesReadOnlyCheckpointHeader(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	s := &checkpointReadCounter{Store: db}
	e := f.Runtime.(*Engine)
	e.store, e.checkpoints = s, NewCheckpoints(s)
	p := turnProgress{Inputs: []model.Input{{Role: model.User, Text: "retained history"}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	first := p.Revision
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	if p.Revision != first {
		t.Fatal("unchanged save advanced revision")
	}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressAlways))
	p.Inputs = append(p.Inputs, model.Input{Role: model.Assistant, Text: "new result"})
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	if s.chains != 0 {
		t.Fatal("save reconstructed old history", s.chains)
	}
	cp, err := e.checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	state, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if s.chains == 0 || !reflect.DeepEqual(state.Inputs, p.Inputs) {
		t.Fatal("full load did not restore saved history")
	}
}

func TestProgressHeaderRejectsInvalidIdentityAndStorage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storedCheckpoint)
		code   core.Code
	}{
		{"owner", func(c *storedCheckpoint) { c.OwnerID = "another-owner" }, core.Forbidden},
		{"assistant", func(c *storedCheckpoint) { c.AssistantID = "another-assistant" }, core.Forbidden},
		{"storage", func(c *storedCheckpoint) { c.ContextStored = false }, core.FailedPrecondition},
		{"sequence", func(c *storedCheckpoint) { c.Sequence++ }, core.Conflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			p := turnProgress{Inputs: []model.Input{{Role: model.User, Text: "original"}}}
			testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
			testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				var c storedCheckpoint
				key := persistence.Key(f.Owner.OwnerID, f.Turn.Event.AssistantID)
				if err := tx.Get(ctx, checkpointsBucket, key, &c); err != nil {
					return err
				}
				tc.change(&c)
				return tx.Put(ctx, checkpointsBucket, key, c)
			}))
			testkit.RequireCode(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged), tc.code)
			if p.Revision != 1 {
				t.Fatal("failed save advanced memory")
			}
		})
	}
}

func TestProgressEvidenceSurvivesSavesRecoveryAndHandoff(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	call := model.ToolCall{ID: "uncertain", Name: "file_write", Arguments: json.RawMessage(`{"path":"example.txt","text":"value"}`)}
	ref := pendingCall(*cursor(f.Turn, p), call)
	response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{call}}
	p.Response, p.NextTool = &response, 1
	p.Inputs = append(p.Inputs, response, model.Input{Role: model.Tool, ReplyTo: call.ID, ToolOperationID: ref.OperationID, Text: `{"error":{"side_effects_possible":true}}`})
	db.FailNextCommit(errors.New("commit rejected"))
	if err := e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged); err == nil {
		t.Fatal("expected failed commit")
	}
	if p.Revision != 1 || len(p.PendingEvidence) != 0 {
		t.Fatal("failed commit changed progress")
	}
	cp, err := e.checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	if cp.Sequence != 1 {
		t.Fatal("failed commit escaped transaction")
	}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	if len(p.PendingEvidence) != 1 || p.PendingEvidence[0].OperationID != ref.OperationID {
		t.Fatal("uncertain result not carried in progress")
	}
	revision := p.Revision
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	if p.Revision != revision || len(p.PendingEvidence) != 1 {
		t.Fatal("evidence duplicated on unchanged save")
	}
	// Nested saves use a candidate; a later outer failure must preserve both the
	// durable evidence and the caller's previous progress for a retry.
	candidate := p
	second := model.ToolCall{ID: "second-uncertain", Name: call.Name, Arguments: call.Arguments}
	secondResponse := model.Input{Role: model.Assistant, Calls: []model.ToolCall{second}}
	candidate.Response = &secondResponse
	candidate.Inputs = append(append([]model.Input(nil), p.Inputs...), secondResponse, model.Input{Role: model.Tool, ReplyTo: second.ID, ToolOperationID: pendingCall(*cursor(f.Turn, p), second).OperationID, Text: `{"error":{"side_effects_possible":true}}`})
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := e.saveProgress(ctx, f.Turn, &candidate, progressIfChanged); err != nil {
			return err
		}
		return errors.New("outer transaction rolled back")
	})
	if err == nil || p.Revision != revision || len(p.PendingEvidence) != 1 {
		t.Fatal("outer rollback changed caller progress")
	}
	cp, err = e.checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	rolledBack, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if cp.Sequence != revision || !reflect.DeepEqual(rolledBack.PendingEvidence, p.PendingEvidence) {
		t.Fatal("outer rollback changed durable evidence")
	}
	// A later model/tool step no longer points at the uncertain call.
	p.Response, p.NextTool = nil, 0
	p.Step++
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	e = f.Restart().(*Engine)
	_, recovered, err := e.prepareExecution(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if !reflect.DeepEqual(recovered.PendingEvidence, p.PendingEvidence) {
		t.Fatal("recovery lost evidence")
	}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &recovered, progressIfChanged))
	// Handoff must keep unresolved operations and subsequently remove resolved ones.
	op := operation.Operation{ID: ref.OperationID, OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, Action: call.Name, State: operation.Running, Version: 1}
	saveOperation := func() {
		t.Helper()
		testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "agent.operations", persistence.Key(f.Owner.OwnerID, op.ID), op)
		}))
	}
	saveOperation()
	next := nextStateTurn(f, "evidence-next", event.ResumeRequested)
	_, handed, err := e.prepareExecution(t.Context(), next)
	testkit.RequireNoError(t, err)
	if len(handed.PendingEvidence) != 1 {
		t.Fatal("handoff lost unresolved evidence")
	}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), next, &handed, progressIfChanged))
	op.State, op.Result = operation.Succeeded, json.RawMessage(`{"text":"confirmed"}`)
	saveOperation()
	last := nextStateTurn(f, "evidence-resolved", event.ResumeRequested)
	_, resolved, err := e.prepareExecution(t.Context(), last)
	testkit.RequireNoError(t, err)
	if len(resolved.PendingEvidence) != 0 {
		t.Fatal("resolved evidence retained")
	}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), last, &resolved, progressIfChanged))
	cp, err = e.checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	state, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if len(state.PendingEvidence) != 0 {
		t.Fatal("save resurrected resolved evidence")
	}
}
