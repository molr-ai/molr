package plan

import (
	"context"
	"fmt"
	"testing"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestResolveUsesBoundedReadsAndVisibleTodoFirst(t *testing.T) {
	s, db := fixture(t)
	if err := seedListSources(t.Context(), db, 400); err != nil {
		t.Fatal(err)
	}
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.todos", "owner/shared", todo.Todo{ID: "shared", OwnerID: "owner", AssistantID: "other", Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "business.schedules", "owner/shared", schedule.Schedule{ID: "shared", OwnerID: "owner", AssistantID: "assistant", Version: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	counted := &listReadCounter{Store: db}
	s.store = counted
	var service Service = s
	p := core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "assistant", ExecutionID: "execution"}
	for _, tc := range []struct {
		principal core.Principal
		id        core.ID
		kind      string
		reads     int
	}{
		{owner(), "shared", "todo", 1}, {p, "shared", "schedule", 2}, {owner(), "plan-0001", "schedule", 2},
	} {
		counted.gets, counted.queries = 0, 0
		ref, err := service.Resolve(t.Context(), tc.principal, tc.id)
		if err != nil || ref.Kind != tc.kind || ref.ID != tc.id {
			t.Fatal(ref, err)
		}
		if counted.gets != tc.reads || counted.queries != 0 {
			t.Fatalf("unexpected reads: gets=%d queries=%d", counted.gets, counted.queries)
		}
	}
	p.SubjectID = "invisible"
	for _, tc := range []struct {
		p  core.Principal
		id core.ID
	}{{p, "shared"}, {owner(), "missing"}, {core.Principal{Kind: core.ActorOwner, OwnerID: "foreign", SubjectID: "foreign", SessionID: "session"}, "shared"}} {
		if _, err := service.Resolve(t.Context(), tc.p, tc.id); core.CodeOf(err) != core.NotFound {
			t.Fatal("invisible source disclosed", err)
		}
	}
	if _, err := service.Resolve(t.Context(), owner(), "../invalid"); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	if _, err := service.Resolve(t.Context(), core.Principal{}, "shared"); err == nil {
		t.Fatal("missing principal accepted")
	}
}

func TestDetailSuccessorsUseOwnerScopedExistenceQuery(t *testing.T) {
	s, db := fixture(t)
	add(t, s, "target", "")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, owner := range []core.ID{"owner", "foreign"} {
			for i := 0; i < 200; i++ {
				ref := core.Object{Kind: "todo", ID: core.ID(fmt.Sprintf("link-%d", i))}
				if err := tx.Put(ctx, bucket, key(owner, ref), Links{OwnerID: owner, Ref: ref, Dependencies: []core.Object{{Kind: "schedule", ID: "target"}}}); err != nil {
					return err
				}
			}
		}
		return tx.Put(ctx, bucket, "foreign/successor", Links{OwnerID: "foreign", Dependencies: []core.Object{{Kind: "todo", ID: "target"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	counted := &listReadCounter{Store: db}
	s.store = counted
	ref := core.Object{Kind: "todo", ID: "target"}
	detail, err := s.Detail(t.Context(), owner(), ref, nil)
	if err != nil || detail.HasSuccessors {
		t.Fatal(detail.HasSuccessors, err)
	}
	if counted.rows[bucket] != 0 {
		t.Fatal("unrelated links loaded", counted.rows)
	}
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bucket, "owner/successor", Links{OwnerID: "owner", Dependencies: []core.Object{ref}})
	})
	if err != nil {
		t.Fatal(err)
	}
	counted.rows = nil
	detail, err = s.Detail(t.Context(), owner(), ref, nil)
	if err != nil || !detail.HasSuccessors {
		t.Fatal(detail.HasSuccessors, err)
	}
	if counted.rows[bucket] != 1 {
		t.Fatal("unbounded successor read", counted.rows)
	}
}
