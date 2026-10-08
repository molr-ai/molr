package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{}

func (fixtureAuthorizer) Require(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.Validate()
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		ctx := testkit.Context(t)
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scheduler.db"))
		testkit.RequireNoError(t, err)
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		now := time.Now().UTC()
		repo := conversation.NewRepository(db)
		testkit.RequireNoError(t, repo.Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: owner.OwnerID, PeerID: "assistant", Version: 1}))
		var mu sync.Mutex
		active, maxActive := map[core.ID]int{}, map[core.ID]int{}
		completed := 0
		started := make(chan struct{}, 10)
		completion := make(chan struct{}, 10)
		release := make(chan struct{})
		var releaseOnce sync.Once
		service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Now: func() time.Time { return now }, TickInterval: time.Millisecond, Handle: func(ctx context.Context, claim Claim) error {
			mu.Lock()
			active[claim.Job.Event.AssistantID]++
			if active[claim.Job.Event.AssistantID] > maxActive[claim.Job.Event.AssistantID] {
				maxActive[claim.Job.Event.AssistantID] = active[claim.Job.Event.AssistantID]
			}
			mu.Unlock()
			defer func() { mu.Lock(); active[claim.Job.Event.AssistantID]--; mu.Unlock() }()
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			mu.Lock()
			completed++
			mu.Unlock()
			completion <- struct{}{}
			return nil
		}})
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = service.Shutdown(cleanup)
			_ = db.Close()
		})
		return contractFixture{Service: service, Queue: NewQueue(db), Consumer: NewMessageConsumer(db, Options{Conversations: repo}), Outbox: conversation.NewOutbox(db), Owner: owner, Other: other, AssistantID: "assistant", ConversationID: "chat", Now: now,
			SeedJob: func(job Job) {
				testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
					if err := event.SaveJob(ctx, tx, job); err != nil {
						return err
					}
					if job.State == Running {
						return operation.NewRepository(tx.Store()).Insert(ctx, operation.Operation{ID: "unknown-effect", OwnerID: job.Event.OwnerID, AssistantID: job.Event.AssistantID, ExecutionID: job.ExecutionID, ResourceID: "resource", Action: "write", State: operation.Unknown, IdempotencyKey: "unknown-effect", InputDigest: "digest", Version: 1, CreatedAt: now, UpdatedAt: now})
					}
					return nil
				}))
			},
			AssistantEvents: func() []event.AssistantEvent {
				events, err := persistence.Scan[event.AssistantEvent](ctx, db, "agent.events")
				testkit.RequireNoError(t, err)
				return events
			}, FailNextCommit: func() { db.FailNextCommit(errors.New("injected SQLite commit failure")) },
			AwaitStarted: func(ctx context.Context) error {
				select {
				case <-started:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}, ReleaseTurns: func() { releaseOnce.Do(func() { close(release) }) },
			AwaitCompleted: func(ctx context.Context, n int) error {
				for {
					mu.Lock()
					done := completed >= n
					mu.Unlock()
					if done {
						return nil
					}
					select {
					case <-completion:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}, MaxActiveForAssistant: func(id core.ID) int { mu.Lock(); defer mu.Unlock(); return maxActive[id] }, ExternalEffects: func() int { return 0 },
		}
	}
}
