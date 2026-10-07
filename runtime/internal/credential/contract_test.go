package credential

import (
	"errors"
	"strings"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Factory binds real manager/broker/repository and trusted test resource records.
// Verification returns a real, fresh owner verification; Bind persists an
// approved resource/provider origin binding rather than changing broker logic.
type contractFixture struct {
	Manager      Manager
	Broker       Broker
	Repository   Repository
	Owner        core.Principal
	OtherOwner   core.Principal
	Assistant    core.Principal
	Execution    core.Execution
	Verification func(string) auth.Verification
	Bind         func(core.ID) Access
	RevokeAccess func(Access)
	ExpireAccess func(Access)
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "credential", newContractFixture)
}
func put(t *testing.T, f contractFixture) Credential {
	t.Helper()
	v, err := f.Manager.Put(testkit.Context(t), f.Owner, PutRequest{Mutation: core.Mutation{IdempotencyKey: "put"}, Provider: "test-provider", Label: "provider key", Value: core.NewSecret("never-export-this-secret"), Verification: f.Verification("credential.put")})
	testkit.RequireNoError(t, err)
	return v
}
func TestOwnerConnectionSecretRequiresOwnerSessionAndProvider(t *testing.T) {
	f := fixture(t)
	c := put(t, f)
	calls := 0
	use := func(secret core.Secret) error {
		calls++
		if secret.Reveal() != "never-export-this-secret" {
			t.Fatal("wrong secret")
		}
		return nil
	}
	testkit.RequireNoError(t, f.Broker.WithOwnerSecret(testkit.Context(t), f.Owner, c.ID, c.Provider, use))
	for _, p := range []core.Principal{f.OtherOwner, f.Assistant, {}} {
		if f.Broker.WithOwnerSecret(testkit.Context(t), p, c.ID, c.Provider, use) == nil {
			t.Fatal("unauthorized draft secret use")
		}
	}
	if f.Broker.WithOwnerSecret(testkit.Context(t), f.Owner, c.ID, "different-provider", use) == nil {
		t.Fatal("wrong credential provider accepted")
	}
	testkit.RequireNoError(t, f.Manager.Delete(testkit.Context(t), f.Owner, DeleteRequest{ID: c.ID, Mutation: core.Mutation{IdempotencyKey: "delete", ExpectedVersion: c.Version}, Verification: f.Verification("credential.delete")}))
	if f.Broker.WithOwnerSecret(testkit.Context(t), f.Owner, c.ID, c.Provider, use) == nil || calls != 1 {
		t.Fatal("deleted credential still usable")
	}
}
func TestTDDCredentialMetadataLifecycle(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	c := put(t, f)
	got, err := f.Manager.Get(ctx, f.Owner, c.ID)
	testkit.RequireNoError(t, err)
	if got.ID != c.ID || got.OwnerID != f.Owner.OwnerID || got.Label != "provider key" {
		t.Fatal("metadata mismatch")
	}
	p, err := f.Manager.List(ctx, f.Owner, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 1 || p.Items[0].ID != c.ID {
		t.Fatal("credential missing from list")
	}
	testkit.RequireNoError(t, f.Manager.Delete(ctx, f.Owner, DeleteRequest{Mutation: core.Mutation{IdempotencyKey: "delete", ExpectedVersion: c.Version}, ID: c.ID, Verification: f.Verification("credential.delete")}))
	_, err = f.Manager.Get(ctx, f.Owner, c.ID)
	testkit.RequireCode(t, err, core.NotFound)
}
func TestTDDCredentialOwnerAndVerificationBoundaries(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	c := put(t, f)
	for _, p := range []core.Principal{f.OtherOwner, f.Assistant, {}} {
		if _, err := f.Manager.Get(ctx, p, c.ID); err == nil {
			t.Fatal("unauthorized credential read")
		}
		list, err := f.Manager.List(ctx, p, core.PageRequest{})
		if err == nil && len(list.Items) != 0 {
			t.Fatal("unauthorized credential listing")
		}
		if _, err := f.Manager.Put(ctx, p, PutRequest{Mutation: core.Mutation{IdempotencyKey: "unauthorized"}, Provider: "test-provider", Value: core.NewSecret("secret")}); err == nil {
			t.Fatal("unauthorized credential creation")
		}
		if err := f.Manager.Delete(ctx, p, DeleteRequest{Mutation: core.Mutation{IdempotencyKey: "unauthorized-delete", ExpectedVersion: c.Version}, ID: c.ID}); err == nil {
			t.Fatal("unauthorized credential deletion")
		}
	}
	if _, err := f.Manager.Put(ctx, f.Owner, PutRequest{Mutation: core.Mutation{IdempotencyKey: "without-verification"}, Provider: "test-provider", Value: core.NewSecret("secret")}); err == nil {
		t.Fatal("write without reauthentication accepted")
	}
}
func TestTDDBrokerDestinationAndLiveRevocation(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	c := put(t, f)
	access := f.Bind(c.ID)
	calls := 0
	testkit.RequireNoError(t, f.Broker.WithSecret(ctx, access, func(secret core.Secret) error {
		calls++
		if secret.Reveal() != "never-export-this-secret" {
			t.Fatal("wrong secret")
		}
		return nil
	}))
	if calls != 1 {
		t.Fatal("callback count mismatch")
	}
	injected := errors.New("adapter failure")
	if err := f.Broker.WithSecret(ctx, access, func(core.Secret) error { return injected }); !errors.Is(err, injected) {
		t.Fatal("adapter failure swallowed")
	}
	for _, mutate := range []func(*Access){func(a *Access) { a.Origin = "https://attacker.example" }, func(a *Access) { a.ResourceID = "other-resource" }, func(a *Access) { a.Execution.Principal.OwnerID = f.OtherOwner.OwnerID }, func(a *Access) { a.CredentialID = "other-credential" }} {
		bad := access
		mutate(&bad)
		before := calls
		if err := f.Broker.WithSecret(ctx, bad, func(core.Secret) error { calls++; return nil }); err == nil {
			t.Fatal("unapproved secret use accepted")
		}
		if calls != before {
			t.Fatal("secret exposed before authorization")
		}
	}
	f.RevokeAccess(access)
	if err := f.Broker.WithSecret(ctx, access, func(core.Secret) error { calls++; return nil }); err == nil {
		t.Fatal("revoked grant retained access")
	}
	if calls != 1 {
		t.Fatal("revoked callback invoked")
	}
}
func TestTDDBrokerRefreshRechecksBinding(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	c := put(t, f)
	access := f.Bind(c.ID)
	testkit.RequireNoError(t, f.Broker.Refresh(ctx, access))
	f.RevokeAccess(access)
	if err := f.Broker.Refresh(ctx, access); err == nil {
		t.Fatal("refresh bypassed revocation")
	}
}
func TestTDDRepositoryEncryptionIsolationAndCAS(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	c := put(t, f)
	r := f.Repository
	enc, err := r.Get(ctx, f.Owner.OwnerID, c.ID)
	testkit.RequireNoError(t, err)
	if len(enc.Ciphertext) == 0 || strings.Contains(string(enc.Ciphertext), "never-export-this-secret") || enc.KeyID == "" {
		t.Fatal("credential not encrypted at rest")
	}
	foreign, err := r.List(ctx, f.OtherOwner.OwnerID, core.PageRequest{})
	testkit.RequireNoError(t, err)
	if len(foreign.Items) != 0 {
		t.Fatal("metadata crossed owner boundary")
	}
	if _, err = r.Get(ctx, f.OtherOwner.OwnerID, c.ID); err == nil {
		t.Fatal("cross-owner encrypted value read")
	}
	enc.Metadata.Label = "renamed"
	testkit.RequireNoError(t, r.Save(ctx, enc, c.Version))
	if err = r.Save(ctx, enc, c.Version); err == nil {
		t.Fatal("stale encrypted-value save accepted")
	}
	updated, err := r.Get(ctx, f.Owner.OwnerID, c.ID)
	testkit.RequireNoError(t, err)
	if updated.Metadata.Label != "renamed" || updated.Metadata.Version <= c.Version {
		t.Fatal("CAS update missing")
	}
	if err = r.Delete(ctx, f.OtherOwner.OwnerID, c.ID, updated.Metadata.Version); err == nil {
		t.Fatal("foreign owner deleted secret")
	}
	if err = r.Delete(ctx, f.Owner.OwnerID, c.ID, c.Version); err == nil {
		t.Fatal("stale delete accepted")
	}
	testkit.RequireNoError(t, r.Delete(ctx, f.Owner.OwnerID, c.ID, updated.Metadata.Version))
	_, err = r.Get(ctx, f.Owner.OwnerID, c.ID)
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDCredentialIdempotencyAndVersion(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	request := PutRequest{Mutation: core.Mutation{IdempotencyKey: "same-put"}, Provider: "test-provider", Label: "first", Value: core.NewSecret("never-export-this-secret"), Verification: f.Verification("credential.put")}
	first, err := f.Manager.Put(ctx, f.Owner, request)
	testkit.RequireNoError(t, err)
	request.Verification = f.Verification("credential.put")
	again, err := f.Manager.Put(ctx, f.Owner, request)
	testkit.RequireNoError(t, err)
	if again.ID != first.ID || again.Version != first.Version {
		t.Fatal("credential retry created a new version")
	}
	request.Label = "changed"
	request.Verification = f.Verification("credential.put")
	_, err = f.Manager.Put(ctx, f.Owner, request)
	testkit.RequireCode(t, err, core.Conflict)
	request.ID = first.ID
	request.Mutation = core.Mutation{IdempotencyKey: "update", ExpectedVersion: first.Version}
	request.Verification = f.Verification("credential.put")
	updated, err := f.Manager.Put(ctx, f.Owner, request)
	testkit.RequireNoError(t, err)
	if updated.ID != first.ID || updated.Version <= first.Version {
		t.Fatal("update lost identity/version")
	}
	request.Mutation.IdempotencyKey = "stale-update"
	request.Verification = f.Verification("credential.put")
	_, err = f.Manager.Put(ctx, f.Owner, request)
	testkit.RequireCode(t, err, core.Conflict)
}

func TestTDDBrokerExpiredAccessDoesNotExposeSecret(t *testing.T) {
	f := fixture(t)
	c := put(t, f)
	access := f.Bind(c.ID)
	f.ExpireAccess(access)
	called := false
	err := f.Broker.WithSecret(testkit.Context(t), access, func(core.Secret) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("expired execution or grant exposed secret")
	}
}
