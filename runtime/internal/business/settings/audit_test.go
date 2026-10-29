package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDSettingsSensitiveAuditsCommitWithEffectsAndRedactProofs(t *testing.T) {
	db := fixtureDB(t)
	ctx := testkit.Context(t)
	object := core.Object{Kind: "task", ID: "private-memory"}
	fixturePut(t, db, "business.tasks", "owner/private-memory", struct {
		OwnerID core.ID
		Text    string
	}{"owner", "SECRET_EXPORTED_MEMORY"})
	proof := auth.Verification{Token: core.NewSecret("SECRET_VERIFICATION_PROOF")}
	saved := 0
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, CheckVerification: func(_ context.Context, _ core.Principal, v auth.Verification, _ string) error {
		if v.Token.Reveal() != proof.Token.Reveal() {
			return forbidden()
		}
		return nil
	}, SaveExport: func(ctx context.Context, p core.Principal, b ExportBundle) (core.ID, error) {
		saved++
		err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "fixture.export-content", persistence.Key(p.OwnerID, "file"), b)
		})
		return "file", err
	}, CleanupObject: func(ctx context.Context, p core.Principal, o core.Object, apply bool) error {
		return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var row json.RawMessage
			if err := tx.Get(ctx, "business.tasks", persistence.Key(p.OwnerID, o.ID), &row); err != nil {
				return err
			}
			if apply {
				return tx.Delete(ctx, "business.tasks", persistence.Key(p.OwnerID, o.ID))
			}
			return nil
		})
	}})
	req := ExportRequest{Mutation: mutation("audited-export", 0), Verification: proof}
	db.FailNextCommit(errors.New("commit interrupted"))
	_, err := service.Export(ctx, owner(), req)
	if err == nil || saved != 0 || fixtureCount(t, db, "security.audit") != 0 {
		t.Fatal("failed export committed audit or external file effect")
	}
	exported, err := service.Export(ctx, owner(), req)
	testkit.RequireNoError(t, err)
	if exported.State != "completed" {
		t.Fatal("export did not complete")
	}
	rows, err := audit.NewRepository(db).List(ctx, "owner", audit.Query{})
	testkit.RequireNoError(t, err)
	if len(rows.Items) != 2 {
		t.Fatalf("export request/completion audit missing: %+v", rows)
	}
	replayed, err := service.Export(ctx, owner(), req)
	testkit.RequireNoError(t, err)
	if replayed.ID != exported.ID || saved != 1 || fixtureCount(t, db, "security.audit") != 2 {
		t.Fatal("export replay repeated file creation or audit")
	}
	cleanup := CleanupRequest{Mutation: mutation("audited-cleanup", 0), Verification: proof, ObjectIDs: []core.Object{object}}
	db.FailNextCommit(errors.New("cleanup interrupted"))
	err = service.Cleanup(ctx, owner(), cleanup)
	if err == nil || fixtureCount(t, db, "business.tasks") != 1 || fixtureCount(t, db, "security.audit") != 2 {
		t.Fatal("cleanup/audit rollback was partial")
	}
	testkit.RequireNoError(t, service.Cleanup(ctx, owner(), cleanup))
	if fixtureCount(t, db, "business.tasks") != 0 {
		t.Fatal("cleanup failed")
	}
	rows, err = audit.NewRepository(db).List(ctx, "owner", audit.Query{})
	testkit.RequireNoError(t, err)
	if len(rows.Items) != 3 {
		t.Fatal("cleanup audit missing")
	}
	testkit.RequireNoError(t, service.Cleanup(ctx, owner(), cleanup))
	if fixtureCount(t, db, "security.audit") != 3 {
		t.Fatal("cleanup replay repeated audit")
	}
	found := false
	for _, record := range rows.Items {
		if record.Action == "settings.cleanup" && record.Target == object && record.Outcome == audit.Succeeded {
			found = true
		}
	}
	if !found {
		t.Fatal("cleanup audit omitted affected object")
	}
	encoded, err := json.Marshal(rows)
	testkit.RequireNoError(t, err)
	for _, secret := range []string{"SECRET_VERIFICATION_PROOF", "SECRET_EXPORTED_MEMORY"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("audit leaked %s", secret)
		}
	}
}
