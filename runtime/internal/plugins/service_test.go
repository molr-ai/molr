package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func fixture(t *testing.T) (*Service, core.Principal, Package) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	s := New(db, Options{Authorize: func(context.Context, core.Principal, bool) error { return nil }, AuthorizeSettings: func(context.Context, core.Principal) error { return nil }, CheckAssistant: func(_ context.Context, p core.Principal, id core.ID) error {
		if p.Kind == core.ActorAssistant && p.SubjectID != id {
			return core.NewError(core.Forbidden, "wrong assistant")
		}
		return nil
	}, ReadPlans: func(context.Context, core.Principal) (any, error) { return []string{"plan"}, nil }})
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	pkg := Package{Manifest: Manifest{ManifestVersion: 1, PluginAPI: 3, ID: "demo.test", Name: "Test", Version: "1.0.0", Contributes: Contributions{Pages: []Page{{ID: "board", Title: "Board", Entry: "ui.html", Capabilities: []string{"plans.read"}}}}}, Files: map[string]string{"ui.html": "<p>test</p>"}}
	return s, p, pkg
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func activate(t *testing.T, s *Service, p core.Principal, v Installation) Installation {
	t.Helper()
	v, err := s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true, Grants: Requested(v.Manifest)})
	must(t, err)
	return v
}
func execution() core.Execution {
	return core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant", OperationID: "operation"}
}
func TestInstallIsolationImmutableUpgradeAndRemoval(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	list, err := s.List(t.Context(), p)
	must(t, err)
	if len(list) != 1 || list[0].Enabled {
		t.Fatal(list)
	}
	other := p
	other.OwnerID = "other"
	other.SubjectID = "other"
	list, err = s.List(t.Context(), other)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("owner leak")
	}
	v = activate(t, s, p, v)
	q := UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "board", Method: "plans.list"}
	_, err = s.UI(t.Context(), p, q)
	must(t, err)
	pkg.Files["ui.html"] = "mutated"
	if _, err = s.Install(t.Context(), p, pkg); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	pkg.Manifest.Version = "2.0.0"
	staged, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	if staged.Digest != v.Digest {
		t.Fatal("import switched version")
	}
	updated, err := s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: staged.Version, Enabled: true, Release: "2.0.0", Grants: []string{"plans.read"}})
	must(t, err)
	if _, err = s.UI(t.Context(), p, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("old UI accepted", err)
	}
	must(t, s.Remove(t.Context(), p, updated.ID, updated.Version))
	list, err = s.List(t.Context(), p)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("uninstalled plugin remains available")
	}
}
func TestManifestAndBridgeFailClosed(t *testing.T) {
	s, p, pkg := fixture(t)
	bad := pkg
	bad.Manifest.PluginAPI = 2
	if bad.Validate() == nil {
		t.Fatal("old API accepted")
	}
	pkg.Files["../escape"] = "bad"
	if pkg.Validate() == nil {
		t.Fatal("traversal accepted")
	}
	delete(pkg.Files, "../escape")
	bad = pkg
	bad.Manifest.Contributes.Tools = []Tool{{ID: "run", Description: "run", Execution: ToolExecution{Type: "shell"}, Capabilities: []string{"compute.execute"}}}
	if bad.Validate() == nil {
		t.Fatal("dedicated shell accepted")
	}
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true})
	must(t, err)
	for _, method := range []string{"document", "plans.list", "auth.sessions"} {
		if _, err = s.UI(t.Context(), p, UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "board", Method: method}); core.CodeOf(err) != core.Forbidden {
			t.Fatal(method, err)
		}
	}
	if _, err = s.Install(t.Context(), execution().Principal, pkg); core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant imported catalog package", err)
	}
}
func archive(t *testing.T, p Package) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	m, _ := json.Marshal(p.Manifest)
	w, err := z.Create("plugin.json")
	must(t, err)
	_, err = w.Write(m)
	must(t, err)
	for name, body := range p.Files {
		w, err = z.Create(name)
		must(t, err)
		_, err = w.Write([]byte(body))
		must(t, err)
	}
	must(t, z.Close())
	return buf.Bytes()
}
func TestArchiveRejectsTraversalDuplicateLinksAndExpansion(t *testing.T) {
	_, _, pkg := fixture(t)
	got, err := ReadArchive(archive(t, pkg))
	must(t, err)
	if got.Digest() != pkg.Digest() {
		t.Fatal("ZIP changed package digest")
	}
	for _, name := range []string{"../escape", "/absolute", "a/../b", "a\\b", "plugin.json"} {
		pkg.Files[name] = "bad"
		if _, err = ReadArchive(archive(t, pkg)); err == nil {
			t.Fatal("unsafe ZIP accepted", name)
		}
		delete(pkg.Files, name)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0777)
	w, err := z.CreateHeader(h)
	must(t, err)
	_, err = w.Write([]byte("/tmp/target"))
	must(t, err)
	must(t, z.Close())
	if _, err = ReadArchive(buf.Bytes()); err == nil {
		t.Fatal("symlink accepted")
	}
	pkg.Files["big"] = strings.Repeat("x", (1<<20)+1)
	if _, err = ReadArchive(archive(t, pkg)); err == nil {
		t.Fatal("expanded file bound bypassed")
	}
}

func TestArchiveImportAuthorizationAndRemoteURLBoundaries(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.ImportArchive(t.Context(), p, archive(t, pkg))
	must(t, err)
	if v.Enabled {
		t.Fatal("import executed or enabled plugin")
	}
	for _, address := range []string{"http://example.invalid/p.zip", "https://user:password@example.invalid/p.zip", "https://example.invalid/p.zip#fragment", "file:///tmp/plugin.zip"} {
		if _, err = s.ImportURL(t.Context(), p, address); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal("unsafe source accepted", address, err)
		}
	}
	if _, err = s.ImportArchive(t.Context(), execution().Principal, archive(t, pkg)); core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant imported package", err)
	}
	if _, err = s.ImportURL(t.Context(), execution().Principal, "https://example.invalid/p.zip"); core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant remote import", err)
	}
}

func TestHTTPToolCapabilitiesHonorBindingsAndStoppedAssistants(t *testing.T) {
	s, p, v, _, _ := setupHTTP(t)
	defs, err := s.Definitions(t.Context(), execution().Principal, false)
	must(t, err)
	if len(defs) != 1 || defs[0].Name != ToolName(v, "lookup") {
		t.Fatal(defs)
	}
	for _, test := range []struct {
		id            core.ID
		stopped, want bool
	}{{"assistant", false, true}, {"other", false, false}, {"assistant", true, false}} {
		caps := agent.Capabilities{Tools: []agent.ToolCapability{{Definition: defs[0], Available: true}}}
		if test.stopped {
			caps.Tools[0].Reason = "assistant_stopped"
		}
		must(t, s.Inspect(t.Context(), p, test.id, &caps))
		if caps.Tools[0].Available != test.want || len(caps.Skills.Items) != 0 {
			t.Fatal(test, caps)
		}
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false})
	must(t, err)
	defs, err = s.Definitions(t.Context(), execution().Principal, false)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal("disabled tools still available")
	}
}

func TestPackageRejectsWorkspaceManagementFields(t *testing.T) {
	_, _, pkg := fixture(t)
	raw, _ := json.Marshal(pkg)
	for _, field := range []string{"skills", "credentials", "requirements"} {
		var value map[string]any
		must(t, json.Unmarshal(raw, &value))
		manifest := value["manifest"].(map[string]any)
		target := manifest
		if field == "skills" {
			target = manifest["contributes"].(map[string]any)
		}
		target[field] = []any{}
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, err := z.Create("plugin.json")
		must(t, err)
		manifestJSON, _ := json.Marshal(manifest)
		_, err = w.Write(manifestJSON)
		must(t, err)
		w, err = z.Create("ui.html")
		must(t, err)
		_, err = w.Write([]byte(pkg.Files["ui.html"]))
		must(t, err)
		must(t, z.Close())
		if _, err := ReadArchive(buf.Bytes()); err == nil {
			t.Fatalf("removed field %s accepted", field)
		}
	}
}
