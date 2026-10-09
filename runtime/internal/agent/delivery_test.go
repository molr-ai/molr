package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func seedCompressionHistory(t *testing.T, f contractFixture) {
	t.Helper()
	inputs := []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("retain original work and evidence ", 500)}}
	raw, _ := json.Marshal(privateContext{Inputs: inputs})
	testkit.RequireNoError(t, f.Checkpoints.Save(t.Context(), Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: "seed-run", LastEventID: "seed-event", Sequence: 1, FormatVersion: 1, Context: raw, SavedAt: time.Now()}, 0))
}
func requestCompression(t *testing.T, e *Engine, p core.Principal, key string) CompressionResult {
	t.Helper()
	out, err := e.Compress(t.Context(), p, "assistant", CompressionRequest{Mutation: core.Mutation{IdempotencyKey: key}})
	testkit.RequireNoError(t, err)
	return out
}
func TestCompressionEventManualIdleDeduplicatesAndSurvivesRestart(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	seedCompressionHistory(t, f)
	request := requestCompression(t, e, f.Owner, "first")
	if next := requestCompression(t, e, f.Owner, "second"); next.EventID != request.EventID {
		t.Fatal("duplicate compression")
	}
	if _, err := e.Compress(t.Context(), f.Other, "assistant", CompressionRequest{Mutation: core.Mutation{IdempotencyKey: "foreign"}}); err == nil {
		t.Fatal("cross owner accepted")
	}
	claim, err := scheduler.NewQueue(db).Claim(t.Context(), time.Now().Add(time.Second))
	testkit.RequireNoError(t, err)
	if claim.Job.Event.Delivery() != event.AfterStep || !claim.Job.Event.Manual {
		t.Fatal(claim.Job)
	}
	restarted := f.Restart()
	out, err := restarted.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event})
	testkit.RequireNoError(t, err)
	if out.Status.State != Idle || out.Status.Context.Compressions != 1 || out.Status.Compression.State != "succeeded" {
		t.Fatal(out.Status)
	}
	if len(f.ModelRequests()) != 1 {
		t.Fatal("compression started a normal conversation", len(f.ModelRequests()))
	}
	if _, err := restarted.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event}); err != nil {
		t.Fatal(err)
	}
	if len(f.ModelRequests()) != 1 {
		t.Fatal("replayed compression")
	}
}
func TestCompressionAfterFinalResponseAndStatus(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	summary, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			completeModel(w, nil)
			return
		}
		if n > 2 {
			completeModel(w, nil) // the reminder remains one-shot after compaction
			return
		}
		close(summary)
		select {
		case <-finish:
		case <-r.Context().Done():
			return
		}
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	seedCompressionHistory(t, f)
	done := make(chan error, 1)
	go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
	awaitExpedite(t, started)
	request := requestCompression(t, e, f.Owner, "during-model")
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.Compression != nil && status.Compression.State == "running" {
		t.Fatal("queued compression presented as running")
	}
	close(release)
	awaitExpedite(t, summary)
	status, err = e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.Compression == nil || status.Compression.State != "running" {
		t.Fatal("missing running state", status)
	}
	close(finish)
	testkit.RequireNoError(t, awaitExpedite(t, done))
	job, err := persistence.Load[scheduler.Job](t.Context(), db, "agent.jobs", persistence.Key(f.Owner.OwnerID, request.EventID))
	testkit.RequireNoError(t, err)
	if job.State != scheduler.Done || calls.Load() != 3 {
		t.Fatal("final-step compression lost or repeated", job, calls.Load())
	}
}
func TestAutomaticCompressionUsesAfterStepEvent(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 32768, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: strings.Repeat("historical data ", 3000)}}}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 31000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
	testkit.RequireNoError(t, e.detectCompression(t.Context(), f.Turn, "model", &p))
	request := requestCompression(t, e, f.Owner, "merge-auto")
	sources, err := persistence.Scan[event.AssistantEvent](t.Context(), db, "agent.events")
	testkit.RequireNoError(t, err)
	if len(sources) != 1 || sources[0].ID != request.EventID || !sources[0].Manual || sources[0].Delivery() != event.AfterStep {
		t.Fatal(sources)
	}
	_, err = e.prepareContext(t.Context(), f.Turn, "model", &p)
	testkit.RequireNoError(t, err)
	jobs, err := scheduler.NewQueue(db).List(t.Context(), f.Owner.OwnerID, scheduler.Query{})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].State != scheduler.Done {
		t.Fatal(jobs)
	}
}
func TestEventDeliveryModesAtModelBoundary(t *testing.T) {
	for _, mode := range []event.DeliveryMode{event.AfterStep, event.Interrupt, event.WhenIdle} {
		t.Run(string(mode), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			body := make(chan string, 2)
			f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if calls.Add(1) == 1 {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				body <- string(raw)
				completeModel(w, nil)
			}))
			e := f.Runtime.(*Engine)
			done := make(chan error, 1)
			go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
			awaitExpedite(t, started)
			source := event.AssistantEvent{ID: "shell-event", OwnerID: f.Owner.OwnerID, AssistantID: "assistant", Kind: event.ShellTaskFinished, OperationID: "shell-op", Details: json.RawMessage(`{"State":"completed"}`), DeliveryMode: mode, CreatedAt: time.Now()}
			testkit.RequireNoError(t, event.NewWriter(db).AppendAssistant(t.Context(), source))
			if mode != event.Interrupt {
				close(release)
			}
			testkit.RequireNoError(t, awaitExpedite(t, done))
			var bodies []string
			for len(body) > 0 {
				bodies = append(bodies, <-body)
			}
			seen := strings.Contains(strings.Join(bodies, "\n"), "shell-event")
			if seen != (mode != event.WhenIdle) {
				t.Fatal("wrong delivery boundary", mode, bodies)
			}
			if mode == event.AfterStep && calls.Load() != 2 {
				t.Fatal("after_step discarded first response")
			}
			cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
			testkit.RequireNoError(t, err)
			if mode != event.WhenIdle && !strings.Contains(string(cp.Context), "shell-event") {
				t.Fatal("injected event not durable")
			}
		})
	}
}

func TestCompressionRetryAfterFinalResponseResumesSummaryOnly(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			completeModel(w, nil)
		case 2:
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"code":"rate_limit_exceeded"}}`)
		default:
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	e.options.ModelRetry = RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
	seedCompressionHistory(t, f)
	done := make(chan error, 1)
	go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
	awaitExpedite(t, started)
	requestCompression(t, e, f.Owner, "retry-summary")
	close(release)
	err := awaitExpedite(t, done)
	if !scheduler.IsDeferred(err) {
		t.Fatal("summary should defer", err)
	}
	time.Sleep(5 * time.Millisecond)
	restarted := NewRuntime(e.store, e.options)
	out, err := restarted.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if calls.Load() != 4 || out.Status.Context.Compressions != 1 || out.Status.State != Idle {
		t.Fatal("summary recovery repeated normal response", calls.Load(), out.Status)
	}
}

type compressionBatchTools struct {
	tools.Executor
	started, release chan struct{}
	count            atomic.Int32
}

func (b *compressionBatchTools) Execute(ctx context.Context, c tools.Call) (tools.Result, error) {
	if c.Name == "conversation_publish" && b.count.Add(1) == 1 {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return tools.Result{}, ctx.Err()
		}
	}
	return b.Executor.Execute(ctx, c)
}
func TestCompressionWaitsForWholeToolBatch(t *testing.T) {
	var calls atomic.Int32
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			completeModel(w, []any{stateTestCall("first", "conversation_publish", `{"conversation_id":"chat","text":"first effect"}`), stateTestCall("second", "conversation_publish", `{"conversation_id":"chat","text":"second effect"}`)})
			return
		}
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	seedCompressionHistory(t, f)
	wrapped := &compressionBatchTools{Executor: e.options.Dependencies.Tools, started: make(chan struct{}), release: make(chan struct{})}
	e.options.Dependencies.Tools = wrapped
	done := make(chan error, 1)
	go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
	awaitExpedite(t, wrapped.started)
	requestCompression(t, e, f.Owner, "whole-batch")
	if calls.Load() != 1 {
		t.Fatal("compressed before tool returned")
	}
	close(wrapped.release)
	testkit.RequireNoError(t, awaitExpedite(t, done))
	if wrapped.count.Load() != 2 || calls.Load() != 3 {
		t.Fatal("after_step skipped tools or continuation", wrapped.count.Load(), calls.Load())
	}
	archives, err := persistence.Scan[contextArchive](t.Context(), e.store, "agent.context_archives")
	testkit.RequireNoError(t, err)
	if len(archives) != 1 {
		t.Fatal(archives)
	}
	replies := 0
	for _, in := range archives[0].Inputs {
		if in.Role == model.Tool {
			replies++
		}
	}
	if replies != 2 {
		t.Fatal("compressed incomplete tool batch", replies)
	}
}
