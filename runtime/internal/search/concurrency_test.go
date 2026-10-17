package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

// The pause represents a slow file system without relying on disk timing.
// Files.List and the memory service are real, as are the competing IM writes.
func TestSlowMemorySearchAllowsOtherOwnerAppendAndRechecksAccess(t *testing.T) {
	for _, change := range []string{"revoke", "delete", "expire"} {
		t.Run(change, func(t *testing.T) {
			db := fixtureDB(t)
			ctx := t.Context()
			fixturePut(t, db, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", Name: "budget", State: assistant.Active})
			im := conversation.NewRepository(db)
			testkit.RequireNoError(t, im.Insert(ctx, conversation.Conversation{ID: "other-chat", OwnerID: "other", PeerID: "other-assistant", Version: 1}))
			root := t.TempDir()
			testkit.RequireNoError(t, os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("budget"), 0600))
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var once sync.Once
			files := memory.NewFiles(memory.FileOptions{JournalRoot: t.TempDir(), Roots: func(ctx context.Context, owner core.ID, q memory.RootQuery) ([]memory.Root, error) {
				once.Do(func() { close(entered) })
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return []memory.Root{{Scope: memory.Assistant, AssistantID: "assistant", Directory: root}}, nil
			}})
			authorizer := candidatePolicy(func(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
				if err := (searchFixtureAuthorizer{store: db}).Require(ctx, p, q); err != nil {
					return err
				}
				var expired bool
				err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
					if err := tx.Get(ctx, "fixture.expired", "owner/session", &expired); err != nil && !persistence.Missing(err) {
						return err
					}
					if q.Target.Kind == "assistant" && q.Target.ID != "" {
						var row assistant.Assistant
						return tx.Get(ctx, "business.assistants", persistence.Key(p.OwnerID, q.Target.ID), &row)
					}
					return nil
				})
				if expired {
					return core.NewError(core.Unauthenticated, "session expired")
				}
				return err
			})
			service := NewService(db, Options{Authorizer: authorizer, Memories: memory.NewService(db, memory.Options{Files: files, Authorizer: authorizer})})
			type outcome struct {
				result Results
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Assistants, Memories}, AssistantID: "assistant"})
				done <- outcome{result, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("search did not reach memory files")
			}
			writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			_, err := im.Append(writeCtx, "other", conversation.Message{ID: "other-message", ConversationID: "other-chat", CreatedAt: time.Now(), Sender: conversation.User, SenderID: "other", Content: conversation.Content{Kind: conversation.Text, Text: "independent message"}})
			testkit.RequireNoError(t, err)
			testkit.RequireNoError(t, db.Write(writeCtx, func(ctx context.Context, tx persistence.Tx) error {
				switch change {
				case "delete":
					return tx.Delete(ctx, "business.assistants", "owner/assistant")
				case "expire":
					return tx.Put(ctx, "fixture.expired", "owner/session", true)
				default:
					return tx.Put(ctx, "fixture.denied", "owner/assistant", true)
				}
			}))
			unblock()
			select {
			case got := <-done:
				if change == "expire" {
					testkit.RequireCode(t, got.err, core.Unauthenticated)
					return
				}
				testkit.RequireNoError(t, got.err)
				if got.result.Counts[Assistants] != 0 || got.result.Counts[Memories] != 1 || len(got.result.Hits.Items) != 1 {
					t.Fatalf("stale access entered counts/results: %+v", got.result)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("search did not finish")
			}
		})
	}
}

type pausedSearchStore struct {
	persistence.Store
	entered, release chan struct{}
	once             sync.Once
}

type batchSearchTx struct {
	persistence.Tx
	messages *bool
}

func (t batchSearchTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == "im.messages" {
		*t.messages = true
	}
	return t.Tx.Query(ctx, bucket, q)
}

func (s *pausedSearchStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	messages := false
	err := s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, batchSearchTx{tx, &messages}) })
	if err == nil && messages {
		s.once.Do(func() {
			close(s.entered)
			select {
			case <-s.release:
			case <-ctx.Done():
			}
		})
	}
	return err
}

func TestSearchMessageBatchesReleaseTransactionAndBoundNewMessages(t *testing.T) {
	db := fixtureDB(t)
	ctx := t.Context()
	im := conversation.NewRepository(db)
	testkit.RequireNoError(t, im.Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	for i := 0; i < 201; i++ {
		_, err := im.Append(ctx, "owner", conversation.Message{ID: core.ID(fmt.Sprintf("message-%03d", i)), ConversationID: "chat", CreatedAt: time.Now(), Sender: conversation.User, SenderID: "owner", Content: conversation.Content{Kind: conversation.Text, Text: "budget"}})
		testkit.RequireNoError(t, err)
	}
	paused := &pausedSearchStore{Store: db, entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(paused.release) }) }
	defer unblock()
	service := NewService(paused, Options{Authorizer: fixtureAuthorizer{}})
	done := make(chan Results, 1)
	errors := make(chan error, 1)
	go func() {
		result, err := service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Messages}})
		done <- result
		errors <- err
	}()
	select {
	case <-paused.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no first batch")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := im.Append(writeCtx, "owner", conversation.Message{ID: "new-message", ConversationID: "chat", CreatedAt: time.Now(), Sender: conversation.User, SenderID: "owner", Content: conversation.Content{Kind: conversation.Text, Text: "budget"}})
	testkit.RequireNoError(t, err)
	unblock()
	select {
	case result := <-done:
		testkit.RequireNoError(t, <-errors)
		if result.Counts[Messages] != 201 {
			t.Fatalf("concurrent append extended search: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("search did not finish")
	}
}
