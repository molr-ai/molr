package app

import (
	"bytes"
	"context"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/policy"
)

type providerTestPolicy struct{}

func (providerTestPolicy) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}

func TestProviderKeyRotationRollsBackWithConnectionFailure(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	// Reusing an audit ID injects a late failure after credential and binding writes.
	s := resource.NewService(b.store, resource.Options{Authorizer: providerTestPolicy{}, Credentials: b.Services.Credentials, Providers: (adapters{networkConfig: cfg.Network}).providers(), NewID: func() core.ID { return "provider-audit-collision" }})
	proof := func() auth.Verification {
		v, err := b.Services.Auth.Reauthenticate(t.Context(), p, auth.ReauthenticateRequest{Purpose: "credential.put", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
		need(t, err)
		return v
	}
	req := resource.SaveProviderRequest{Name: "Provider", Endpoint: "http://127.0.0.1:11434/v1/chat/completions", UseAPIKey: true, APIKey: core.NewSecret("original-secret"), Verification: proof(), Mutation: core.Mutation{IdempotencyKey: "create"}}
	r, err := s.SaveProvider(t.Context(), p, req)
	need(t, err)
	connection, err := s.ProviderConnection(t.Context(), p, r.ID)
	need(t, err)
	repo := credential.NewRepository(b.store)
	before, err := repo.Get(t.Context(), p.OwnerID, connection.CredentialID)
	need(t, err)
	req.ID, req.Name, req.APIKey, req.Verification = r.ID, "Changed", core.NewSecret("replacement-secret"), proof()
	req.Mutation = core.Mutation{IdempotencyKey: "rotate", ExpectedVersion: r.Version}
	if _, err = s.SaveProvider(t.Context(), p, req); core.CodeOf(err) != core.Conflict {
		t.Fatalf("late failure not exercised: %v", err)
	}
	after, err := repo.Get(t.Context(), p.OwnerID, connection.CredentialID)
	need(t, err)
	if !bytes.Equal(before.Ciphertext, after.Ciphertext) || before.Metadata.Version != after.Metadata.Version {
		t.Fatal("failed provider update rotated the key")
	}
	current, err := s.ProviderConnection(t.Context(), p, r.ID)
	need(t, err)
	if current.Resource.Name != "Provider" || current.Resource.Version != r.Version {
		t.Fatal("failed provider update changed the connection")
	}
	// The failed transaction also rolls back proof consumption, so retry can commit.
	updated, err := b.Services.Resources.SaveProvider(t.Context(), p, req)
	need(t, err)
	if updated.Name != "Changed" {
		t.Fatal("retry did not commit")
	}
	keys, err := b.Services.Credentials.List(t.Context(), p, core.PageRequest{})
	need(t, err)
	if len(keys.Items) != 1 || keys.Items[0].Version != 2 {
		t.Fatal("key replacement created an extra credential")
	}
}
