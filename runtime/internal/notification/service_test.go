package notification

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"testing"
)

type allow struct{}

func (allow) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.RequireOwner()
}
func principal(id core.ID) core.Principal {
	return core.Principal{OwnerID: id, SubjectID: id, Kind: core.ActorOwner, SessionID: "session"}
}
func TestLifecycleIsolationReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	s := New(db, allow{})
	p := principal("owner")
	in := Input{Kind: "weixin.delivery", Code: "unknown:unknown", Source: core.Object{Kind: "weixin_delivery", ID: "delivery"}, AssistantID: "assistant"}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(Sync(ctx, db, p.OwnerID, in, true))
	check(Sync(ctx, db, p.OwnerID, in, true))
	list, err := s.List(ctx, p, Query{})
	check(err)
	if len(list.Items) != 1 || list.UnreadCount != 1 {
		t.Fatal(list)
	}
	first := list.Items[0]
	check(s.Update(ctx, p, []core.ID{first.ID}, "read"))
	list, err = s.List(ctx, p, Query{})
	check(err)
	if list.UnreadCount != 0 || list.PendingCount != 1 {
		t.Fatal(list)
	}
	check(s.Update(ctx, p, []core.ID{first.ID}, "dismiss"))
	check(Sync(ctx, db, p.OwnerID, in, true))
	dismissed, err := s.Get(ctx, p, first.ID)
	check(err)
	check(s.Update(ctx, p, []core.ID{first.ID}, "dismiss"))
	same, err := s.Get(ctx, p, first.ID)
	check(err)
	if same.Version != dismissed.Version {
		t.Fatal("duplicate acknowledgement changed version")
	}
	check(db.Close())
	db, err = sqlitestore.Open(path)
	check(err)
	s = New(db, allow{})
	same, err = s.Get(ctx, p, first.ID)
	check(err)
	if same.DismissedAt == nil || same.ReadAt == nil || same.ResolvedAt != nil {
		t.Fatal(same)
	}
	check(Sync(ctx, db, p.OwnerID, in, false))
	same, err = s.Get(ctx, p, first.ID)
	check(err)
	if same.ResolvedAt == nil {
		t.Fatal("not resolved")
	}
	check(Sync(ctx, db, p.OwnerID, in, true))
	in.Code = "failed:rejected"
	check(Sync(ctx, db, p.OwnerID, in, true))
	list, err = s.List(ctx, p, Query{Filter: "active"})
	check(err)
	if len(list.Items) != 1 || list.Items[0].ID == first.ID || list.Items[0].ReadAt != nil {
		t.Fatal(list)
	}
	other := principal("other")
	empty, err := s.List(ctx, other, Query{})
	check(err)
	if len(empty.Items) != 0 {
		t.Fatal("owner leak")
	}
	if _, err = s.Get(ctx, other, first.ID); !persistence.Missing(err) {
		t.Fatal(err)
	}
	if err = s.Update(ctx, other, []core.ID{first.ID}, "dismiss"); !persistence.Missing(err) {
		t.Fatal(err)
	}
	// Batch failure rolls back earlier acknowledgements.
	active := list.Items[0]
	if err = s.Update(ctx, p, []core.ID{active.ID, "missing"}, "dismiss"); !persistence.Missing(err) {
		t.Fatal(err)
	}
	active, err = s.Get(ctx, p, active.ID)
	check(err)
	if active.DismissedAt != nil {
		t.Fatal("partial batch")
	}
	page, err := s.List(ctx, p, Query{Page: core.PageRequest{Limit: 1}})
	check(err)
	if page.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	next, err := s.List(ctx, p, Query{Page: core.PageRequest{Limit: 1, Cursor: page.NextCursor}})
	check(err)
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next)
	}
	if _, err = s.List(ctx, other, Query{Page: core.PageRequest{Cursor: page.NextCursor}}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	events, err := realtime.NewStore(db).Replay(ctx, p.OwnerID, realtime.Query{Topics: []string{"notification.changed"}})
	check(err)
	if len(events.Items) != 7 {
		t.Fatalf("unexpected event count %d", len(events.Items))
	}
	assistant := core.Principal{OwnerID: p.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	if _, err = s.List(ctx, assistant, Query{}); err == nil {
		t.Fatal("assistant read owner notices")
	}
}
func TestSourceAndNoticeRollbackTogether(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expected := errors.New("rollback")
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.fixture", "owner/source", true); err != nil {
			return err
		}
		if err := Sync(ctx, db, "owner", Input{Kind: "plan.ready", Source: core.Object{Kind: "todo", ID: "source"}}, true); err != nil {
			return err
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatal(err)
	}
	list, err := New(db, allow{}).List(ctx, principal("owner"), Query{})
	if err != nil || len(list.Items) != 0 {
		t.Fatal(list, err)
	}
	events, err := realtime.NewStore(db).Replay(ctx, "owner", realtime.Query{})
	if err != nil || len(events.Items) != 0 {
		t.Fatal(events, err)
	}
}

type readOnly struct{}

func (readOnly) Require(_ context.Context, p core.Principal, q policy.CheckRequest) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if q.Action != policy.Read {
		return core.NewError(core.Forbidden, "read-only credential")
	}
	return nil
}
func TestReadOnlyPrincipalCannotAcknowledge(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	p := principal("owner")
	if err := Sync(ctx, db, p.OwnerID, Input{Kind: "plan.ready", Source: core.Object{Kind: "todo", ID: "plan"}}, true); err != nil {
		t.Fatal(err)
	}
	s := New(db, readOnly{})
	rows, err := s.List(ctx, p, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, p, []core.ID{rows.Items[0].ID}, "read"); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	n, err := s.Get(ctx, p, rows.Items[0].ID)
	if err != nil || n.ReadAt != nil {
		t.Fatal(n, err)
	}
}
