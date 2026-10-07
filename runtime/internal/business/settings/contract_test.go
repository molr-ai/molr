// These contract tests run against real implementations wired by fixture_test.go.
// Factory adapters may seed dependencies, but must not fake the service under test.
// "other" is a trusted authenticated fixture identity used to exercise isolation;
// it does not imply open registration or multiple accounts in self-host deployment.
package settings

import (
	"testing"

	"molr.ai/runtime/internal/auth"
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

// Adapter seeds authenticated owner and other accounts, existing settings at
// version 1, one assistant with a budget, and owned/foreign cleanup objects.
type contractFixture struct {
	Service            Service
	Repository         Repository
	Verify             func(core.Principal, string) auth.Verification
	AssistantBudget    func() core.AssistantBudget
	OwnedObject        core.Object
	ForeignObject      core.Object
	Exists             func(core.Object) bool
	ExpireVerification func()
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDSettingsUpdateDoesNotRewriteActiveBudget(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	before := f.AssistantBudget()
	s, err := f.Service.Get(ctx, owner())
	if err != nil || s.OwnerID != "owner" {
		t.Fatalf("get: %+v %v", s, err)
	}
	originalVersion := s.Version
	s.Theme = Dark
	s.Timezone = "Asia/Shanghai"
	s.Language = "zh-CN"

	req := UpdateRequest{Mutation: mutation("settings-update", s.Version), Settings: s}
	updated, err := f.Service.Update(ctx, owner(), req)
	if err != nil || updated.Theme != Dark || updated.Version <= originalVersion {
		t.Fatalf("update: %+v %v", updated, err)
	}
	replay, err := f.Service.Update(ctx, owner(), req)
	if err != nil || replay.Version != updated.Version {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if after := f.AssistantBudget(); after != before {
		t.Fatalf("assistant budget changed: %+v -> %+v", before, after)
	}
	req.Mutation = mutation("stale-settings", originalVersion)
	_, err = f.Service.Update(ctx, owner(), req)
	wantCode(t, err, core.Conflict)
	req.Mutation = mutation("foreign-owner-payload", updated.Version)
	req.Settings.OwnerID = "other"
	_, err = f.Service.Update(ctx, owner(), req)
	if err == nil {
		t.Fatal("settings accepted forged owner ID")
	}
	_, err = f.Service.Get(ctx, agent())
	wantCode(t, err, core.Forbidden)
}
func TestTDDExportRequiresFreshPurposeBoundVerification(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	_, err := f.Service.Export(ctx, owner(), ExportRequest{Mutation: mutation("unverified", 0), IncludeMessages: true})
	if err == nil {
		t.Fatal("export accepted no verification")
	}
	verification := f.Verify(owner(), "export")
	result, err := f.Service.Export(ctx, owner(), ExportRequest{Mutation: mutation("export", 0), Verification: verification, IncludeMessages: true, IncludeArtifacts: true})
	if err != nil || result.ID == "" || result.State == "" {
		t.Fatalf("export: %+v %v", result, err)
	}
	status, err := f.Service.ExportStatus(ctx, owner(), result.ID)
	if err != nil || status.ID != result.ID {
		t.Fatalf("status: %+v %v", status, err)
	}
	_, err = f.Service.ExportStatus(ctx, otherOwner(), result.ID)
	wantDenied(t, err)
	_, err = f.Service.Export(ctx, owner(), ExportRequest{Mutation: mutation("reuse-verification", 0), Verification: verification})
	if err == nil {
		t.Fatal("verification reused across new export")
	}
	expired := f.Verify(owner(), "export")
	f.ExpireVerification()
	_, err = f.Service.Export(ctx, owner(), ExportRequest{Mutation: mutation("expired", 0), Verification: expired})
	if err == nil {
		t.Fatal("expired verification accepted")
	}
}
func TestTDDCleanupChecksEveryObjectBeforeMutation(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	err := f.Service.Cleanup(ctx, owner(), CleanupRequest{Mutation: mutation("mixed-cleanup", 0), Verification: f.Verify(owner(), "cleanup"), ObjectIDs: []core.Object{f.OwnedObject, f.ForeignObject}})
	wantDenied(t, err)
	if !f.Exists(f.OwnedObject) || !f.Exists(f.ForeignObject) {
		t.Fatal("unauthorized mixed cleanup partially deleted data")
	}
	err = f.Service.Cleanup(ctx, owner(), CleanupRequest{Mutation: mutation("wrong-purpose", 0), Verification: f.Verify(owner(), "export"), ObjectIDs: []core.Object{f.OwnedObject}})
	if err == nil {
		t.Fatal("export verification authorized cleanup")
	}
	err = f.Service.Cleanup(ctx, owner(), CleanupRequest{Mutation: mutation("cleanup", 0), Verification: f.Verify(owner(), "cleanup"), ObjectIDs: []core.Object{f.OwnedObject}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Exists(f.OwnedObject) || !f.Exists(f.ForeignObject) {
		t.Fatal("cleanup did not isolate selected owned objects")
	}
}
func TestTDDSettingsRepositoryVersionsAndExports(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	s, err := f.Repository.Get(ctx, "owner")
	if err != nil || s.OwnerID != "owner" {
		t.Fatalf("get: %+v %v", s, err)
	}
	old := s.Version
	s.Version++
	s.Theme = Light
	if err = f.Repository.Save(ctx, s, old); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.Repository.Save(ctx, s, old), core.Conflict)
	export := Export{ID: "export-row", State: "pending"}
	if err = f.Repository.PutExport(ctx, "owner", export); err != nil {
		t.Fatal(err)
	}
	got, err := f.Repository.Export(ctx, "owner", export.ID)
	if err != nil || got.ID != export.ID || got.State != "pending" {
		t.Fatalf("export row: %+v %v", got, err)
	}
	_, err = f.Repository.Export(ctx, "other", export.ID)
	wantDenied(t, err)
	export.State = "completed"
	export.FileID = "file"
	if err = f.Repository.PutExport(ctx, "owner", export); err != nil {
		t.Fatal(err)
	}
	got, err = f.Repository.Export(ctx, "owner", export.ID)
	if err != nil || got.FileID != "file" || got.State != "completed" {
		t.Fatalf("export completion: %+v %v", got, err)
	}
}

func TestPalettePersistenceValidationAndLegacyUpdates(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	current, err := f.Service.Get(ctx, owner())
	if err != nil || current.Palette != Forest {
		t.Fatalf("legacy default: %+v %v", current, err)
	}
	for _, palette := range []Palette{Ocean, Iris, Sand, Rose, Monochrome, Forest} {
		current.Palette = palette
		updated, err := f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("palette-"+string(palette), current.Version), Settings: current})
		if err != nil {
			t.Fatal(err)
		}
		current, err = f.Service.Get(ctx, owner())
		if err != nil || current.Palette != palette || current.Version != updated.Version {
			t.Fatalf("persisted: %+v %v", current, err)
		}
	}
	current.Palette = Ocean
	current, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("ocean-again", current.Version), Settings: current})
	if err != nil {
		t.Fatal(err)
	}
	legacy := current
	legacy.Palette = ""
	legacy.Language = "zh"
	current, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("legacy-client", current.Version), Settings: legacy})
	if err != nil || current.Palette != Ocean || current.Language != "zh" {
		t.Fatalf("legacy overwrite: %+v %v", current, err)
	}
	invalid := current
	invalid.Palette = "unknown"
	_, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("bad-palette", current.Version), Settings: invalid})
	wantCode(t, err, core.InvalidArgument)
	after, err := f.Service.Get(ctx, owner())
	if err != nil || after != current {
		t.Fatalf("invalid update changed settings: %+v %v", after, err)
	}
}

func TestUIThemePersistsPerOwnerAndSurvivesLegacyUpdates(t *testing.T) {
	f := testkit.RequireFactory(t, "settings", newContractFixture)
	ctx := testkit.Context(t)
	current, err := f.Service.Get(ctx, owner())
	testkit.RequireNoError(t, err)
	if current.UITheme != DefaultUITheme {
		t.Fatalf("default: %+v", current)
	}
	original := current
	current.UITheme = "molr.test@1.0.0"
	updated, err := f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("ui-theme", current.Version), Settings: current})
	testkit.RequireNoError(t, err)
	current, err = f.Service.Get(ctx, owner())
	testkit.RequireNoError(t, err)
	if current != updated || current.UITheme != "molr.test@1.0.0" || current.Theme != original.Theme || current.Palette != original.Palette {
		t.Fatalf("theme persistence: %+v", current)
	}
	foreign, err := f.Service.Get(ctx, otherOwner())
	testkit.RequireNoError(t, err)
	if foreign.UITheme != DefaultUITheme {
		t.Fatalf("theme leaked across owners: %+v", foreign)
	}
	legacy := current
	legacy.UITheme = ""
	legacy.Palette = Rose
	current, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("legacy-ui-theme", current.Version), Settings: legacy})
	testkit.RequireNoError(t, err)
	if current.UITheme != "molr.test@1.0.0" || current.Palette != Rose {
		t.Fatalf("legacy overwrite: %+v", current)
	}
	for i, value := range []string{"https://evil.test/", "molr.test", "molr.test@../secret", "molr.test@1.0.0\n"} {
		bad := current
		bad.UITheme = value
		_, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("invalid-ui-theme-"+string(rune('a'+i)), current.Version), Settings: bad})
		wantCode(t, err, core.InvalidArgument)
	}
	after, err := f.Service.Get(ctx, owner())
	testkit.RequireNoError(t, err)
	if after != current {
		t.Fatal("invalid theme changed settings")
	}
	current.UITheme = DefaultUITheme
	current, err = f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation("default-ui-theme", current.Version), Settings: current})
	testkit.RequireNoError(t, err)
	if current.UITheme != DefaultUITheme {
		t.Fatal("could not restore default")
	}
}
