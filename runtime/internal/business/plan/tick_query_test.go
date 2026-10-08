package plan

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
	"testing"
	"time"
)

func TestHandoffTickSkipsUnrelatedHistory(t *testing.T) {
	s, db := fixture(t)
	ctx := t.Context()
	prep := add(t, s, "prep", "")
	next := add(t, s, "next", "assistant")
	if _, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("cfg", 0), Ref: next, Dependencies: []core.Object{prep}, Handoff: "automatic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("done", 0), Ref: prep, Action: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			ref := core.Object{Kind: "todo", ID: core.ID(fmt.Sprint("old-", i))}
			v := Links{OwnerID: "other", Ref: ref, Handoff: "notify", Dependencies: []core.Object{prep}, Dispatched: true}
			if err := saveLinks(ctx, tx, v); err != nil {
				return err
			}
			task := task.Task{ID: ref.ID, OwnerID: "other", Plan: next, State: task.Completed}
			if err := tx.Put(ctx, "business.tasks", persistence.Key(task.OwnerID, task.ID), task); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	observed := &testkit.ObservedStore{Store: db}
	s.store = observed
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if observed.Scans != 0 || observed.Rows[bucket] != 1 || observed.Rows["business.tasks"] != 0 {
		t.Fatal("unrelated history read", observed)
	}
	observed.Rows = nil
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if observed.Rows[bucket] != 0 || observed.Rows["business.tasks"] != 0 {
		t.Fatal("dispatched plan rechecked", observed)
	}
}

func TestFilteredHandoffsStillResolveNotices(t *testing.T) {
	for _, action := range []string{"pause", "remove-dependencies", "automatic", "delete", "start"} {
		t.Run(action, func(t *testing.T) {
			s, db := fixture(t)
			ctx := t.Context()
			prep := add(t, s, "prep", "")
			next := add(t, s, "next", "assistant")
			links, err := s.Configure(ctx, owner(), ConfigureRequest{Mutation: mutation("cfg", 0), Ref: next, Dependencies: []core.Object{prep}, Handoff: "notify"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("done", 0), Ref: prep, Action: "complete"}); err != nil {
				t.Fatal(err)
			}
			if err = s.Tick(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
			notices := notification.New(db, allowOwner{})
			page, err := notices.List(ctx, owner(), notification.Query{Kind: "plan.ready"})
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
			n := page.Items[0]
			if err = notices.Update(ctx, owner(), []core.ID{n.ID}, "dismiss"); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "delete":
				err = todo.NewRepository(db).Delete(ctx, "owner", next.ID, 1)
			case "start":
				_, err = s.Execute(ctx, owner(), ExecuteRequest{Mutation: mutation("start", links.Version), Ref: next, Action: "start"})
			default:
				req := ConfigureRequest{Mutation: mutation("change", links.Version), Ref: next, Dependencies: []core.Object{prep}, Handoff: "notify"}
				if action == "pause" {
					req.Paused = true
				}
				if action == "remove-dependencies" {
					req.Dependencies = nil
				}
				if action == "automatic" {
					req.Handoff = "automatic"
				}
				_, err = s.Configure(ctx, owner(), req)
			}
			if err != nil {
				t.Fatal(err)
			}
			// Reconstruct the coordinator: no in-memory dirty set may be required.
			s = New(db, allowOwner{}, s.tasks)
			if err = s.Tick(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
			got, err := notices.Get(ctx, owner(), n.ID)
			if err != nil || got.ResolvedAt == nil {
				t.Fatal(got, err)
			}
			if action == "start" {
				observed := &testkit.ObservedStore{Store: db}
				s.store = observed
				if err = s.Tick(ctx, time.Now()); err != nil {
					t.Fatal(err)
				}
				if observed.Rows[bucket] != 0 {
					t.Fatal("started plan remains candidate", observed)
				}
			}
		})
	}
}
