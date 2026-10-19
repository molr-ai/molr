// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package resource

import (
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func owner() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
}
func otherOwner() core.Principal {
	return core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
}
func agent() core.Principal {
	return core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
}
func mutation(key string, v core.Version) core.Mutation {
	return core.Mutation{IdempotencyKey: key, ExpectedVersion: v}
}
func wantCode(t *testing.T, err error, code core.Code) {
	t.Helper()
	if core.CodeOf(err) != code {
		t.Fatalf("error = %v (%s), want %s", err, core.CodeOf(err), code)
	}
}
func wantDenied(t *testing.T, err error) {
	t.Helper()
	c := core.CodeOf(err)
	if c != core.Forbidden && c != core.NotFound {
		t.Fatalf("cross-owner access = %v, want forbidden or not found", err)
	}
}

// SupportedProvider/Binding point to a deterministic local provider dependency.
// No test needs public network access or actual host filesystem credentials.
type contractFixture struct {
	Service            Service
	Repository         Repository
	SupportedProvider  string
	SafeBinding        Binding
	SetProviderOffline func(bool)
}

var newContractFixture func(*testing.T) contractFixture

func connect(t *testing.T, f contractFixture) Resource {
	t.Helper()
	r, e := f.Service.Connect(testkit.Context(t), owner(), ConnectRequest{Mutation: mutation("connect", 0), Kind: Storage, Name: "Documents", Provider: f.SupportedProvider, Binding: f.SafeBinding})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestTDDResourceConnectLifecycle(t *testing.T) {
	f := testkit.RequireFactory(t, "resource", newContractFixture)
	ctx := testkit.Context(t)
	r := connect(t, f)
	if r.ID == "" || r.OwnerID != catalog.Scope || r.Version != 1 || r.Kind != Storage {
		t.Fatalf("connect: %+v", r)
	}
	replay := connect(t, f)
	if replay.ID != r.ID {
		t.Fatal("idempotent connect created a second resource")
	}
	got, err := f.Service.Get(ctx, owner(), r.ID)
	if err != nil || got.ID != r.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	page, err := f.Service.List(ctx, owner(), Query{Kind: Storage, Name: "Documents"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("rename", r.Version), ID: r.ID, Name: "Archive"})
	if err != nil || updated.Name != "Archive" || updated.Version <= r.Version {
		t.Fatalf("update: %+v %v", updated, err)
	}
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("stale", r.Version), ID: r.ID, Name: "Stale"})
	wantCode(t, err, core.Conflict)
	status, err := f.Service.Check(ctx, owner(), r.ID)
	if err != nil || status.State != Available || status.CheckedAt.IsZero() {
		t.Fatalf("check: %+v %v", status, err)
	}
	f.SetProviderOffline(true)
	status, err = f.Service.Check(ctx, owner(), r.ID)
	if err != nil || status.State != Offline {
		t.Fatalf("offline check: %+v %v", status, err)
	}
	// Health probes may persist a new resource state and advance its version.
	current, err := f.Service.Get(ctx, owner(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Service.Disconnect(ctx, owner(), DisconnectRequest{Mutation: mutation("disconnect", current.Version), ID: r.ID}); err != nil {
		t.Fatal(err)
	}
	got, err = f.Service.Get(ctx, owner(), r.ID)
	if err != nil || got.State != Disconnected {
		t.Fatalf("disconnect: %+v %v", got, err)
	}
}
func TestTDDResourceOwnerOnlyAndSafeDescriptors(t *testing.T) {
	f := testkit.RequireFactory(t, "resource", newContractFixture)
	ctx := testkit.Context(t)
	r := connect(t, f)
	_, err := f.Service.Get(ctx, otherOwner(), r.ID)
	wantDenied(t, err)
	_, err = f.Service.List(ctx, otherOwner(), Query{})
	wantDenied(t, err)
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{Mutation: mutation("foreign-update", r.Version), ID: r.ID, Name: "Other"})
	wantDenied(t, err)
	_, err = f.Service.Check(ctx, otherOwner(), r.ID)
	wantDenied(t, err)
	wantDenied(t, f.Service.Disconnect(ctx, otherOwner(), DisconnectRequest{Mutation: mutation("foreign-disconnect", r.Version), ID: r.ID}))
	_, err = f.Service.Connect(ctx, agent(), ConnectRequest{Mutation: mutation("agent-connect", 0), Kind: Storage, Name: "Agent", Provider: f.SupportedProvider, Binding: f.SafeBinding})
	wantCode(t, err, core.Forbidden)
	_, err = f.Service.Connect(ctx, owner(), ConnectRequest{Mutation: mutation("unknown-provider", 0), Kind: Storage, Name: "Unknown", Provider: "not-supported", Binding: f.SafeBinding})
	wantCode(t, err, core.InvalidArgument)
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{f.SafeBinding.Endpoint, f.SafeBinding.RootPath, string(f.SafeBinding.CredentialID)} {
		if secret != "" && strings.Contains(string(encoded), secret) {
			t.Fatalf("descriptor leaked binding %q", secret)
		}
	}
}
func TestTDDResourceRepositorySharedCatalog(t *testing.T) {
	f := testkit.RequireFactory(t, "resource", newContractFixture)
	ctx := testkit.Context(t)
	r := Resource{ID: "resource-row", OwnerID: "owner", Kind: Storage, Name: "Documents", Provider: f.SupportedProvider, State: Available, Version: 1}
	binding := f.SafeBinding
	binding.ResourceID = r.ID
	if err := f.Repository.Insert(ctx, r, binding); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Insert(ctx, r, binding), core.Conflict)
	got, err := f.Repository.Get(ctx, "owner", r.ID)
	if err != nil || got.ID != r.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	shared, err := f.Repository.Get(ctx, "other", r.ID)
	if err != nil || shared.OwnerID != catalog.Scope || shared.ID != r.ID {
		t.Fatal(shared, err)
	}
	page, err := f.Repository.List(ctx, "owner", Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	b, err := f.Repository.Binding(ctx, "owner", r.ID)
	if err != nil || b.ResourceID != r.ID || b.CredentialID != binding.CredentialID {
		t.Fatalf("binding: %+v %v", b, err)
	}
	sharedBinding, err := f.Repository.Binding(ctx, "other", r.ID)
	if err != nil || sharedBinding.ResourceID != r.ID {
		t.Fatal(sharedBinding, err)
	}
	r.Version = 2
	r.Name = "Archive"
	if err = f.Repository.Save(ctx, r, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Save(ctx, r, 1), core.Conflict)
}
