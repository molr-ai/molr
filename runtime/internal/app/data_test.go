package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/tests/testkit"
)

func dataBundle(t *testing.T) (*Bundle, core.Principal) {
	t.Helper()
	b, err := Build(testkit.Context(t), testConfig(t))
	need(t, err)
	t.Cleanup(func() { _ = b.Close() })
	owner, _ := ownerSession(t, b)
	return b, owner
}
func dataAssistant(t *testing.T, b *Bundle, p core.Principal) assistant.Assistant {
	t.Helper()
	id := configureTestModel(t, b, p)
	a, err := b.Services.Assistants.Hire(testkit.Context(t), p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "data-assistant"}, Name: "Data", ModelID: id})
	need(t, err)
	return a
}
func TestCleanupMessageKeepsSequenceMonotonicAndReadStateUsable(t *testing.T) {
	b, p := dataBundle(t)
	ctx := testkit.Context(t)
	a := dataAssistant(t, b, p)
	first, err := b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "first"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "Question"}})
	need(t, err)
	repo := conversation.NewRepository(b.store)
	last, err := repo.Append(ctx, p.OwnerID, conversation.Message{ID: "last-message", ConversationID: a.ConversationID, Sender: conversation.Assistant, SenderID: a.ID, Content: conversation.Content{Kind: conversation.Text, Text: "Old reply"}, CreatedAt: time.Now()})
	need(t, err)
	chat, err := repo.Get(ctx, p.OwnerID, a.ConversationID)
	need(t, err)
	need(t, repo.MarkRead(ctx, p.OwnerID, conversation.ReadRequest{ConversationID: a.ConversationID, ThroughSequence: last.Sequence}))
	need(t, cleanupObject(ctx, b.store, access{b.store}, p, core.Object{Kind: "message", ID: last.ID}, true))
	chat, err = repo.Get(ctx, p.OwnerID, a.ConversationID)
	need(t, err)
	if chat.LastMessageID != first.ID || chat.UnreadCount != 0 {
		t.Fatalf("last/read state not repaired: %+v", chat)
	}
	next, err := repo.Append(ctx, p.OwnerID, conversation.Message{ID: "new-message", ConversationID: a.ConversationID, Sender: conversation.Assistant, SenderID: a.ID, Content: conversation.Content{Kind: conversation.Text, Text: "New reply"}, CreatedAt: time.Now()})
	need(t, err)
	if next.Sequence <= last.Sequence {
		t.Fatalf("deleted sequence reused: removed=%d next=%d", last.Sequence, next.Sequence)
	}
	chat, err = repo.Get(ctx, p.OwnerID, a.ConversationID)
	need(t, err)
	need(t, repo.MarkRead(ctx, p.OwnerID, conversation.ReadRequest{ConversationID: a.ConversationID, ThroughSequence: next.Sequence}))
	chat, err = repo.Get(ctx, p.OwnerID, a.ConversationID)
	need(t, err)
	if chat.UnreadCount != 0 {
		t.Fatal("new message can no longer be marked read")
	}
}
func TestCleanupConversationKeepsRehireShellAndOtherOwnerMessages(t *testing.T) {
	b, p := dataBundle(t)
	ctx := testkit.Context(t)
	a := dataAssistant(t, b, p)
	repo := conversation.NewRepository(b.store)
	message, err := b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "archive-input"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "Delete my history"}})
	need(t, err)
	foreignOwner := core.ID("other-owner")
	need(t, repo.Insert(ctx, conversation.Conversation{ID: a.ConversationID, OwnerID: foreignOwner, PeerID: "foreign-assistant", Version: 1}))
	_, err = repo.Append(ctx, foreignOwner, conversation.Message{ID: "foreign-message", ConversationID: a.ConversationID, Sender: conversation.User, SenderID: foreignOwner, Content: conversation.Content{Kind: conversation.Text, Text: "Retain other owner's content"}, CreatedAt: time.Now()})
	need(t, err)
	dismissed, err := b.Services.Assistants.Dismiss(ctx, p, assistant.LifecycleRequest{Mutation: core.Mutation{IdempotencyKey: "dismiss-data", ExpectedVersion: a.Version}, ID: a.ID})
	need(t, err)
	need(t, cleanupObject(ctx, b.store, access{b.store}, p, core.Object{Kind: "conversation", ID: a.ConversationID}, true))
	own, err := repo.Messages(ctx, p.OwnerID, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if len(own.Items) != 0 {
		t.Fatal("cleanup retained own messages")
	}
	foreign, err := repo.Messages(ctx, foreignOwner, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if len(foreign.Items) != 1 {
		t.Fatal("conversation cleanup crossed owner boundary")
	}
	pending, err := conversation.NewOutbox(b.store).Pending(ctx, 200)
	need(t, err)
	for _, item := range pending {
		if item.OwnerID == p.OwnerID && item.MessageID == message.ID {
			t.Fatal("deleted message remains in pending outbox")
		}
	}
	rehired, err := b.Services.Assistants.Rehire(ctx, p, assistant.RehireRequest{LifecycleRequest: assistant.LifecycleRequest{Mutation: core.Mutation{IdempotencyKey: "rehire-data", ExpectedVersion: dismissed.Version}, ID: a.ID}})
	need(t, err)
	if rehired.ConversationID != a.ConversationID {
		t.Fatal("cleanup replaced conversation identity")
	}
	next, err := b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "after-cleanup"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "Hello again"}})
	need(t, err)
	if next.Sequence <= message.Sequence {
		t.Fatal("whole-conversation cleanup reset sequence")
	}
}
func TestCleanupTaskRemovesRevisionsAndResumeEmitsTaskEvent(t *testing.T) {
	b, p := dataBundle(t)
	ctx := testkit.Context(t)
	a := dataAssistant(t, b, p)
	work, err := b.Services.Tasks.Create(ctx, p, task.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "data-task"}, AssistantID: a.ID, Goal: "Audit report"})
	need(t, err)
	paused, err := b.Services.Tasks.Pause(ctx, p, task.ControlRequest{Mutation: core.Mutation{IdempotencyKey: "pause-data-task", ExpectedVersion: work.Version}, ID: work.ID})
	need(t, err)
	resumed, err := b.Services.Tasks.Resume(ctx, p, task.ControlRequest{Mutation: core.Mutation{IdempotencyKey: "resume-data-task", ExpectedVersion: paused.Version}, ID: work.ID})
	need(t, err)
	events, err := persistence.Scan[event.AssistantEvent](ctx, b.store, "agent.events")
	need(t, err)
	found := false
	for _, e := range events {
		if e.ID == stableEventID(work.ID, resumed.Version) && e.TaskID == work.ID && e.Kind == event.ResumeRequested {
			found = true
		}
	}
	if !found {
		t.Fatal("resume did not persist a task-linked wake event")
	}
	revised, err := b.Services.Tasks.Revise(ctx, p, task.ReviseRequest{Mutation: core.Mutation{IdempotencyKey: "revise-data-task", ExpectedVersion: resumed.Version}, ID: work.ID, Instructions: "Add evidence"})
	need(t, err)
	_, err = b.Services.Tasks.Cancel(ctx, p, task.ControlRequest{Mutation: core.Mutation{IdempotencyKey: "cancel-data-task", ExpectedVersion: revised.Version}, ID: work.ID})
	need(t, err)
	need(t, cleanupObject(ctx, b.store, access{b.store}, p, core.Object{Kind: "task", ID: work.ID}, true))
	_, err = task.NewRepository(b.store).Get(ctx, p.OwnerID, work.ID)
	testkit.RequireCode(t, err, core.NotFound)
	need(t, b.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, "business.revisions")
		if err != nil {
			return err
		}
		for _, row := range rows {
			if strings.HasPrefix(row.Key, persistence.Key(p.OwnerID, work.ID)+"/") {
				t.Fatal("cleaned task content remains in revisions/export")
			}
		}
		return nil
	}))
}
func TestExportIncludesCompleteArtifactsAndRejectsTruncation(t *testing.T) {
	b, p := dataBundle(t)
	ctx := testkit.Context(t)
	content := []byte("complete artifact contents")
	uploaded, err := b.Services.Files.Upload(ctx, p, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "export-source"}, Name: "report.txt", Size: int64(len(content)), Body: bytes.NewReader(content)})
	need(t, err)
	descriptor, err := json.Marshal(uploaded)
	need(t, err)
	bundle := settings.ExportBundle{ID: "export-complete", IncludeArtifacts: true, Records: map[string][]json.RawMessage{files.Bucket: {descriptor}}}
	id, err := saveExport(ctx, b.Services.Files, p, bundle)
	need(t, err)
	download, err := b.Services.Files.Open(ctx, p, id)
	need(t, err)
	data, err := io.ReadAll(download.Body)
	need(t, err)
	need(t, download.Body.Close())
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	need(t, err)
	found := false
	for _, entry := range archive.File {
		if entry.Name == "files/"+string(uploaded.ID) {
			reader, err := entry.Open()
			need(t, err)
			actual, err := io.ReadAll(reader)
			need(t, err)
			need(t, reader.Close())
			if !bytes.Equal(actual, content) {
				t.Fatal("export truncated artifact")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("export omitted requested artifact")
	}
	wrong := uploaded
	wrong.Size--
	encoded, err := json.Marshal(wrong)
	need(t, err)
	bundle.ID = "export-short-descriptor"
	bundle.Records[files.Bucket] = []json.RawMessage{encoded}
	_, err = saveExport(ctx, b.Services.Files, p, bundle)
	testkit.RequireCode(t, err, core.FailedPrecondition)
	wrong.Size = 20<<20 + 1
	encoded, err = json.Marshal(wrong)
	need(t, err)
	bundle.ID = "export-too-large"
	bundle.Records[files.Bucket] = []json.RawMessage{encoded}
	_, err = saveExport(ctx, b.Services.Files, p, bundle)
	testkit.RequireCode(t, err, core.InvalidArgument)
}
func TestCleanupFilePublishesGCBeforePhysicalDeletion(t *testing.T) {
	b, p := dataBundle(t)
	ctx := testkit.Context(t)
	uploaded, err := b.Services.Files.Upload(ctx, p, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "gc-source"}, Name: "discard.txt", Size: 7, Body: strings.NewReader("discard")})
	need(t, err)
	need(t, cleanupObject(ctx, b.store, access{b.store}, p, core.Object{Kind: "file", ID: uploaded.ID}, true))
	_, err = b.Services.Files.Get(ctx, p, uploaded.ID)
	testkit.RequireCode(t, err, core.NotFound)
	storedID, err := persistence.Load[core.ID](ctx, b.store, "files.gc", persistence.Key(p.OwnerID, uploaded.ID))
	need(t, err)
	if storedID != uploaded.ID {
		t.Fatal("GC job lost file identity")
	}
	body, err := b.objects.Open(ctx, uploaded.ID)
	need(t, err)
	need(t, body.Close())
	need(t, collectFiles(ctx, b.store, b.objects))
	_, err = b.objects.Open(ctx, uploaded.ID)
	if err == nil {
		t.Fatal("GC retained object bytes")
	}
	_, err = persistence.Load[core.ID](ctx, b.store, "files.gc", persistence.Key(p.OwnerID, uploaded.ID))
	testkit.RequireCode(t, err, core.NotFound)
}
