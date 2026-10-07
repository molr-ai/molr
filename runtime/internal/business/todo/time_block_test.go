package todo

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
	"time"
)

func TestTimeBlockPersistsWithCompletionAndRejectsInvalidRanges(t *testing.T) {
	f := testkit.RequireFactory(t, "todo", newContractFixture)
	ctx := testkit.Context(t)
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	due := end.Add(24 * time.Hour)
	for i, pair := range [][2]*time.Time{{&start, nil}, {nil, &end}, {&end, &start}, {&start, &start}} {
		_, err := f.Service.Create(ctx, owner(), CreateRequest{Mutation: mutation(string(rune('a'+i)), 0), Title: "Invalid", StartAt: pair[0], EndAt: pair[1]})
		wantCode(t, err, core.InvalidArgument)
	}
	req := CreateRequest{Mutation: mutation("block", 0), Title: "Report", DueAt: &due, StartAt: &start, EndAt: &end}
	created, err := f.Service.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Mutation = mutation("done", created.Version)
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: req, ID: created.ID, Completed: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.Repository.Get(ctx, owner().OwnerID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Completed || !stored.StartAt.Equal(start) || !stored.EndAt.Equal(end) || !stored.DueAt.Equal(due) || stored.ID != updated.ID {
		t.Fatalf("lost plan state: %+v", stored)
	}
	tasks, grants, events := f.Observe()
	if tasks+grants+events != 0 {
		t.Fatal("time block executed work")
	}
	req.Mutation = mutation("clear", updated.Version)
	req.StartAt = nil
	req.EndAt = nil
	cleared, err := f.Service.Update(ctx, owner(), UpdateRequest{CreateRequest: req, ID: created.ID, Completed: true})
	if err != nil || cleared.StartAt != nil || cleared.EndAt != nil {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
}
