package objectaccess_test

import (
	"context"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/objectaccess"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnershipUsesDomainRecordsAndAttachmentReferences(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	agent := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
	other := agent
	other.SubjectID = "other"
	foreign := owner
	foreign.OwnerID = "foreign"
	foreign.SubjectID = "foreign"
	rows := []struct {
		bucket, id string
		value      any
	}{
		{"business.assistants", "assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active}},
		{"business.assistants", "clone", assistant.Assistant{ID: "clone", OwnerID: "owner", State: assistant.Cloning}},
		{"business.groups", "group", assistant.Group{ID: "group", OwnerID: "owner"}},
		{"im.conversations", "chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"}},
		{"im.conversations", "cloning-chat", conversation.Conversation{ID: "cloning-chat", OwnerID: "owner", PeerID: "clone"}},
		{"business.tasks", "task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant"}},
		{"business.todos", "todo", todo.Todo{ID: "todo", OwnerID: "owner", AssistantID: "assistant"}},
		{"business.schedules", "schedule", schedule.Schedule{ID: "schedule", OwnerID: "owner", AssistantID: "assistant"}},
		{"agent.operations", "operation", operation.Operation{ID: "operation", OwnerID: "owner", AssistantID: "assistant"}},
		{"security.permission_requests", "permission", policy.PermissionRequest{ID: "permission", OwnerID: "owner", AssistantID: "assistant"}},
		{"business.exports", "export", settings.Export{ID: "export"}},
		{"search.jobs", "search", search.IndexStatus{ID: "search"}},
		{files.Bucket, "direct", files.File{ID: "direct", OwnerID: "owner", AssistantID: "assistant"}},
		{files.Bucket, "task-file", files.File{ID: "task-file", OwnerID: "owner", TaskID: "task"}},
		{files.Bucket, "attached", files.File{ID: "attached", OwnerID: "owner"}},
		{files.Bucket, "unattached", files.File{ID: "unattached", OwnerID: "owner"}},
	}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, row := range rows {
			if err := tx.Put(ctx, row.bucket, "owner/"+row.id, row.value); err != nil {
				return err
			}
		}
		for i, id := range []core.ID{"message", "second"} {
			if err := tx.Put(ctx, "im.messages", persistence.Key("owner", id), conversation.Message{ID: id, ConversationID: "chat", Sequence: uint64(i + 1), Content: conversation.Content{AttachmentIDs: []core.ID{"attached"}}, CreatedAt: time.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := objectaccess.Service{Store: store}
	for _, obj := range []core.Object{{Kind: "assistant", ID: "assistant"}, {Kind: "conversation", ID: "chat"}, {Kind: "message", ID: "message"}, {Kind: "task", ID: "task"}, {Kind: "todo", ID: "todo"}, {Kind: "schedule", ID: "schedule"}, {Kind: "operation", ID: "operation"}, {Kind: "permission_request", ID: "permission"}, {Kind: "file", ID: "direct"}, {Kind: "file", ID: "task-file"}, {Kind: "file", ID: "attached"}} {
		for _, p := range []core.Principal{owner, agent} {
			if err := s.Require(t.Context(), p, obj); err != nil {
				t.Fatalf("%s %v: %v", p.SubjectID, obj, err)
			}
		}
		for _, p := range []core.Principal{other, foreign} {
			if err := s.Require(t.Context(), p, obj); err == nil {
				t.Fatalf("unexpected access: %s %v", p.SubjectID, obj)
			}
		}
	}
	for _, obj := range []core.Object{{Kind: "assistant", ID: "group"}, {Kind: "settings", ID: "export"}, {Kind: "search", ID: "search"}, {Kind: "file", ID: "unattached"}} {
		if err := s.Require(t.Context(), owner, obj); err != nil {
			t.Fatal(obj, err)
		}
		if err := s.Require(t.Context(), agent, obj); err == nil {
			t.Fatal("assistant accessed owner object", obj)
		}
	}
	for _, obj := range []core.Object{{Kind: "assistant", ID: "clone"}, {Kind: "conversation", ID: "cloning-chat"}} {
		if err := s.Require(t.Context(), owner, obj); core.CodeOf(err) != core.Forbidden {
			t.Fatal(obj, err)
		}
	}
	chats, err := conversation.AttachmentConversations(t.Context(), store, "owner", "attached")
	if err != nil || len(chats) != 1 || chats[0] != "chat" {
		t.Fatal(chats, err)
	}
}
