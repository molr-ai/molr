package resource

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestProviderConnectionOwnerOnlyAndVersionedChanges(t *testing.T) {
	db := fixtureDB(t)
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"openai-compatible": {
		Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read", "execute"}, nil },
	}}})
	req := SaveProviderRequest{Name: "Local", Endpoint: "http://127.0.0.1:11434/v1/chat/completions", Mutation: mutation("save", 0)}
	r, err := s.SaveProvider(t.Context(), owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.SaveProvider(t.Context(), owner(), req)
	if err != nil || replay.ID != r.ID {
		t.Fatalf("save replay: %#v %v", replay, err)
	}
	r, err = s.ConfigureModel(t.Context(), owner(), ConfigureModelRequest{ID: r.ID, Model: ModelConfig{Name: "manual", ContextLimit: 8192}, Mutation: mutation("model", r.Version)})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := s.ProviderConnection(t.Context(), owner(), r.ID)
	if err != nil || connection.Endpoint != req.Endpoint || connection.Resource.ID != r.ID {
		t.Fatalf("connection metadata: %#v %v", connection, err)
	}
	req.ID, req.Name, req.Endpoint, req.Mutation = r.ID, "Updated", "http://127.0.0.1:11435/v1/chat/completions", mutation("update", r.Version)
	updated, err := s.SaveProvider(t.Context(), owner(), req)
	if err != nil || updated.ID != r.ID || updated.Name != "Updated" {
		t.Fatalf("update: %#v %v", updated, err)
	}
	b, err := NewRepository(db).Binding(t.Context(), owner().OwnerID, r.ID)
	if err != nil || len(b.Models) != 1 || b.Models[0].Name != "manual" {
		t.Fatal("manual model configuration lost")
	}
	req.Mutation = mutation("stale", r.Version)
	_, err = s.SaveProvider(t.Context(), owner(), req)
	wantCode(t, err, core.Conflict)
	for _, actor := range []core.Principal{otherOwner(), agent()} {
		_, err = s.ProviderConnection(t.Context(), actor, r.ID)
		wantDenied(t, err)
		_, err = s.SaveProvider(t.Context(), actor, req)
		wantDenied(t, err)
	}
}
