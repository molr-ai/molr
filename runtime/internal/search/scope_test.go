package search

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

type observedMemory struct {
	memory.Service
	queries []memory.Query
}

func (m *observedMemory) List(_ context.Context, p core.Principal, q memory.Query) (core.Page[memory.Memory], error) {
	m.queries = append(m.queries, q)
	return core.Page[memory.Memory]{Items: []memory.Memory{{OwnerID: p.OwnerID, Ref: memory.Ref{Scope: q.Scope, AssistantID: q.AssistantID, Path: "budget.md"}, Text: "budget"}}}, nil
}

type searchReads struct {
	persistence.Store
	rows    map[string]int
	maximum int
	gets    map[string]int
}
type searchTx struct {
	persistence.Tx
	reads *searchReads
}

func (s *searchReads) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, searchTx{tx, s}) })
}
func (t searchTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	t.reads.rows[bucket] += len(rows)
	if len(rows) > t.reads.maximum {
		t.reads.maximum = len(rows)
	}
	return rows, err
}
func (t searchTx) Get(ctx context.Context, bucket, key string, out any) error {
	t.reads.gets[bucket]++
	return t.Tx.Get(ctx, bucket, key, out)
}
func (s *searchReads) reset() { s.rows = map[string]int{}; s.gets = map[string]int{}; s.maximum = 0 }

func TestSearchFiltersSourcesBeforeReadingAndMemoryScope(t *testing.T) {
	db := fixtureDB(t)
	ctx := t.Context()
	fixturePut(t, db, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", Name: "Budget", State: assistant.Active})
	fixturePut(t, db, "business.tasks", "owner/task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant", Goal: "Budget"})
	// Excluded sources must not even be decoded; a corrupt unrelated record is
	// intentional here, while the searched source remains authoritative and valid.
	fixturePut(t, db, "im.messages", "owner/unrelated", map[string]any{"ID": []string{"not-a-string"}})
	observed := &searchReads{Store: db}
	observed.reset()
	memories := &observedMemory{}
	service := NewService(observed, Options{Authorizer: fixtureAuthorizer{}, Memories: memories})
	result, err := service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Assistants}})
	testkit.RequireNoError(t, err)
	if len(result.Hits.Items) != 1 || observed.rows["im.messages"] != 0 || observed.rows["business.tasks"] != 0 || len(memories.queries) != 0 {
		t.Fatalf("unrelated data read: %+v %+v", observed, memories.queries)
	}
	observed.reset()
	result, err = service.Search(ctx, owner(), Query{Text: "budget", Sources: []Source{Memories}, AssistantID: "assistant"})
	testkit.RequireNoError(t, err)
	if len(result.Hits.Items) != 1 || len(observed.rows) != 0 || len(memories.queries) != 1 || memories.queries[0].Scope != memory.Assistant {
		t.Fatalf("memory scope: %+v %+v", observed, memories.queries)
	}
}

func TestSearchMessageTraversalBoundsAssistantArchiveAndOwner(t *testing.T) {
	db := fixtureDB(t)
	ctx := t.Context()
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, c := range []struct {
			owner, id, peer core.ID
			archived        bool
			count           int
		}{{"owner", "chosen", "assistant", false, 501}, {"owner", "archive", "assistant", true, 300}, {"owner", "unrelated", "someone-else", false, 700}, {"other", "foreign", "assistant", false, 400}} {
			if err := tx.Put(ctx, "im.conversations", persistence.Key(c.owner, c.id), conversation.Conversation{ID: c.id, OwnerID: c.owner, PeerID: c.peer, Archived: c.archived}); err != nil {
				return err
			}
			for i := 0; i < c.count; i++ {
				id := core.ID(fmt.Sprintf("%s-%04d", c.id, c.count-i)) // IDs deliberately disagree with sequence order.
				text := "other text"
				if i%2 == 0 {
					text = "BÜDGET 预算"
				}
				if i == c.count-1 {
					text = "BÜDGET BÜDGET 预算"
				}
				if err := tx.Put(ctx, "im.messages", persistence.Key(c.owner, id), conversation.Message{ID: id, ConversationID: c.id, Sequence: uint64(i + 1), Content: conversation.Content{Kind: conversation.Text, Text: text}}); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	observed := &searchReads{Store: db}
	observed.reset()
	service := NewService(observed, Options{Authorizer: fixtureAuthorizer{}})
	query := Query{Text: "büdget", Sources: []Source{Messages}, AssistantID: "assistant", Page: core.PageRequest{Limit: 2}}
	first, err := service.Search(ctx, owner(), query)
	testkit.RequireNoError(t, err)
	if first.Counts[Messages] != 251 || len(first.Hits.Items) != 2 || first.Hits.Items[0].Score != 2 || first.Hits.NextCursor == "" {
		t.Fatalf("counts/rank: %+v", first)
	}
	if observed.rows["im.messages"] != 501 || observed.rows["im.conversations"] != 1 || observed.gets["im.conversations"] != 0 || observed.maximum > 200 {
		t.Fatalf("unbounded reads: %+v", observed)
	}
	query.Page.Cursor = first.Hits.NextCursor
	next, err := service.Search(ctx, owner(), query)
	testkit.RequireNoError(t, err)
	if len(next.Hits.Items) != 2 || next.Counts[Messages] != first.Counts[Messages] || reflect.DeepEqual(next.Hits.Items, first.Hits.Items) {
		t.Fatal("pagination lost ordering or counts")
	}
	query.Page.Cursor = ""
	query.IncludeArchived = true
	observed.reset()
	result, err := service.Search(ctx, owner(), query)
	testkit.RequireNoError(t, err)
	if result.Counts[Messages] != 402 || observed.rows["im.messages"] != 801 || observed.rows["im.conversations"] != 2 {
		t.Fatalf("archive scope: %+v %+v", result, observed)
	}
	query.AssistantID = "absent"
	observed.reset()
	result, err = service.Search(ctx, owner(), query)
	testkit.RequireNoError(t, err)
	if len(result.Hits.Items) != 0 || observed.rows["im.messages"] != 0 {
		t.Fatal("empty scope read message history")
	}
}

func TestSearchAssistantScopeFiltersTaskAndAssistantReads(t *testing.T) {
	db := fixtureDB(t)
	for _, id := range []core.ID{"chosen", "unrelated"} {
		fixturePut(t, db, "business.assistants", persistence.Key("owner", id), assistant.Assistant{ID: id, OwnerID: "owner", Name: "Budget", State: assistant.Active})
		fixturePut(t, db, "business.tasks", persistence.Key("owner", id), task.Task{ID: id, OwnerID: "owner", AssistantID: id, Goal: "Budget"})
	}
	observed := &searchReads{Store: db}
	observed.reset()
	service := NewService(observed, Options{Authorizer: fixtureAuthorizer{}})
	result, err := service.Search(t.Context(), owner(), Query{Text: "budget", Sources: []Source{Assistants, Tasks}, AssistantID: "chosen"})
	testkit.RequireNoError(t, err)
	if len(result.Hits.Items) != 2 || observed.rows["business.assistants"] != 1 || observed.rows["business.tasks"] != 1 {
		t.Fatalf("scope read unrelated rows: %+v %+v", result, observed)
	}
	for _, hit := range result.Hits.Items {
		if hit.AssistantID != "chosen" {
			t.Fatal(hit)
		}
	}
}
