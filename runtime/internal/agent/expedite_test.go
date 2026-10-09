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

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func awaitExpedite[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for execution")
		var v T
		return v
	}
}
func expeditedMessage(t *testing.T, e *Engine, p core.Principal, text string) conversation.Message {
	t.Helper()
	m, err := e.options.Conversations.SendUser(t.Context(), p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: string(stableID(text))}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: text}})
	testkit.RequireNoError(t, err)
	return m
}
func requestExpedite(t *testing.T, e *Engine, p core.Principal, m conversation.Message) ExpediteStatus {
	t.Helper()
	s, err := e.Expedite(t.Context(), p, "assistant", ExpediteRequest{Mutation: core.Mutation{IdempotencyKey: "urgent-" + string(m.ID)}, ConversationID: "chat", ThroughSequence: m.Sequence})
	testkit.RequireNoError(t, err)
	return s
}
func completeModel(w http.ResponseWriter, calls []any) {
	m := map[string]any{"role": "assistant", "content": "private"}
	finish := "stop"
	if len(calls) > 0 {
		m["tool_calls"] = calls
		finish = "tool_calls"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "receipt", "choices": []any{map[string]any{"message": m, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 2}})
}
func TestExpediteCancelsModelReadsOrderedMessagesAndConsumesQueueOnce(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(map[bool]string{false: "active-engine", true: "shared-store-engine"}[separate], func(t *testing.T) {
			testExpediteModelDelivery(t, separate)
		})
	}
}
func testExpediteModelDelivery(t *testing.T, separate bool) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var calls atomic.Int32
	second := make(chan string, 2)
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-r.Context().Done():
				close(canceled)
			case <-t.Context().Done():
			}
			return
		}
		second <- string(body)
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	done := make(chan error, 1)
	go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
	awaitExpedite(t, started)
	// The triggering message is still unread while the first model request runs.
	statusBefore, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if statusBefore.State != Working || statusBefore.TriggerMessageID != f.Turn.Event.MessageID {
		t.Fatal("working state lacks its trigger", statusBefore)
	}
	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat", MessageID: f.Turn.Event.MessageID})
	testkit.RequireNoError(t, err)
	trigger := page.Items[0]
	if trigger.ReadAt != nil {
		t.Fatal("trigger was prematurely marked read")
	}
	if result := requestExpedite(t, e, f.Owner, trigger); result.State != "not_needed" {
		t.Fatal("trigger-only request must not interrupt its own model", result)
	}
	if _, err := persistence.Load[expediteRecord](t.Context(), db, expediteBucket, "owner/assistant"); !persistence.Missing(err) {
		t.Fatal("trigger-only request wrote an inbox", err)
	}
	restored, err := f.Restart().Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if restored.TriggerMessageID != trigger.ID {
		t.Fatal("restart lost trigger", restored)
	}
	one := expeditedMessage(t, e, f.Owner, "first correction")
	two := expeditedMessage(t, e, f.Owner, "second correction")
	sender := e
	if separate {
		sender = NewRuntime(db, e.options)
	}
	status := requestExpedite(t, sender, f.Owner, two)
	if status.ThroughSequence != two.Sequence {
		t.Fatal(status)
	}
	awaitExpedite(t, canceled)
	body := awaitExpedite(t, second)
	testkit.RequireNoError(t, awaitExpedite(t, done))
	if strings.Contains(body, f.ChatSentinel) {
		t.Fatal("trigger was injected as expedited input", body)
	}
	if !strings.Contains(body, "first correction") || strings.Index(body, "first correction") > strings.Index(body, "second correction") {
		t.Fatal("messages missing or reordered", body)
	}
	for _, m := range []conversation.Message{one, two} {
		page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat", MessageID: m.ID})
		testkit.RequireNoError(t, err)
		if page.Items[0].ReadAt == nil {
			t.Fatal("model input was not marked read")
		}
		job, err := persistence.Load[scheduler.Job](t.Context(), db, "agent.jobs", persistence.Key(f.Owner.OwnerID, scheduler.UserMessageEvent(f.Owner.OwnerID, "assistant", m).ID))
		testkit.RequireNoError(t, err)
		if job.State != scheduler.Done || job.JoinedTo != "" {
			t.Fatal("expedited message not consumed", job)
		}
	}
	// The original IM outbox can arrive after the joined jobs commit.
	consumer := scheduler.NewMessageConsumer(db, scheduler.Options{})
	pending, err := conversation.NewOutbox(db).Pending(t.Context(), 100)
	testkit.RequireNoError(t, err)
	for _, m := range pending {
		testkit.RequireNoError(t, consumer.AcceptMessage(t.Context(), m))
	}
	_, err = scheduler.NewQueue(db).Claim(t.Context(), time.Now())
	testkit.RequireCode(t, err, core.NotFound)
	final, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if final.TriggerMessageID != "" {
		t.Fatal("completed turn retained trigger", final)
	}
	if result := requestExpedite(t, e, f.Owner, trigger); result.State != "not_needed" {
		t.Fatal("no-op replay changed", result)
	}
	requestExpedite(t, e, f.Owner, two) // retry after completion is idempotent
	if calls.Load() != 3 {
		t.Fatal("duplicate execution", calls.Load())
	}
}

type blockingExpediteTools struct {
	tools.Executor
	started chan struct{}
	release chan struct{}
	count   atomic.Int32
}

func (b *blockingExpediteTools) Execute(ctx context.Context, c tools.Call) (tools.Result, error) {
	if c.Name == "conversation_publish" {
		b.count.Add(1)
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return tools.Result{}, ctx.Err()
		}
	}
	return b.Executor.Execute(ctx, c)
}
func TestExpediteWaitsForRunningToolAndSkipsRemainingCalls(t *testing.T) {
	var calls atomic.Int32
	after := make(chan string, 1)
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			call := func(id, text string) any {
				return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "conversation_publish", "arguments": `{"conversation_id":"chat","text":"` + text + `"}`}}
			}
			completeModel(w, []any{call("first", "already running"), call("second", "must not publish")})
			return
		}
		after <- string(body)
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	wrapped := &blockingExpediteTools{Executor: e.options.Dependencies.Tools, started: make(chan struct{}), release: make(chan struct{})}
	e.options.Dependencies.Tools = wrapped
	done := make(chan error, 1)
	go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
	awaitExpedite(t, wrapped.started)
	m := expeditedMessage(t, e, f.Owner, "cancel remaining publications")
	if s := requestExpedite(t, e, f.Owner, m); s.State != "waiting_tool" {
		t.Fatal(s)
	}
	requestExpedite(t, e, f.Owner, m)
	select {
	case err := <-done:
		t.Fatal("tool was interrupted", err)
	default:
	}
	close(wrapped.release)
	body := awaitExpedite(t, after)
	testkit.RequireNoError(t, awaitExpedite(t, done))
	if wrapped.count.Load() != 1 || !strings.Contains(body, "not executed: user expedited") || !strings.Contains(body, "cancel remaining publications") {
		t.Fatal("tool batch did not yield", body)
	}
	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	for _, m := range page.Items {
		if m.Content.Text == "must not publish" {
			t.Fatal("remaining side effect executed")
		}
	}
}
func TestExpediteAuthorizationBoundsAndStop(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	m := expeditedMessage(t, e, f.Owner, "unread")
	r := ExpediteRequest{Mutation: core.Mutation{IdempotencyKey: "test"}, ConversationID: "chat", ThroughSequence: m.Sequence}
	if _, err := e.Expedite(t.Context(), f.Other, "assistant", r); err == nil {
		t.Fatal("foreign owner expedited")
	}
	if _, err := e.Expedite(t.Context(), f.Turn.Principal, "assistant", r); err == nil {
		t.Fatal("assistant expedited itself")
	}
	s, err := e.Expedite(t.Context(), f.Owner, "assistant", r)
	testkit.RequireNoError(t, err)
	if s.State != "not_running" {
		t.Fatal(s)
	}
	r.ThroughSequence = m.Sequence + 1
	if _, err := e.Expedite(t.Context(), f.Owner, "assistant", r); err == nil {
		t.Fatal("future message accepted")
	}
	testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
	r.ThroughSequence = m.Sequence
	s, err = e.Expedite(t.Context(), f.Owner, "assistant", r)
	testkit.RequireNoError(t, err)
	if s.State != "not_running" {
		t.Fatal("expedite revived stopped assistant")
	}
}

func TestExpeditePersistedInboxSurvivesRestartAndRespectsCutoff(t *testing.T) {
	body := make(chan string, 2)
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body <- string(data)
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, statusBucket, "owner/assistant", Status{AssistantID: "assistant", State: Working, ExecutionID: "run"})
	}))
	target := expeditedMessage(t, e, f.Owner, "before restart")
	requestExpedite(t, e, f.Owner, target)
	later := expeditedMessage(t, e, f.Owner, "after cutoff")
	restarted := f.Restart()
	_, err := restarted.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	request := awaitExpedite(t, body)
	if !strings.Contains(request, "before restart") || strings.Contains(request, "after cutoff") {
		t.Fatal("durable inbox or cutoff lost", request)
	}
	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat", MessageID: later.ID})
	testkit.RequireNoError(t, err)
	if page.Items[0].ReadAt != nil {
		t.Fatal("later message incorrectly read")
	}
	if reminder := awaitExpedite(t, body); !strings.Contains(reminder, "Did you forget to send a message?") {
		t.Fatal("missing one-time reminder", reminder)
	}
	_, err = restarted.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	select {
	case <-body:
		t.Fatal("committed turn executed twice")
	default:
	}
}

func TestExpediteRestartAfterIntakeRetainsJoinedContext(t *testing.T) {
	body := make(chan string, 2)
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body <- string(data)
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, statusBucket, "owner/assistant", Status{AssistantID: "assistant", State: Working, ExecutionID: "run"})
	}))
	m := expeditedMessage(t, e, f.Owner, "durable takeover")
	requestExpedite(t, e, f.Owner, m)
	inputs, err := e.builder.Build(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	progress := turnProgress{Inputs: inputs}
	testkit.RequireNoError(t, e.intakeExpedite(t.Context(), f.Turn, &progress))
	pending, err := conversation.NewOutbox(db).Pending(t.Context(), 100)
	testkit.RequireNoError(t, err)
	consumer := scheduler.NewMessageConsumer(db, scheduler.Options{})
	for _, input := range pending {
		testkit.RequireNoError(t, consumer.AcceptMessage(t.Context(), input))
	}
	_, err = scheduler.NewQueue(db).Claim(t.Context(), time.Now())
	testkit.RequireCode(t, err, core.NotFound)
	_, err = f.Restart().Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if request := awaitExpedite(t, body); strings.Count(request, "durable takeover") != 1 {
		t.Fatal("joined message lost or repeated", request)
	}
	_, err = scheduler.NewQueue(db).Claim(t.Context(), time.Now())
	testkit.RequireCode(t, err, core.NotFound)
}

func TestSchedulerAcceptanceDoesNotMarkMessageRead(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	m := expeditedMessage(t, e, f.Owner, "still unread")
	testkit.RequireNoError(t, e.options.Conversations.AcceptUserMessage(t.Context(), core.Execution{Principal: f.Turn.Principal, AssistantID: "assistant", OperationID: "event"}, m.ID))
	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat", MessageID: m.ID})
	testkit.RequireNoError(t, err)
	if page.Items[0].AcceptedAt == nil || page.Items[0].ReadAt != nil {
		t.Fatal("acceptance confused with actual delivery")
	}
}

type failAfterExpediteTool struct {
	tools.Executor
	fail   func()
	failed bool
}

func (b *failAfterExpediteTool) Execute(ctx context.Context, c tools.Call) (tools.Result, error) {
	result, err := b.Executor.Execute(ctx, c)
	if c.Name == "conversation_publish" && !b.failed {
		b.failed = true
		b.fail()
	}
	return result, err
}
func TestExpediteRecoveryKeepsCompletedToolResult(t *testing.T) {
	var calls atomic.Int32
	body := make(chan string, 1)
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			tool := func(id, text string) any {
				return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "conversation_publish", "arguments": `{"conversation_id":"chat","text":"` + text + `"}`}}
			}
			completeModel(w, []any{tool("completed", "completed-before-crash"), tool("unstarted", "must-not-run")})
			return
		}
		body <- string(data)
		completeModel(w, nil)
	}))
	e := f.Runtime.(*Engine)
	e.options.Dependencies.Tools = &failAfterExpediteTool{Executor: e.options.Dependencies.Tools, fail: f.FailNextCommit}
	_, err := e.Handle(t.Context(), f.Turn)
	if err == nil {
		t.Fatal("expected failed tool reply commit")
	}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, statusBucket, "owner/assistant", Status{AssistantID: "assistant", State: Working, ExecutionID: "run"})
	}))
	m := expeditedMessage(t, e, f.Owner, "stop unfinished publication")
	requestExpedite(t, e, f.Owner, m)
	_, err = f.Restart().Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	request := awaitExpedite(t, body)
	if !strings.Contains(request, "completed-before-crash") || !strings.Contains(request, "not executed: user expedited") {
		t.Fatal("recovery lost real tool outcome", request)
	}
	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	count := 0
	for _, m := range page.Items {
		if m.Content.Text == "completed-before-crash" {
			count++
		}
		if m.Content.Text == "must-not-run" {
			t.Fatal("unstarted tool executed")
		}
	}
	if count != 1 {
		t.Fatal("completed effect repeated", count)
	}
}
