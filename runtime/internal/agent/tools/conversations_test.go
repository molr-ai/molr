package tools

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/tests/testkit"
	"testing"
)

type attachmentFixture struct {
	seen      map[string]int
	principal core.Principal
}

func (a attachmentFixture) Get(_ context.Context, p core.Principal, id core.ID) (files.File, error) {
	a.seen["get"]++
	if p != a.principal {
		return files.File{}, core.NewError(core.Forbidden, "identity changed")
	}
	return files.File{ID: id, Name: "attachment"}, nil
}
func (a attachmentFixture) Upload(_ context.Context, e core.Execution, r Attachment, _ int) (core.ID, error) {
	a.seen["upload"]++
	if e.Principal != a.principal || r.Path != "result.txt" {
		return "", core.NewError(core.Forbidden, "identity or path changed")
	}
	return "uploaded", nil
}
func (a attachmentFixture) Import(_ context.Context, e core.Execution, r ConversationRequest) (json.RawMessage, error) {
	a.seen["import"]++
	if e.Principal != a.principal || r.ConversationID != "chat" || r.FileID != "uploaded" {
		return nil, core.NewError(core.Forbidden, "attachment scope changed")
	}
	return json.RawMessage(`{"Path":"/work/result.txt"}`), nil
}
func TestConversationAttachmentDispatchPreservesIdentityAndReferences(t *testing.T) {
	f := newContractFixture(t)
	ex := f.Executor.(*executor)
	ctx := t.Context()
	repo := conversation.NewRepository(ex.store)
	testkit.RequireNoError(t, repo.Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: f.Owner.OwnerID, PeerID: f.Call.Execution.AssistantID, Version: 1}))
	ex.options.Conversations = conversation.NewService(ex.store, conversation.Options{Authorizer: ex.options.Authorizer})
	a := attachmentFixture{map[string]int{}, f.Call.Execution.Principal}
	ex.options.ConversationAttachments = a
	for i, tc := range []struct{ name, raw string }{
		{"conversation_publish", `{"conversation_id":"chat","attachments":[{"path":"result.txt"}]}`},
		{"conversation_read", `{"conversation_id":"chat"}`},
		{"conversation_attachment", `{"conversation_id":"chat","file_id":"uploaded","path":"result.txt"}`},
		{"conversation_publish", `{"conversation_id":"chat","attachments":[{"file_id":"uploaded"}]}`},
	} {
		call := f.Call
		call.Execution.OperationID = core.ID(string(rune('a' + i)))
		call.Name = tc.name
		call.Arguments = json.RawMessage(tc.raw)
		_, err := ex.Execute(ctx, call)
		testkit.RequireNoError(t, err)
	}
	if a.seen["upload"] != 1 || a.seen["import"] != 1 || a.seen["get"] != 2 {
		t.Fatal(a.seen)
	}
	page, err := repo.Messages(ctx, f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 2 || page.Items[0].Content.AttachmentIDs[0] != "uploaded" {
		t.Fatal(page)
	}
}

func TestAttachmentSandboxSelectionIsExplicitAndValidated(t *testing.T) {
	for _, input := range []struct {
		name, args string
		valid      bool
	}{
		{"conversation_publish", `{"conversation_id":"chat","attachments":[{"sandbox_id":"remote","path":"/work/report.txt"}]}`, false},
		{"conversation_publish", `{"conversation_id":"chat","attachments":[{"sandbox_id":"remote","file_id":"file"}]}`, false},
		{"conversation_attachment", `{"conversation_id":"chat","file_id":"file","path":"/work/report.txt","sandbox_id":"remote"}`, false},
		{"conversation_attachment", `{"conversation_id":"chat","file_id":"file","path":"/work/report.txt","sandbox_id":"../host"}`, false},
	} {
		_, err := conversationArgs(Call{Name: input.name, Arguments: json.RawMessage(input.args)})
		if (err == nil) != input.valid {
			t.Fatalf("%s %s: %v", input.name, input.args, err)
		}
	}
}
