package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDResourceAuditIsAtomicIdempotentAndContainsNoBinding(t *testing.T) {
	db := fixtureDB(t)
	ctx := testkit.Context(t)
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"audit-provider": {Kind: Storage, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read"}, nil }}}})
	req := ConnectRequest{Mutation: mutation("audit-connect", 0), Kind: Storage, Name: "private connection name", Provider: "audit-provider", Binding: Binding{Endpoint: "https://private.example/SECRET_ENDPOINT", RootPath: "/SECRET_ROOT", CredentialID: "SECRET_CREDENTIAL_REFERENCE"}}
	db.FailNextCommit(errors.New("injected commit failure"))
	_, err := service.Connect(ctx, owner(), req)
	if err == nil {
		t.Fatal("failed connection commit succeeded")
	}
	if fixtureCount(t, db, bucket) != 0 || fixtureCount(t, db, "security.audit") != 0 {
		t.Fatal("connection/audit transaction partially committed")
	}
	connected, err := service.Connect(ctx, owner(), req)
	testkit.RequireNoError(t, err)
	_, err = service.Connect(ctx, owner(), req)
	testkit.RequireNoError(t, err)
	rows, err := audit.NewRepository(db).List(ctx, "owner", audit.Query{})
	testkit.RequireNoError(t, err)
	if len(rows.Items) != 1 || rows.Items[0].Target.ID != connected.ID || rows.Items[0].Action != "resource.connect" || rows.Items[0].Outcome != audit.Succeeded {
		t.Fatalf("unexpected audit/replay: %+v", rows)
	}
	updated, err := service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("audit-update", connected.Version), ID: connected.ID, Name: "changed private name"})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, service.Disconnect(ctx, owner(), DisconnectRequest{Mutation: mutation("audit-disconnect", updated.Version), ID: connected.ID}))
	rows, err = audit.NewRepository(db).List(ctx, "owner", audit.Query{})
	testkit.RequireNoError(t, err)
	if len(rows.Items) != 3 {
		t.Fatal("sensitive resource mutation omitted audit")
	}
	encoded, err := json.Marshal(rows)
	testkit.RequireNoError(t, err)
	for _, secret := range []string{"SECRET_ENDPOINT", "SECRET_ROOT", "SECRET_CREDENTIAL_REFERENCE", "private connection name", "changed private name"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("audit included binding/body %q", secret)
		}
	}
	for _, record := range rows.Items {
		if record.ActorKind != core.ActorOwner || record.SubjectID != "owner" || record.Target.ID != connected.ID || record.At.IsZero() {
			t.Fatalf("audit identity lost: %+v", record)
		}
	}
}
