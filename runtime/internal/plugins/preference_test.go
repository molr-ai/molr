package plugins

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestPersonalPluginEnablementAndGroupAccess(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	denied := false
	s.options.CheckPlugin = func(_ context.Context, p core.Principal, _ string) error {
		if denied && p.OwnerID == "member" {
			return core.NewError(core.Forbidden, "group denied")
		}
		return nil
	}
	member := p
	member.OwnerID = "member"
	member.SubjectID = "member"
	rows, err := s.List(t.Context(), member)
	must(t, err)
	if len(rows) != 1 || !rows[0].UserEnabled || rows[0].CatalogID != Key(v.ID) {
		t.Fatal(rows)
	}
	pref, err := s.SavePreference(t.Context(), member, PreferenceRequest{ID: v.ID, Digest: v.Digest, Enabled: false})
	must(t, err)
	rows, err = s.List(t.Context(), member)
	must(t, err)
	if len(rows) != 1 || rows[0].UserEnabled {
		t.Fatal("disabled plugin must remain discoverable")
	}
	q := UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "settings", Method: "document"}
	if _, err = s.UI(t.Context(), member, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = s.Settings(t.Context(), member, v.ID, v.Digest); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, _, err = s.ConnectionAccess(t.Context(), member, a.ID); err == nil {
		t.Fatal("disabled connection access")
	}
	defs, err := s.Definitions(t.Context(), member, false)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal(defs)
	}
	if _, err = s.UI(t.Context(), p, q); err != nil {
		t.Fatal("other account affected", err)
	}
	if _, err = s.SavePreference(t.Context(), member, PreferenceRequest{ID: v.ID, Digest: v.Digest, Enabled: true}); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	_, err = s.SavePreference(t.Context(), member, PreferenceRequest{ID: v.ID, Digest: v.Digest, Enabled: true, ExpectedVersion: pref.Version})
	must(t, err)
	_, err = s.UI(t.Context(), member, q)
	must(t, err)
	denied = true
	rows, err = s.List(t.Context(), member)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("unauthorized plugin visible")
	}
	if _, err = s.UI(t.Context(), member, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = s.Connections(t.Context(), member, v.ID, v.Digest); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = s.SavePreference(t.Context(), member, PreferenceRequest{ID: v.ID, Digest: v.Digest, Enabled: true, ExpectedVersion: 2}); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	call := tools.Call{Name: ToolName(v, "lookup"), Execution: execution(), Arguments: json.RawMessage(`{"query":"test"}`)}
	call.Execution.Principal.OwnerID = "member"
	if err = s.ValidateCall(t.Context(), call); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
}

func TestOptionalUserSettingsAndSharedAdminSettings(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	pkg := httpPackage()
	pkg.Manifest.Version = "2.0.0"
	pkg.Manifest.AdminSettings = pkg.Manifest.Settings
	pkg.Manifest.Settings = nil
	pkg.Manifest.AdminSettingsPage = &Page{ID: "admin", Title: "Admin", Entry: "ui.html", Capabilities: []string{"settings.read", "settings.write"}}
	pkg.Manifest.Contributes.Tools[0].Execution.SettingsScope = "admin"
	for i := range pkg.Manifest.Providers {
		pkg.Manifest.Providers[i].Test.SettingsScope = "admin"
	}
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Release: "2.0.0", Enabled: true, Grants: Requested(pkg.Manifest), Bindings: []Binding{{AssistantID: "assistant", Tools: []string{"lookup"}}}})
	must(t, err)
	a, err = s.SaveConnection(t.Context(), p, SaveConnection{PluginID: v.ID, Digest: v.Digest, ID: a.ID, ProviderID: a.ProviderID, Name: a.Name, Enabled: true, CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10, Mutation: core.Mutation{IdempotencyKey: "upgrade-a", ExpectedVersion: a.Version}})
	must(t, err)
	if _, err = s.Settings(t.Context(), p, v.ID, v.Digest); core.CodeOf(err) != core.NotFound {
		t.Fatal(err)
	}
	values, err := s.Settings(t.Context(), p, v.ID, v.Digest, "admin")
	must(t, err)
	values.Values["backend"] = string(a.ID)
	values, err = s.SaveSettings(t.Context(), p, SettingsRequest{ID: v.ID, Digest: v.Digest, Scope: "admin", ExpectedVersion: values.Version, Values: values.Values})
	must(t, err)
	member := p
	member.OwnerID = "member"
	member.SubjectID = "member"
	if _, err = s.Settings(t.Context(), member, v.ID, v.Digest, "admin"); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = s.SaveSettings(t.Context(), member, SettingsRequest{ID: v.ID, Digest: v.Digest, Scope: "admin", Values: map[string]any{}}); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	q := UIRequest{ID: v.ID, Digest: v.Digest, Kind: "admin-settings", Contribution: "admin", Method: "settings.get"}
	if _, err = s.UI(t.Context(), member, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = s.UI(t.Context(), p, q); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(t.Context(), member)
	must(t, err)
	if len(list) != 1 || list[0].Manifest.AdminSettings != nil || list[0].Manifest.AdminSettingsPage != nil {
		t.Fatal("admin schema leaked")
	}
	call := tools.Call{Name: ToolName(v, "lookup"), Execution: execution(), Arguments: json.RawMessage(`{"query":"shared"}`)}
	call.Execution.Principal.OwnerID = "member"
	_, err = s.PrepareCall(t.Context(), call)
	must(t, err)
	result, err := s.Execute(t.Context(), call)
	must(t, err)
	if len(result) == 0 {
		t.Fatal("missing shared result")
	}
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false, Grants: v.Grants})
	must(t, err)
	_, err = s.Settings(t.Context(), p, v.ID, v.Digest, "admin")
	must(t, err)
}
