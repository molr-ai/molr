package assistant

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

// fixtureAuthorizer is the controlled authentication/policy dependency. The
// business Service and Repository below are real implementations over SQLite.
type fixtureAuthorizer struct{}

func (fixtureAuthorizer) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.OwnerID != "owner" && p.OwnerID != "other" {
		return core.NewError(core.Unauthenticated, "unknown test identity")
	}
	if p.Kind == core.ActorAssistant && q.Target.Kind == "shared_memory" && q.Action == policy.Write {
		return core.NewError(core.Forbidden, "shared write requires grant")
	}
	return nil
}
func fixtureDB(t *testing.T) *sqlitestore.Store {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
func fixturePut(t *testing.T, db persistence.Store, bucket, key string, value any) {
	t.Helper()
	if err := db.Write(testkit.Context(t), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }); err != nil {
		t.Fatal(err)
	}
}
func fixtureCount(t *testing.T, db persistence.Store, bucket string) int {
	t.Helper()
	n := 0
	if err := db.Read(testkit.Context(t), func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, bucket)
		n = len(rows)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
func fixtureCheckAssistant(db persistence.Store) func(context.Context, core.Principal, core.ID) error {
	return func(ctx context.Context, p core.Principal, id core.ID) error {
		var row struct {
			ID, OwnerID core.ID
			State       string
		}
		err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Get(ctx, "business.assistants", persistence.Key(p.OwnerID, id), &row)
		})
		if err != nil {
			return err
		}
		if row.State != "active" {
			return core.NewError(core.FailedPrecondition, "assistant dismissed")
		}
		return nil
	}
}
func fixtureFailure(db *sqlitestore.Store) func() {
	return func() { db.FailNextCommit(errors.New("injected commit failure")) }
}

type fixtureLoop struct {
	Stopped   bool
	Transient bool
}

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		db := fixtureDB(t)
		ctx := testkit.Context(t)
		repo := NewRepository(db)
		im := conversation.NewRepository(db)
		grants := policy.NewRepository(db)
		root := t.TempDir()
		memoryFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
			rows, err := persistence.Scan[Assistant](ctx, db, "business.assistants")
			out := []memory.Root{}
			for _, a := range rows {
				if a.OwnerID == owner {
					out = append(out, memory.Root{Scope: memory.Assistant, AssistantID: a.ID, Directory: filepath.Join(root, string(owner), string(a.ID))})
				}
			}
			return out, err
		}})
		options := Options{CopyMemory: memoryFiles.Clone, Authorizer: fixtureAuthorizer{}, Conversations: im, Grants: grants, CheckModel: func(ctx context.Context, p core.Principal, id core.ID) error {
			if id != "model" {
				return invalid("model")
			}
			return nil
		}, GrantModel: func(context.Context, core.Principal, Assistant) error {
			// Model resolution/authorization is a controlled composition dependency;
			// app integration tests cover real provider grants and rollback.
			return nil
		}, Loop: func(ctx context.Context, p core.Principal, id core.ID, action string) error {
			return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "fixture.loops", persistence.Key(p.OwnerID, id), fixtureLoop{Stopped: action == "stop"})
			})
		}}
		activeGrants := func(id core.ID) []policy.Grant {
			page, err := grants.Grants(ctx, "owner", policy.Query{AssistantID: id, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				t.Fatal(err)
			}
			var result []policy.Grant
			for _, g := range page.Items {
				if g.RevokedAt == nil {
					result = append(result, g)
				}
			}
			return result
		}
		dependencies := func(id core.ID) dependentState {
			result := dependentState{}
			a, err := repo.Get(ctx, "owner", id)
			if err != nil {
				t.Fatal(err)
			}
			messages, err := im.Messages(ctx, "owner", conversation.MessageQuery{ConversationID: a.ConversationID, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range messages.Items {
				result.Messages = append(result.Messages, m.Content.Text)
			}
			memories, err := memoryFiles.List(ctx, "owner", memory.Query{AssistantID: id, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range memories.Items {
				result.Memories = append(result.Memories, m.Text)
			}
			for _, g := range activeGrants(id) {
				result.ActiveGrantIDs = append(result.ActiveGrantIDs, g.ID)
			}
			schedules, err := schedule.NewRepository(db).List(ctx, "owner", schedule.Query{AssistantID: id, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range schedules.Items {
				if p.Enabled {
					result.ActiveScheduleIDs = append(result.ActiveScheduleIDs, p.ID)
				}
			}
			tasks, err := task.NewRepository(db).List(ctx, "owner", task.Query{AssistantID: id, States: []task.State{task.Running}, Page: core.PageRequest{Limit: 200}})
			if err != nil {
				t.Fatal(err)
			}
			for _, work := range tasks.Items {
				result.RunningTaskIDs = append(result.RunningTaskIDs, work.ID)
			}
			loop, err := persistence.Load[fixtureLoop](ctx, db, "fixture.loops", persistence.Key("owner", id))
			if err != nil {
				t.Fatal(err)
			}
			result.HasTransientContext = loop.Transient
			return result
		}
		return contractFixture{Service: NewService(db, options), Repository: repo, FailCommitOnce: fixtureFailure(db), Observe: func() (int, int, int, int) {
			return fixtureCount(t, db, bucket), fixtureCount(t, db, "im.conversations"), fixtureCount(t, db, "fixture.loops"), fixtureCount(t, db, "agent.events")
		}, LifecycleState: func(id core.ID) (bool, bool, int, int) {
			a, err := repo.Get(ctx, "owner", id)
			if err != nil {
				t.Fatal(err)
			}
			c, err := im.Get(ctx, "owner", a.ConversationID)
			if err != nil {
				t.Fatal(err)
			}
			state := dependencies(id)
			return c.Archived, !c.Archived && a.State == Active, len(state.ActiveScheduleIDs), len(state.ActiveGrantIDs)
		}, Dependencies: dependencies, GrantDetails: activeGrants, SeedDependencies: func(id core.ID, state dependentState) {
			a, err := repo.Get(ctx, "owner", id)
			if err != nil {
				t.Fatal(err)
			}
			for i, text := range state.Messages {
				sender := conversation.User
				senderID := core.ID("owner")
				if i%2 == 1 {
					sender = conversation.Assistant
					senderID = id
				}
				_, err := im.Append(ctx, "owner", conversation.Message{ID: core.ID(fmt.Sprintf("message-%d", i)), ConversationID: a.ConversationID, Sender: sender, SenderID: senderID, Content: conversation.Content{Kind: conversation.Text, Text: text}, CreatedAt: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
			}
			for i, text := range state.Memories {
				_, err := memoryFiles.Put(ctx, memory.Memory{OwnerID: "owner", Text: text, Ref: memory.Ref{Path: fmt.Sprintf(".agents/memory/memory-%d.md", i), AssistantID: id, Scope: memory.Assistant}}, "", nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, grantID := range state.ActiveGrantIDs {
				err := grants.InsertGrant(ctx, policy.Grant{ID: grantID, OwnerID: "owner", AssistantID: id, ResourceID: core.ID("resource-" + string(grantID)), Actions: []policy.Action{policy.Read}, Scope: policy.Scope{PathPrefixes: []string{"/work"}, ReadOnly: true}, Version: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, planID := range state.ActiveScheduleIDs {
				start := time.Now().UTC().Add(time.Hour)
				err := schedule.NewRepository(db).Insert(ctx, schedule.Schedule{ID: planID, OwnerID: "owner", AssistantID: id, Kind: schedule.AssistantSchedule, Title: "Plan", Trigger: schedule.Trigger{StartAt: start, Timezone: "UTC", MissedPolicy: schedule.Skip}, Enabled: true, NextAt: &start, Version: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, taskID := range state.RunningTaskIDs {
				err := task.NewRepository(db).Insert(ctx, task.Task{ID: taskID, OwnerID: "owner", AssistantID: id, Goal: "Running work", State: task.Running, Revision: 1, Version: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			fixturePut(t, db, "fixture.loops", persistence.Key("owner", id), fixtureLoop{Transient: state.HasTransientContext})
		}}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.List(testkit.Context(t), owner(), Query{})
	wantCode(t, err, core.Unavailable)
}
