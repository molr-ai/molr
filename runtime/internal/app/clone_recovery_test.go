package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/search"
)

func TestCloneReplayAndConcurrentCallsUseRealLifecycle(t *testing.T) {
	b, p := dataBundle(t)
	a := dataAssistant(t, b, p)
	req := assistant.CloneRequest{SourceID: a.ID, Name: "Copy", CopyMemory: true, Mutation: core.Mutation{IdempotencyKey: "copy"}}
	const count = 8
	results := make([]assistant.Assistant, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() { results[i], errs[i] = b.Services.Assistants.Clone(t.Context(), p, req) })
	}
	wg.Wait()
	for i := range count {
		need(t, errs[i])
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatal("replay changed result")
		}
	}
	status, err := b.Services.Agent.Status(t.Context(), p, results[0].ID)
	need(t, err)
	if status.State != agent.Idle {
		t.Fatal(status)
	}
	// The same receipt remains successful after subsequent lifecycle changes.
	_, err = b.Services.Assistants.Dismiss(t.Context(), p, assistant.LifecycleRequest{ID: results[0].ID, Mutation: core.Mutation{IdempotencyKey: "dismiss", ExpectedVersion: results[0].Version}})
	need(t, err)
	replay, err := b.Services.Assistants.Clone(t.Context(), p, req)
	need(t, err)
	if !reflect.DeepEqual(replay, results[0]) {
		t.Fatal("receipt was not preserved")
	}
	status, err = b.Services.Agent.Status(t.Context(), p, results[0].ID)
	need(t, err)
	if status.State != agent.Stopped {
		t.Fatal("replay reinitialized dismissed assistant", status)
	}
	req.Name = "Different"
	_, err = b.Services.Assistants.Clone(t.Context(), p, req)
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("payload conflict lost", err)
	}
}

func TestCloneRecoveryAfterCopyOrCommitFailureAndRestart(t *testing.T) {
	for _, point := range []string{"copy", "commit"} {
		t.Run(point, func(t *testing.T) {
			ctx := t.Context()
			cfg := testConfig(t)
			b, err := Build(ctx, cfg)
			need(t, err)
			defer func() {
				if b != nil {
					b.Close()
				}
			}()
			p, _ := ownerSession(t, b)
			source := dataAssistant(t, b, p)
			_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{ConversationID: source.ConversationID, Mutation: core.Mutation{IdempotencyKey: "source-message"}, Content: conversation.Content{Kind: conversation.Text, Text: "Clone history"}})
			need(t, err)
			m, err := b.Services.Memories.Create(ctx, p, memory.CreateRequest{Ref: memory.Ref{Scope: memory.Assistant, AssistantID: source.ID, Path: ".agents/memory/copy.md"}, Text: "Clone recovery evidence", Mutation: memory.Mutation{IdempotencyKey: "source-memory"}})
			need(t, err)
			files := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(cfg.DataDir, "memory-operations"), Roots: func(ctx context.Context, owner core.ID, q memory.RootQuery) ([]memory.Root, error) {
				return memoryRoots(ctx, b.store, cfg.DataDir, cfg.Sandbox.WorkRoot, owner, q, true)
			}})
			failure := errors.New("injected clone failure")
			var target core.ID
			svc := assistant.NewService(b.store, assistant.Options{Authorizer: b.Services.Grants.(policy.Authorizer), Loop: b.Services.Agent.(*agent.Engine).Lifecycle, CopyMemory: func(ctx context.Context, p core.Principal, from, to core.ID) error {
				target = to
				if point == "copy" {
					return failure
				}
				if err := files.CloneStaged(ctx, p, from, to); err != nil {
					return err
				}
				b.store.(*sqlitestore.Store).FailNextCommit(failure)
				return nil
			}})
			req := assistant.CloneRequest{SourceID: source.ID, Name: "Unfinished clone", CopyMemory: true, CopyMessages: true, Mutation: core.Mutation{IdempotencyKey: "recover-copy"}}
			_, err = svc.Clone(ctx, p, req)
			if !errors.Is(err, failure) {
				t.Fatal("fault not reached", err)
			}
			staged, err := assistant.NewRepository(b.store).Get(ctx, p.OwnerID, target)
			need(t, err)
			if staged.State != assistant.Cloning {
				t.Fatal("partial clone became active", staged)
			}
			assertCloneHidden(t, b, p, staged)
			// Reopen the complete composition: startup recovery has no original session
			// or caller request and resumes before scheduling begins.
			need(t, b.Close())
			b = nil
			b, err = Build(ctx, cfg)
			need(t, err)
			need(t, b.Application.(*Runtime).hooks.RecoverCalls(ctx))
			recovered, err := assistant.NewRepository(b.store).Get(ctx, p.OwnerID, target)
			need(t, err)
			if recovered.State != assistant.Active {
				t.Fatal("startup did not resume clone", recovered)
			}
			replay, err := b.Services.Assistants.Clone(ctx, p, req)
			need(t, err)
			if replay.ID != target {
				t.Fatal("recovery allocated a second assistant")
			}
			messages, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: recovered.ConversationID})
			need(t, err)
			if len(messages.Items) != 1 || messages.Items[0].Content.Text != "Clone history" {
				t.Fatal("history not copied exactly once", messages)
			}
			page, err := b.Services.Memories.List(ctx, p, memory.Query{AssistantID: target})
			need(t, err)
			if len(page.Items) != 1 || page.Items[0].Text != m.Text {
				t.Fatal("memory not recovered", page)
			}
			pending, err := persistence.Scan[any](ctx, b.store, "business.assistants.clones")
			need(t, err)
			if len(pending) != 0 {
				t.Fatal("completed operation retained as pending")
			}
			// Successful replay must not recreate a subsequently removed target file.
			name := filepath.Join(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, p.OwnerID, target), ".agents/memory/copy.md")
			need(t, os.Remove(name))
			_, err = b.Services.Assistants.Clone(ctx, p, req)
			need(t, err)
			if _, err = os.Stat(name); !os.IsNotExist(err) {
				t.Fatal("replay recreated deleted memory", err)
			}
		})
	}
}

func assertCloneHidden(t *testing.T, b *Bundle, p core.Principal, a assistant.Assistant) {
	t.Helper()
	ctx := t.Context()
	page, err := b.Services.Assistants.List(ctx, p, assistant.Query{})
	need(t, err)
	for _, item := range page.Items {
		if item.ID == a.ID {
			t.Fatal("staged assistant listed")
		}
	}
	_, err = b.Services.Assistants.Get(ctx, p, a.ID)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("staged assistant readable", err)
	}
	chats, err := b.Services.Conversations.List(ctx, p, core.PageRequest{})
	need(t, err)
	for _, item := range chats.Items {
		if item.ID == a.ConversationID {
			t.Fatal("staged conversation listed")
		}
	}
	_, err = b.Services.Conversations.Get(ctx, p, a.ConversationID)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("staged conversation readable", err)
	}
	_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{ConversationID: a.ConversationID, Mutation: core.Mutation{IdempotencyKey: "cannot-send"}, Content: conversation.Content{Kind: conversation.Text, Text: "Do not execute"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("staged assistant accepted input", err)
	}
	hits, err := b.Services.Search.Search(ctx, p, search.Query{Text: "clone", IncludeArchived: true})
	need(t, err)
	for _, h := range hits.Hits.Items {
		if h.AssistantID == a.ID {
			t.Fatal("search exposed partial clone", h)
		}
	}
	roots, err := runtimeMemoryRoots(ctx, b.store, "data", "work", p.OwnerID, memory.RootQuery{AssistantID: a.ID})
	need(t, err)
	if len(roots) != 0 {
		t.Fatal("staged memory root publicly available")
	}
	var status agent.Status
	err = b.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "agent.status", persistence.Key(p.OwnerID, a.ID), &status)
	})
	if !persistence.Missing(err) {
		t.Fatal("partial clone has Loop", status, err)
	}
}
