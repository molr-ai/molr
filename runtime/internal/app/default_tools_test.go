package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/files"
)

func defaultToolFixture(t *testing.T) (resourceAccessFixture, func(string, any) (json.RawMessage, error)) {
	t.Helper()
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	fs := newResourceFileTools(resourceFileOptions{adapters: f.ad})
	ex := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), Conversations: f.b.Services.Conversations.(conversation.Service), ConversationAttachments: &conversationAttachments{files: f.b.Services.Files, workspace: fs, conversations: f.b.Services.Conversations.(conversation.Service)}, Plan: planTools{watches: f.b.Services.Watches, store: f.b.store, plans: f.b.Services.Plans.(*plan.Coordinator), todos: f.b.Services.Todos, schedules: f.b.Services.Schedules}.execute})
	sequence := 0
	return f, func(name string, args any) (json.RawMessage, error) {
		sequence++
		raw, _ := json.Marshal(args)
		e := f.execution
		e.OperationID = core.ID(fmt.Sprintf("tool-%d", sequence))
		out, err := ex.Execute(t.Context(), tools.Call{Execution: e, Name: name, Arguments: raw})
		return out.Content, err
	}
}
func TestPlanToolsCRUDPaginationAndIsolation(t *testing.T) {
	f, call := defaultToolFixture(t)
	manage := func(action string, args map[string]any) (json.RawMessage, error) {
		args["action"] = action
		return call("plan_manage", args)
	}
	create := func(title string) planView {
		t.Helper()
		raw, err := manage("create", map[string]any{"mode": "todo", "title": title, "instructions": "instructions"})
		need(t, err)
		var v planView
		need(t, json.Unmarshal(raw, &v))
		return v
	}
	read := func(id string) planView {
		t.Helper()
		raw, err := call("plan_read", map[string]any{"plan_id": id})
		need(t, err)
		var v planView
		need(t, json.Unmarshal(raw, &v))
		return v
	}
	a, b := create("first"), create("second")
	originalVersion := a.Version
	hidden, err := f.b.Services.Todos.Create(t.Context(), f.owner, todo.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "personal"}, Title: "personal secret"})
	need(t, err)
	raw, err := call("plan_list", map[string]any{"limit": 1})
	need(t, err)
	var page core.Page[planView]
	need(t, json.Unmarshal(raw, &page))
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("page=%s", raw)
	}
	raw, err = call("plan_list", map[string]any{"limit": 1, "cursor": page.NextCursor})
	need(t, err)
	var next core.Page[planView]
	need(t, json.Unmarshal(raw, &next))
	if len(next.Items) != 1 || next.Items[0].PlanID == page.Items[0].PlanID || next.NextCursor != "" {
		t.Fatalf("next=%s", raw)
	}
	if _, err := call("plan_read", map[string]any{"plan_id": "todo:" + string(hidden.ID)}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("foreign plan read: %v", err)
	}
	for _, action := range []string{"update", "delete", "execute"} {
		args := map[string]any{"plan_id": "todo:" + string(hidden.ID), "expected_version": a.Version}
		if action == "update" {
			args["title"] = "stolen"
		}
		if _, err := manage(action, args); core.CodeOf(err) != core.Forbidden {
			t.Fatalf("%s crossed assignment boundary: %v", action, err)
		}
	}
	_, err = manage("update", map[string]any{"plan_id": a.PlanID, "title": "changed", "instructions": "new instructions", "expected_version": a.Version})
	need(t, err)
	a = read(a.PlanID)
	if a.Version == originalVersion || a.Title != "changed" {
		t.Fatal("content update did not change unified version")
	}
	if _, err = manage("update", map[string]any{"plan_id": a.PlanID, "title": "stale", "expected_version": originalVersion}); core.CodeOf(err) != core.Conflict {
		t.Fatalf("stale version accepted: %v", err)
	}
	_, err = manage("update", map[string]any{"plan_id": b.PlanID, "expected_version": b.Version, "title": "with dependency", "dependencies": []string{a.PlanID}, "handoff": "notify", "paused": true})
	need(t, err)
	b = read(b.PlanID)
	if !b.Paused || b.Title != "with dependency" || b.Instructions != "instructions" || len(b.Dependencies) != 1 {
		t.Fatalf("combined partial update=%+v", b)
	}
	// A relation failure must roll back the content update in the same call.
	if _, err = manage("update", map[string]any{"plan_id": a.PlanID, "expected_version": a.Version, "title": "must roll back", "dependencies": []string{"todo:" + string(hidden.ID)}}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("foreign dependency accepted: %v", err)
	}
	after := read(a.PlanID)
	if after.Version != a.Version || after.Title != a.Title {
		t.Fatal("combined update partially committed")
	}
	if _, err = manage("delete", map[string]any{"plan_id": a.PlanID, "expected_version": a.Version}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("deleted dependency: %v", err)
	}
	_, err = manage("delete", map[string]any{"plan_id": b.PlanID, "expected_version": b.Version})
	need(t, err)
	_, err = manage("execute", map[string]any{"plan_id": a.PlanID, "expected_version": a.Version})
	need(t, err)
	a = read(a.PlanID)
	if len(a.Executions) != 1 || a.ExecutionCount != 1 || a.Executions[0].Task != nil {
		t.Fatal("execution summary missing or exposed raw task")
	}
	_, err = manage("update", map[string]any{"plan_id": a.PlanID, "title": "later edit", "instructions": "future instructions", "expected_version": a.Version})
	need(t, err)
	bound := f.execution
	bound.TaskID = a.Executions[0].ID
	bound.OperationID = "read-current-plan"
	input, _ := json.Marshal(map[string]any{"execution_id": bound.TaskID})
	adapter := planTools{watches: f.b.Services.Watches, store: f.b.store, plans: f.b.Services.Plans.(*plan.Coordinator), todos: f.b.Services.Todos, schedules: f.b.Services.Schedules}
	snapshot, err := adapter.execute(t.Context(), bound, "plan_read", input)
	need(t, err)
	var current planView
	need(t, json.Unmarshal(snapshot, &current))
	if current.CurrentExecution == nil || current.CurrentExecution.Instructions != "changed\nnew instructions" || current.Instructions != "future instructions" {
		t.Fatalf("lost execution snapshot: %s", snapshot)
	}
	if _, err = manage("delete", map[string]any{"plan_id": a.PlanID, "expected_version": current.Version}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("deleted execution history: %v", err)
	}
	for _, name := range []string{"plan_create", "plan_update", "plan_delete", "plan_configure", "plan_execute"} {
		if _, err := call(name, map[string]any{}); core.CodeOf(err) != core.NotFound {
			t.Fatalf("legacy tool %s still registered", name)
		}
	}
}

func TestPlanManagePartialUpdatesAndSingleVersion(t *testing.T) {
	f, call := defaultToolFixture(t)
	invoke := func(args map[string]any) planView {
		t.Helper()
		raw, err := call("plan_manage", args)
		need(t, err)
		if bytes.Contains(raw, []byte("LinksVersion")) {
			t.Fatal("internal version leaked")
		}
		var v planView
		need(t, json.Unmarshal(raw, &v))
		return v
	}
	start := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	end := start.Add(time.Hour)
	v := invoke(map[string]any{"action": "create", "mode": "todo", "title": "test", "instructions": "keep", "start_at": start, "end_at": end, "due_at": end})
	initial := v.Version
	v = invoke(map[string]any{"action": "update", "plan_id": v.PlanID, "expected_version": v.Version, "paused": true})
	if v.Version == initial || !v.Paused || v.StartAt == nil || v.Instructions != "keep" {
		t.Fatal("pause changed unrelated fields or did not invalidate version")
	}
	if _, err := call("plan_manage", map[string]any{"action": "update", "plan_id": v.PlanID, "expected_version": initial, "title": "stale after pause"}); core.CodeOf(err) != core.Conflict {
		t.Fatal("relation change did not invalidate content version")
	}
	v = invoke(map[string]any{"action": "update", "plan_id": v.PlanID, "expected_version": v.Version, "start_at": nil, "end_at": nil, "due_at": nil, "instructions": "", "dependencies": []string{}, "paused": false})
	if v.StartAt != nil || v.EndAt != nil || v.DueAt != nil || v.Instructions != "" || v.Paused {
		t.Fatal("explicit clearing was ignored")
	}
	scheduled := invoke(map[string]any{"action": "create", "mode": "assistant_schedule", "title": "reminder", "instructions": "notify", "start_at": start, "timezone": "Asia/Shanghai", "recurrence": "FREQ=DAILY;COUNT=2"})
	scheduledID := core.ID(strings.TrimPrefix(scheduled.PlanID, "schedule:"))
	repo := schedule.NewRepository(f.b.store)
	source, err := repo.Get(t.Context(), f.owner.OwnerID, scheduledID)
	need(t, err)
	nextOccurrence := start.Add(24 * time.Hour)
	source.NextAt = &nextOccurrence
	source.Version++
	need(t, repo.Save(t.Context(), source, source.Version-1))
	readRaw, err := call("plan_read", map[string]any{"plan_id": scheduled.PlanID})
	need(t, err)
	need(t, json.Unmarshal(readRaw, &scheduled))
	scheduled = invoke(map[string]any{"action": "update", "plan_id": scheduled.PlanID, "expected_version": scheduled.Version, "title": "renamed", "paused": true})
	source, err = repo.Get(t.Context(), f.owner.OwnerID, scheduledID)
	need(t, err)
	if source.NextAt == nil || !source.NextAt.Equal(nextOccurrence) {
		t.Fatal("content-only update rewound scheduled progress")
	}

	if scheduled.Title != "renamed" || !scheduled.Paused || scheduled.Recurrence != "FREQ=DAILY;COUNT=2" || scheduled.Instructions != "notify" || scheduled.StartAt == nil || !scheduled.StartAt.Equal(start) {
		t.Fatalf("partial scheduled update=%+v", scheduled)
	}
	// Both old storage revisions participate in the one public version token.
	_, err = f.b.Services.Plans.Configure(t.Context(), f.owner, plan.ConfigureRequest{Ref: core.Object{Kind: "schedule", ID: core.ID(strings.TrimPrefix(scheduled.PlanID, "schedule:"))}, Mutation: core.Mutation{IdempotencyKey: "owner-change", ExpectedVersion: 1}, Dependencies: []core.Object{}, Handoff: "notify", Paused: false})
	need(t, err)
	if _, err := call("plan_manage", map[string]any{"action": "delete", "plan_id": scheduled.PlanID, "expected_version": scheduled.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("owner relation change did not invalidate public version")
	}
}

func TestConversationToolsAttachmentRoundTripAndIsolation(t *testing.T) {
	f, call := defaultToolFixture(t)
	ctx := t.Context()
	repo := conversation.NewRepository(f.b.store)
	need(t, repo.Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: f.owner.OwnerID, PeerID: f.execution.AssistantID, Version: 1}))
	payload := []byte{0, 1, 2, 255, 0, 42}
	upload := func(key string) files.File {
		t.Helper()
		file, err := f.b.Services.Files.Upload(ctx, f.owner, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: key}, Name: "sample.bin", Body: bytes.NewReader(payload), Size: int64(len(payload))})
		need(t, err)
		return file
	}
	file := upload("upload")
	private := upload("private")
	_, err := f.b.Services.Conversations.SendUser(ctx, f.owner, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "message"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, AttachmentIDs: []core.ID{file.ID}}})
	need(t, err)
	raw, err := call("conversation_read", map[string]any{"conversation_id": "chat"})
	need(t, err)
	if !bytes.Contains(raw, []byte("sample.bin")) {
		t.Fatalf("attachment metadata absent: %s", raw)
	}
	_, err = call("conversation_attachment", map[string]any{"conversation_id": "chat", "file_id": file.ID, "path": "inbox/sample.bin"})
	need(t, err)
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	got, err := os.ReadFile(filepath.Join(root, "inbox/sample.bin"))
	need(t, err)
	if !bytes.Equal(got, payload) {
		t.Fatal("binary attachment damaged")
	}
	_, err = call("conversation_attachment", map[string]any{"conversation_id": "chat", "file_id": file.ID, "path": "inbox/sample.bin"})
	if core.CodeOf(err) != core.Conflict {
		t.Fatalf("overwrote existing file: %v", err)
	}
	for _, path := range []string{"../escape", "/etc/passwd"} {
		if _, err := call("conversation_attachment", map[string]any{"conversation_id": "chat", "file_id": file.ID, "path": path}); err == nil {
			t.Fatal("escaped workspace")
		}
	}
	if _, err = call("conversation_attachment", map[string]any{"conversation_id": "chat", "file_id": private.ID, "path": "private.bin"}); core.CodeOf(err) != core.NotFound {
		t.Fatalf("unattached owner file read: %v", err)
	}
	raw, err = call("conversation_publish", map[string]any{"conversation_id": "chat", "attachments": []map[string]any{{"path": "inbox/sample.bin", "name": "result.bin"}}})
	need(t, err)
	var message conversation.Message
	need(t, json.Unmarshal(raw, &message))
	if len(message.Content.AttachmentIDs) != 1 {
		t.Fatalf("missing published attachment: %s", raw)
	}
	download, err := f.b.Services.Files.Open(ctx, f.owner, message.Content.AttachmentIDs[0])
	need(t, err)
	defer download.Body.Close()
	got, err = io.ReadAll(download.Body)
	need(t, err)
	if !bytes.Equal(got, payload) || download.File.AssistantID != f.execution.AssistantID {
		t.Fatal("attachment export damaged or unbound")
	}
	_, err = call("conversation_publish", map[string]any{"conversation_id": "chat", "text": "forward", "attachments": []map[string]any{{"file_id": file.ID}}})
	need(t, err)
	if _, err = call("conversation_publish", map[string]any{"conversation_id": "chat", "attachments": []map[string]any{{"file_id": private.ID}}}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("unattached private file published: %v", err)
	}
	// The owner can read an assistant upload; another assistant cannot claim it.
	other := f.execution.Principal
	other.SubjectID = "other"
	if err := (access{f.b.store}).owns(context.Background(), other, core.Object{Kind: "file", ID: download.File.ID}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("assistant upload leaked: %v", err)
	}
}

func TestPlanToolsWatchLifecycleAndTypeSpecificFields(t *testing.T) {
	_, call := defaultToolFixture(t)
	raw, err := call("plan_manage", map[string]any{"action": "create", "mode": "assistant_watch", "title": "Watch issues", "instructions": "Notify me of new issues", "watcher": map[string]any{"compute_id": "compute-fixture", "script": "exec watcher"}, "paused": true})
	need(t, err)
	var w planView
	need(t, json.Unmarshal(raw, &w))
	if w.Mode != "assistant_watch" || w.Watch == nil || w.EndAt != nil || !w.Paused {
		t.Fatalf("wrong watch projection: %s", raw)
	}
	raw, err = call("plan_list", map[string]any{})
	need(t, err)
	if !strings.Contains(string(raw), w.PlanID) {
		t.Fatal("watch missing from plan_list")
	}
	raw, err = call("plan_read", map[string]any{"plan_id": w.PlanID})
	need(t, err)
	if !strings.Contains(string(raw), "exec watcher") {
		t.Fatal("watch configuration missing")
	}
	raw, err = call("plan_manage", map[string]any{"action": "update", "plan_id": w.PlanID, "expected_version": w.Version, "instructions": "Only relevant issues"})
	need(t, err)
	var updated planView
	need(t, json.Unmarshal(raw, &updated))
	if updated.Instructions != "Only relevant issues" || updated.Watch.Watcher.Script != "exec watcher" || !updated.Paused {
		t.Fatal("watch partial update lost configuration")
	}
	if _, err = call("plan_manage", map[string]any{"action": "update", "plan_id": w.PlanID, "expected_version": updated.Version, "recurrence": "FREQ=MINUTELY"}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("watch accepted recurrence", err)
	}
	if _, err = call("plan_manage", map[string]any{"action": "create", "mode": "todo", "title": "Invalid", "watcher": map[string]any{"compute_id": "compute-fixture", "script": "exec watcher"}}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("todo accepted watcher", err)
	}
	_, err = call("plan_manage", map[string]any{"action": "delete", "plan_id": w.PlanID, "expected_version": updated.Version})
	need(t, err)
}
