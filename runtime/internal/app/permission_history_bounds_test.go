package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type permissionHistoryStore struct {
	persistence.Store
	rows      int
	queries   int
	failQuery int
}

type permissionHistoryTx struct {
	persistence.Tx
	counts *permissionHistoryStore
}

func (s *permissionHistoryStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return fn(ctx, &permissionHistoryTx{tx, s})
	})
}

func (tx *permissionHistoryTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == permissionRequestsBucket {
		tx.counts.queries++
		if tx.counts.failQuery == tx.counts.queries {
			return nil, errors.New("synthetic query failure")
		}
	}
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == permissionRequestsBucket {
		tx.counts.rows += len(rows)
	}
	return rows, err
}

func TestPermissionHistoryLoadsOnlyVisibleRequests(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "requests.db"))
	need(t, err)
	defer db.Close()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			state := "rejected"
			if i < 3 {
				state = "pending"
			}
			id := core.ID(fmt.Sprintf("request-%04d", i))
			row := policy.PermissionRequest{ID: id, OwnerID: "owner", AssistantID: "assistant", State: state, Version: 1, CreatedAt: base.Add(time.Duration(i) * time.Nanosecond)}
			if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key("owner", id), row); err != nil {
				return err
			}
		}
		return nil
	}))
	counts := &permissionHistoryStore{Store: db}
	s := &permissionRequests{store: counts, authorizer: providerTestPolicy{}}
	for _, tc := range []struct {
		name  string
		limit int
	}{{"owner_list", 200}, {"model_context", 40}} {
		t.Run(tc.name, func(t *testing.T) {
			counts.rows = 0
			var rows []policy.PermissionRequest
			if tc.name == "owner_list" {
				rows, err = s.List(t.Context(), core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, "assistant")
			} else {
				var docsErr error
				docs, docsErr := s.context(t.Context(), core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant"})
				need(t, docsErr)
				if len(docs) != 2 {
					t.Fatalf("unexpected context documents: %d", len(docs))
				}
				err = json.Unmarshal([]byte(docs[0].Content), &rows)
			}
			need(t, err)
			if len(rows) != tc.limit || rows[0].ID != "request-0002" || rows[1].ID != "request-0001" || rows[2].ID != "request-0000" || rows[3].ID != "request-0999" {
				t.Fatalf("wrong pending-first, newest-first history: %+v", rows)
			}
			t.Logf("returned=%d loaded=%d", len(rows), counts.rows)
			if counts.rows > tc.limit {
				t.Fatalf("loaded %d permission requests for %d visible records", counts.rows, tc.limit)
			}
		})
	}
}

func TestPermissionHistoryOrderingScopeAndQueryFailure(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "order.db"))
	need(t, err)
	defer db.Close()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []policy.PermissionRequest{
		{ID: "pending-b", OwnerID: "owner", AssistantID: "assistant", State: "pending", CreatedAt: base},
		{ID: "pending-a", OwnerID: "owner", AssistantID: "assistant", State: "pending", CreatedAt: base.In(time.FixedZone("offset", 3600))},
		{ID: "rejected-b", OwnerID: "owner", AssistantID: "assistant", State: "rejected", CreatedAt: base.Add(time.Second)},
		{ID: "approved-a", OwnerID: "owner", AssistantID: "assistant", State: "approved", CreatedAt: base.Add(time.Second)},
		{ID: "approved-newest", OwnerID: "owner", AssistantID: "assistant", State: "approved", CreatedAt: base.Add(time.Second + time.Nanosecond)},
		{ID: "foreign-owner", OwnerID: "other", AssistantID: "assistant", State: "pending", CreatedAt: base.Add(time.Hour)},
		{ID: "foreign-assistant", OwnerID: "owner", AssistantID: "other", State: "pending", CreatedAt: base.Add(time.Hour)},
	}
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range rows {
			if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key(r.OwnerID, r.ID), r); err != nil {
				return err
			}
		}
		return nil
	}))
	counts := &permissionHistoryStore{Store: db}
	s := &permissionRequests{store: counts, authorizer: providerTestPolicy{}}
	for _, tc := range []struct {
		limit int
		want  []core.ID
	}{{1, []core.ID{"pending-a"}}, {2, []core.ID{"pending-a", "pending-b"}}, {4, []core.ID{"pending-a", "pending-b", "approved-newest", "approved-a"}}, {200, []core.ID{"pending-a", "pending-b", "approved-newest", "approved-a", "rejected-b"}}} {
		got, err := s.rows(t.Context(), "owner", "assistant", tc.limit)
		need(t, err)
		ids := []core.ID{}
		for _, r := range got {
			ids = append(ids, r.ID)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Fatalf("limit %d: %v != %v", tc.limit, ids, tc.want)
		}
	}
	empty, err := s.rows(t.Context(), "absent", "assistant", 200)
	need(t, err)
	if empty == nil || len(empty) != 0 {
		t.Fatal("empty history must be an empty array", empty)
	}
	for _, nth := range []int{1, 2} {
		counts.queries = 0
		counts.failQuery = nth
		got, err := s.rows(t.Context(), "owner", "assistant", 200)
		if err == nil || got != nil {
			t.Fatal("query failure returned partial history", got, err)
		}
	}
	counts.failQuery = 0
}

func TestPermissionCreateReadsOnlyPendingAndReusesAtCap(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := newPermissionRequestsFixture(t, f)
	input := policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Reports", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read"}, ExpiresAt: futurePermissionTime(), Reason: "Read reports"}
	raw, err := s.create(t.Context(), f.execution, input)
	need(t, err)
	var original policy.PermissionRequest
	need(t, json.Unmarshal(raw, &original))
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			r := original
			r.ID = core.ID(fmt.Sprintf("history-%04d", i))
			r.State = "rejected"
			if i < 19 {
				r.State = "pending"
			}
			r.Input.ResourceDescription = fmt.Sprintf("Other request %d", i)
			if err := tx.Put(ctx, permissionRequestsBucket, persistence.Key(r.OwnerID, r.ID), r); err != nil {
				return err
			}
		}
		return nil
	}))
	counts := &permissionHistoryStore{Store: s.store}
	s.store = counts
	duplicate := f.execution
	duplicate.OperationID = "duplicate-at-cap"
	raw, err = s.create(t.Context(), duplicate, input)
	need(t, err)
	var reused policy.PermissionRequest
	need(t, json.Unmarshal(raw, &reused))
	if reused.ID != original.ID || counts.rows != 20 {
		t.Fatalf("duplicate %s; loaded=%d", reused.ID, counts.rows)
	}
	counts.rows = 0
	fresh := f.execution
	fresh.OperationID = "new-at-cap"
	input.ResourceDescription = "New request"
	_, err = s.create(t.Context(), fresh, input)
	if core.CodeOf(err) != core.FailedPrecondition || counts.rows != 20 {
		t.Fatalf("new request at cap: %v; loaded=%d", err, counts.rows)
	}
}
