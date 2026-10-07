package resource

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestDeleteProviderAuthorizationVersionAndReplay(t *testing.T) {
	db := fixtureDB(t)
	deleted := 0
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"test": {Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return nil, nil }}},
		CheckVerification: func(context.Context, core.Principal, auth.Verification, string) error { return nil },
		OnDeleteProvider:  func(context.Context, core.Principal, Resource, Binding) error { deleted++; return nil },
	})
	r, err := s.Connect(t.Context(), owner(), ConnectRequest{Mutation: mutation("connect", 0), Kind: Model, Name: "Provider", Provider: "test"})
	if err != nil {
		t.Fatal(err)
	}
	req := DeleteProviderRequest{ID: r.ID, Mutation: mutation("delete", r.Version)}
	for _, actor := range []core.Principal{otherOwner(), agent()} {
		wantDenied(t, s.DeleteProvider(t.Context(), actor, req))
	}
	stale := req
	stale.Mutation = mutation("stale", r.Version+1)
	wantCode(t, s.DeleteProvider(t.Context(), owner(), stale), core.Conflict)
	if err := s.DeleteProvider(t.Context(), owner(), req); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProvider(t.Context(), owner(), req); err != nil {
		t.Fatal("delete retry failed", err)
	}
	if deleted != 1 {
		t.Fatal("delete effects repeated")
	}
	_, err = s.Get(t.Context(), owner(), r.ID)
	wantCode(t, err, core.NotFound)
	_, err = NewRepository(db).Binding(t.Context(), owner().OwnerID, r.ID)
	wantCode(t, err, core.NotFound)
	rows, err := s.List(t.Context(), owner(), Query{})
	if err != nil || len(rows.Items) != 0 {
		t.Fatal("deleted provider still listed")
	}
	stale.Mutation.IdempotencyKey = req.Mutation.IdempotencyKey
	wantCode(t, s.DeleteProvider(t.Context(), owner(), stale), core.Conflict)
}
