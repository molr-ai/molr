package resource

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"testing"
	"time"
)

func TestResourceReconnectDeleteLifecycle(t *testing.T) {
	for _, kind := range []Kind{Storage, Network, Compute} {
		t.Run(string(kind), func(t *testing.T) {
			db := fixtureDB(t)
			probes, ready := 0, true
			s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"test": {Kind: kind, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read", "write"}, nil }}}, CheckResource: func(context.Context, core.Principal, Resource, Binding) (Status, error) {
				probes++
				if !ready {
					return Status{State: Offline, ReasonCode: "directory_unavailable"}, nil
				}
				return Status{State: Available}, nil
			}})
			r, err := s.Connect(t.Context(), owner(), ConnectRequest{Kind: kind, Provider: "test", Name: "resource", Mutation: mutation("connect", 0)})
			if err != nil {
				t.Fatal(err)
			}
			wantCode(t, s.Delete(t.Context(), owner(), DeleteRequest{ID: r.ID, Mutation: mutation("delete-connected", r.Version)}), core.FailedPrecondition)
			if err = s.Disconnect(t.Context(), owner(), DisconnectRequest{ID: r.ID, Mutation: mutation("disconnect", r.Version)}); err != nil {
				t.Fatal(err)
			}
			r.Version++
			status, err := s.Check(t.Context(), owner(), r.ID)
			if err != nil || status.State != Disconnected || probes != 0 {
				t.Fatal(status, err, probes)
			}
			req := ReconnectRequest{ID: r.ID, Mutation: mutation("reconnect", r.Version)}
			for _, p := range []core.Principal{agent(), otherOwner()} {
				_, err = s.Reconnect(t.Context(), p, req)
				wantDenied(t, err)
			}
			ready = false
			_, err = s.Reconnect(t.Context(), owner(), req)
			wantCode(t, err, core.FailedPrecondition)
			current, err := s.Get(t.Context(), owner(), r.ID)
			if err != nil || current.State != Disconnected || current.Version != r.Version {
				t.Fatal(current, err)
			}
			ready = true
			db.FailNextCommit(errors.New("commit failed"))
			_, err = s.Reconnect(t.Context(), owner(), req)
			if err == nil {
				t.Fatal("expected rollback")
			}
			current, err = s.Get(t.Context(), owner(), r.ID)
			if err != nil || current.State != Disconnected {
				t.Fatal(current, err)
			}
			current, err = s.Reconnect(t.Context(), owner(), req)
			if err != nil || current.ID != r.ID || current.State != Available {
				t.Fatal(current, err)
			}
			before := probes
			again, err := s.Reconnect(t.Context(), owner(), req)
			if err != nil || again.Version != current.Version || probes != before {
				t.Fatal("replay changed resource", again, err, probes)
			}
			if err = s.Disconnect(t.Context(), owner(), DisconnectRequest{ID: r.ID, Mutation: mutation("disconnect-again", current.Version)}); err != nil {
				t.Fatal(err)
			}
			del := DeleteRequest{ID: r.ID, Mutation: mutation("delete", current.Version+1)}
			for _, p := range []core.Principal{agent(), otherOwner()} {
				wantDenied(t, s.Delete(t.Context(), p, del))
			}
			stale := del
			stale.Mutation = mutation("stale", 1)
			wantCode(t, s.Delete(t.Context(), owner(), stale), core.Conflict)
			db.FailNextCommit(errors.New("commit failed"))
			if err = s.Delete(t.Context(), owner(), del); err == nil {
				t.Fatal("expected rollback")
			}
			if _, err = NewRepository(db).Binding(t.Context(), "owner", r.ID); err != nil {
				t.Fatal(err)
			}
			if err = s.Delete(t.Context(), owner(), del); err != nil {
				t.Fatal(err)
			}
			if err = s.Delete(t.Context(), owner(), del); err != nil {
				t.Fatal("delete replay", err)
			}
			_, err = s.Get(t.Context(), owner(), r.ID)
			wantCode(t, err, core.NotFound)
			_, err = NewRepository(db).Binding(t.Context(), "owner", r.ID)
			wantCode(t, err, core.NotFound)
			rows, err := s.List(t.Context(), owner(), Query{})
			if err != nil || len(rows.Items) != 0 {
				t.Fatal(rows, err)
			}
		})
	}
}

func TestResourceDeleteChecksAllAccountsAndPreservesHistory(t *testing.T) {
	db := fixtureDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Now: func() time.Time { return now }})
	r := Resource{ID: "directory", Kind: Storage, Provider: "local-directory", State: Disconnected, Version: 1}
	if err := NewRepository(db).Insert(t.Context(), r, Binding{ResourceID: r.ID, RootPath: "/synthetic/path"}); err != nil {
		t.Fatal(err)
	}
	repo := policy.NewRepository(db)
	expires := now.Add(time.Hour)
	g := policy.Grant{ID: "other-grant", OwnerID: "other", AssistantID: "inactive-assistant", ResourceID: r.ID, Actions: []policy.Action{policy.Read}, ExpiresAt: &expires, Version: 1}
	if err := repo.InsertGrant(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	req := DeleteRequest{ID: r.ID, Mutation: mutation("delete", 1)}
	err := s.Delete(t.Context(), owner(), req)
	if core.ReasonOf(err) != "resource_has_grants" {
		t.Fatal(err)
	}
	if err := repo.RevokeGrant(t.Context(), g.OwnerID, g.ID, 1, now); err != nil {
		t.Fatal(err)
	}
	g.ID = "expired"
	expired := now
	g.ExpiresAt = &expired
	if err := repo.InsertGrant(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(t.Context(), owner(), req); err != nil {
		t.Fatal(err)
	}
	history, err := repo.Grants(t.Context(), "other", policy.Query{ResourceID: r.ID})
	if err != nil || len(history.Items) != 2 {
		t.Fatal("history lost", history, err)
	}
}

func TestResourceBuiltinCannotDisconnectReconnectOrDelete(t *testing.T) {
	db := fixtureDB(t)
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
	r := Resource{ID: "workspace", Kind: Storage, Provider: "managed-workspace", State: Available, Version: 1}
	if err := NewRepository(db).Insert(t.Context(), r, Binding{ResourceID: r.ID}); err != nil {
		t.Fatal(err)
	}
	wantDenied(t, s.Disconnect(t.Context(), owner(), DisconnectRequest{ID: r.ID, Mutation: mutation("disconnect", 1)}))
	_, err := s.Reconnect(t.Context(), owner(), ReconnectRequest{ID: r.ID, Mutation: mutation("reconnect", 1)})
	wantDenied(t, err)
	wantDenied(t, s.Delete(t.Context(), owner(), DeleteRequest{ID: r.ID, Mutation: mutation("delete", 1)}))
}

func TestResourceCheckCannotUndoConcurrentDisconnect(t *testing.T) {
	db := fixtureDB(t)
	repo := NewRepository(db)
	calls := 0
	r := Resource{ID: "files", Kind: Storage, Provider: "local-directory", State: Available, Version: 1}
	if err := repo.Insert(t.Context(), r, Binding{ResourceID: r.ID}); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, CheckResource: func(ctx context.Context, _ core.Principal, snapshot Resource, _ Binding) (Status, error) {
		calls++
		snapshot.State = Disconnected
		snapshot.Version++
		err := repo.Save(ctx, snapshot, snapshot.Version-1)
		return Status{State: Available}, err
	}})
	status, err := s.Check(t.Context(), owner(), r.ID)
	if err != nil || status.State != Disconnected {
		t.Fatal(status, err)
	}
	_, err = s.Check(t.Context(), owner(), r.ID)
	if err != nil || calls != 1 {
		t.Fatal("disconnected resource probed", calls, err)
	}
}

func TestResourceDeleteGrantRace(t *testing.T) {
	for range 10 {
		db := fixtureDB(t)
		repo := NewRepository(db)
		r := Resource{ID: "race", Kind: Storage, Provider: "local-directory", State: Disconnected, Version: 1}
		if err := repo.Insert(t.Context(), r, Binding{ResourceID: r.ID}); err != nil {
			t.Fatal(err)
		}
		s := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
		start := make(chan struct{})
		result := make(chan error, 2)
		go func() {
			<-start
			result <- s.Delete(t.Context(), owner(), DeleteRequest{ID: r.ID, Mutation: mutation("delete", 1)})
		}()
		go func() {
			<-start
			result <- db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if _, err := repo.Get(ctx, "other", r.ID); err != nil {
					return err
				}
				return policy.NewRepository(db).InsertGrant(ctx, policy.Grant{ID: "grant", OwnerID: "other", ResourceID: r.ID, AssistantID: "assistant", Actions: []policy.Action{policy.Read}})
			})
		}()
		close(start)
		a, b := <-result, <-result
		if (a == nil) == (b == nil) {
			t.Fatal("exactly one mutation must succeed", a, b)
		}
	}
}
