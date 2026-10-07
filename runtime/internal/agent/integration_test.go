package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDAgentUsesExplicitHistoryAndPublishingToolsOverHTTP(t *testing.T) {
	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad input", 400)
			return
		}
		step := calls.Add(1)
		message := map[string]any{"role": "assistant", "content": ""}
		finish := "tool_calls"
		switch step {
		case 1:
			if strings.Contains(string(body), "raw-chat-not-implicit-context") {
				t.Error("initial HTTP request leaked raw chat history")
			}
			message["tool_calls"] = []any{map[string]any{"id": "read-call", "type": "function", "function": map[string]any{"name": "conversation_read", "arguments": `{"conversation_id":"chat","limit":10}`}}}
		case 2:
			if !strings.Contains(string(body), "raw-chat-not-implicit-context") {
				t.Error("explicit history tool result missing from model context")
			}
			message["tool_calls"] = []any{map[string]any{"id": "publish-call", "type": "function", "function": map[string]any{"name": "conversation_publish", "arguments": `{"conversation_id":"chat","text":"delivered"}`}}}
		default:
			message["content"] = "private internal completion"
			finish = "stop"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 3}})
	})
	f, db := makeLoopFixture(t, handler)
	ctx := testkit.Context(t)
	out, err := f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if calls.Load() != 3 || out.Status.State != Idle {
		t.Fatalf("tool loop calls=%d outcome=%+v", calls.Load(), out.Status)
	}
	page, err := conversation.NewRepository(db).Messages(ctx, f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 2 || page.Items[1].Sender != conversation.Assistant || page.Items[1].Content.Text != "delivered" {
		t.Fatalf("visible conversation = %+v", page)
	}
	if f.VisibleMessageCount() != 1 {
		t.Fatal("internal model text was automatically published")
	}
	_, err = f.Restart().Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if calls.Load() != 3 {
		t.Fatal("restart repeated completed model calls")
	}
}
func TestTDDAgentTaskEventBindsEveryExecutionToTask(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	ctx := testkit.Context(t)
	// The real policy repository reads the existing task's persisted budget.
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.tasks", persistence.Key(f.Owner.OwnerID, "task"), map[string]any{"ID": "task", "OwnerID": f.Owner.OwnerID, "AssistantID": "assistant", "State": "running", "Budget": core.Money{Currency: "USD", Micros: 1000000}})
	}))
	turn := f.Turn
	turn.Event.Kind = event.ResumeRequested
	turn.Event.MessageID = ""
	turn.Event.TaskID = "task"
	out, err := f.Runtime.Handle(ctx, turn)
	testkit.RequireNoError(t, err)
	if out.Status.ActiveTaskID != "task" {
		t.Fatal("task binding missing from status")
	}
	for _, request := range f.ModelRequests() {
		if request.Execution.TaskID != "task" {
			t.Fatal("model execution lost task binding")
		}
	}
	ops, err := persistence.Scan[operation.Operation](ctx, db, "agent.operations")
	testkit.RequireNoError(t, err)
	if len(ops) != 1 || ops[0].TaskID != "task" {
		t.Fatalf("operation task binding = %+v", ops)
	}
}

func TestTDDAgentCanPublishProductCardWithFixedTool(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	ctx := testkit.Context(t)
	engine := f.Runtime.(*Engine)
	execution := core.Execution{Principal: f.Turn.Principal, AssistantID: f.Turn.Event.AssistantID, OperationID: "card-publication"}
	_, err := engine.options.Dependencies.Tools.Execute(ctx, tools.Call{Execution: execution, Name: "conversation_publish", Arguments: json.RawMessage(`{"conversation_id":"chat","card":{"kind":"task","target_id":"task","title":"Task ready","summary":"Review the result"}}`)})
	testkit.RequireNoError(t, err)
	page, err := conversation.NewRepository(db).Messages(ctx, f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 2 || page.Items[1].Content.Kind != conversation.Card || page.Items[1].Content.Card.TargetID != "task" {
		t.Fatalf("card publication = %+v", page)
	}
}

func TestTDDAgentRehireDoesNotReviveOldJobs(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "lifecycle.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	engine := NewRuntime(db, Options{Dependencies: Dependencies{Policy: fixtureAuthorizer{}}})
	testkit.RequireNoError(t, engine.Lifecycle(ctx, owner, "assistant", "initialize"))
	source := event.AssistantEvent{ID: "running", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now()}
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.jobs", persistence.Key("owner", source.ID), scheduler.Job{Event: source, State: scheduler.Running, ExecutionID: "old-run", Version: 1})
	}))
	pending := source
	pending.ID = "pending"
	testkit.RequireNoError(t, event.NewWriter(db).AppendAssistant(ctx, pending))
	testkit.RequireNoError(t, engine.Lifecycle(ctx, owner, "assistant", "stop"))
	testkit.RequireNoError(t, engine.Lifecycle(ctx, owner, "assistant", "restore"))
	jobs, err := scheduler.NewQueue(db).List(ctx, "owner", scheduler.Query{})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 2 {
		t.Fatalf("old work disappeared: %+v", jobs)
	}
	for _, job := range jobs.Items {
		if job.State != scheduler.Canceled {
			t.Fatalf("rehire revived job: %+v", job)
		}
	}
	_, err = scheduler.NewQueue(db).Claim(ctx, time.Now().Add(time.Hour))
	testkit.RequireCode(t, err, core.NotFound)
	status, err := engine.Status(ctx, owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Idle || status.ExecutionID != "" {
		t.Fatalf("rehire status = %+v", status)
	}
}

func TestConversationHistoryReadsLatestPagesAndExactEvent(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	ctx := t.Context()
	repo := conversation.NewRepository(db)
	for i := 0; i < 250; i++ {
		_, err := repo.Append(ctx, f.Owner.OwnerID, conversation.Message{ID: core.ID(fmt.Sprintf("message-%03d", i)), ConversationID: "chat", Sender: conversation.User, SenderID: f.Owner.SubjectID, Content: conversation.Content{Kind: conversation.Text, Text: fmt.Sprintf("text-%03d", i)}, CreatedAt: time.Now()})
		testkit.RequireNoError(t, err)
	}
	ex := f.Runtime.(*Engine).options.Dependencies.Tools
	read := func(id string, args map[string]any) core.Page[conversation.Message] {
		t.Helper()
		raw, _ := json.Marshal(args)
		out, err := ex.Execute(ctx, tools.Call{Execution: core.Execution{Principal: f.Turn.Principal, AssistantID: "assistant", OperationID: core.ID(id)}, Name: "conversation_read", Arguments: raw})
		testkit.RequireNoError(t, err)
		var page core.Page[conversation.Message]
		testkit.RequireNoError(t, json.Unmarshal(out.Content, &page))
		return page
	}
	latest := read("latest", map[string]any{"conversation_id": "chat", "limit": 2})
	if len(latest.Items) != 2 || latest.Items[0].ID != "message-248" || latest.Items[1].ID != "message-249" || latest.NextCursor == "" {
		t.Fatalf("latest=%+v", latest)
	}
	older := read("older", map[string]any{"conversation_id": "chat", "limit": 2, "cursor": latest.NextCursor})
	if len(older.Items) != 2 || older.Items[0].ID != "message-246" || older.Items[1].ID != "message-247" {
		t.Fatalf("older=%+v", older)
	}
	// Walk all older pages and check chronological order and complete coverage.
	page := latest
	expectedNewest := 249
	for {
		for i := len(page.Items) - 1; i >= 0; i-- {
			want := core.ID(fmt.Sprintf("message-%03d", expectedNewest))
			if expectedNewest == -1 {
				want = "history"
			}
			if page.Items[i].ID != want {
				t.Fatalf("history: got %s, want %s", page.Items[i].ID, want)
			}
			expectedNewest--
		}
		if page.NextCursor == "" {
			break
		}
		page = read(fmt.Sprintf("page-%d", expectedNewest), map[string]any{"conversation_id": "chat", "limit": 37, "cursor": page.NextCursor})
	}
	if expectedNewest != -2 {
		t.Fatalf("history incomplete: next expected index %d", expectedNewest)
	}
	single := read("single", map[string]any{"conversation_id": "chat", "limit": 1})
	if len(single.Items) != 1 || single.Items[0].ID != "message-249" || single.NextCursor == "" {
		t.Fatalf("single=%+v", single)
	}
	exact := read("exact", map[string]any{"conversation_id": "chat", "message_id": "message-025"})
	if len(exact.Items) != 1 || exact.Items[0].ID != "message-025" {
		t.Fatalf("exact=%+v", exact)
	}
	testkit.RequireNoError(t, repo.Insert(ctx, conversation.Conversation{ID: "other-chat", OwnerID: f.Owner.OwnerID, PeerID: "other", Version: 1}))
	hidden := read("hidden", map[string]any{"conversation_id": "chat", "message_id": "not-in-chat"})
	if len(hidden.Items) != 0 {
		t.Fatal("message filter escaped conversation")
	}
}

func TestLoopReturnsToolErrorsToModel(t *testing.T) {
	var count atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := count.Add(1)
		message := map[string]any{"role": "assistant", "content": ""}
		finish := "tool_calls"
		name, args, id := "conversation_read", `{"conversation_id":"chat","bogus":true}`, "bad-arguments"
		switch n {
		case 1:
		case 2:
			if !strings.Contains(string(body), `execution_state`) || !strings.Contains(string(body), `invalid_argument`) {
				t.Error("model did not receive safe tool error")
			}
			name, args, id = "conversation_read", `{"conversation_id":"chat","message_id":"history"}`, "corrected-read"
		case 3:
			if !strings.Contains(string(body), "raw-chat-not-implicit-context") {
				t.Error("corrected history read missing")
			}
			name, args, id = "conversation_publish", `{"conversation_id":"chat","text":"recovered"}`, "publish"
		default:
			finish = "stop"
			message["content"] = "done"
		}
		if finish == "tool_calls" {
			message["tool_calls"] = []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 3}})
	})
	f, _ := makeLoopFixture(t, handler)
	out, err := f.Runtime.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if count.Load() != 4 || out.Status.State != Idle || f.VisibleMessageCount() != 1 {
		t.Fatalf("failed to recover: %d %+v", count.Load(), out.Status)
	}
}
