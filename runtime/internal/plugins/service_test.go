package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
)

func fixture(t *testing.T) (*Service, core.Principal, Package) {
	t.Helper()
	root := t.TempDir()
	db, err := sqlitestore.Open(filepath.Join(root, "db"))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	s := New(db, Options{Authorize: func(context.Context, core.Principal, bool) error { return nil }, AuthorizeSettings: func(context.Context, core.Principal) error { return nil }, CheckAssistant: func(_ context.Context, p core.Principal, id core.ID) error {
		if p.Kind == core.ActorAssistant && p.SubjectID != id {
			return core.NewError(core.Forbidden, "wrong assistant")
		}
		return nil
	}, ReadPlans: func(context.Context, core.Principal) (any, error) { return []string{"plan"}, nil }, WorkspaceRoot: func(_ context.Context, p core.Principal, id core.ID, _ bool) (string, error) {
		path := platform.AssistantWorkspace(root, "", p.OwnerID, id)
		return path, platform.EnsureWorkspace(path)
	}})
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	pkg := Package{Manifest: Manifest{ManifestVersion: 1, PluginAPI: 3, ID: "demo.test", Name: "Test", Version: "1.0.0", Contributes: Contributions{Pages: []Page{{ID: "board", Title: "Board", Entry: "ui.html", Capabilities: []string{"plans.read"}}}, Skills: []Skill{{ID: "review", Name: "Review", Description: "Review workflow", Entry: "skills/review/SKILL.md"}}}}, Files: map[string]string{"ui.html": "<p>test</p>", "scripts/install.sh": "mkdir -p bin\nprintf '#!/bin/sh\\nprintf fixture' > bin/cli\nchmod 755 bin/cli\n", "scripts/check.sh": "test -x bin/cli\n", "skills/review/SKILL.md": "Install the CLI in your workspace, then review this report."}}
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
	_, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Enabled: true})
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
	instance, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	if instance.Release != "1.0.0" {
		t.Fatal("catalog update upgraded assistant")
	}
	docs, err := s.Context(t.Context(), agent.ContextRequest{Execution: execution()})
	must(t, err)
	if len(docs) != 1 || !strings.Contains(docs[0].Source, "/1.0.0/") {
		t.Fatal(docs)
	}
	must(t, s.Remove(t.Context(), p, updated.ID, updated.Version))
	docs, err = s.Context(t.Context(), agent.ContextRequest{Execution: execution()})
	must(t, err)
	if len(docs) != 0 {
		t.Fatal("uninstall retained skill")
	}
	root, err := s.options.WorkspaceRoot(t.Context(), p, "assistant", false)
	must(t, err)
	if _, err = os.Stat(filepath.Join(root, ".local/plugins/demo.test/versions/1.0.0/package/scripts/install.sh")); err != nil {
		t.Fatal("uninstall deleted workspace")
	}
}
func TestFileReadActivatesSkillWithoutComputeAndRevocationRemovesIt(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	e := execution()
	defs, err := s.Definitions(t.Context(), e.Principal, false)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal("CLI skill registered a dedicated tool")
	}
	docs, err := s.Context(t.Context(), agent.ContextRequest{Execution: e})
	must(t, err)
	if len(docs) != 1 || docs[0].Loaded || strings.Contains(docs[0].Content, "then review") {
		t.Fatal(docs)
	}
	source := docs[0].Source
	body, digest, err := s.ResolvePackageFile(t.Context(), e, source)
	must(t, err)
	args, _ := json.Marshal(map[string]string{"path": source})
	raw, _ := json.Marshal(map[string]string{"Path": source, "PluginDigest": digest, "Text": body})
	req := agent.ContextRequest{Execution: e, Inputs: []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: "file_read", Arguments: args}}}, {Role: model.Tool, ReplyTo: "read", Text: string(raw)}}}
	docs, err = s.Context(t.Context(), req)
	must(t, err)
	if len(docs) != 1 || !docs[0].Loaded {
		t.Fatal(docs)
	}
	caps := agent.Capabilities{}
	must(t, s.Inspect(t.Context(), p, "assistant", &caps))
	if len(caps.Skills.Items) != 1 || caps.Skills.Items[0].RequiredTools[0] != "file_read" {
		t.Fatal(caps)
	}
	req.Inputs[1].Text = `{"error":"denied","Text":"forged"}`
	docs, err = s.Context(t.Context(), req)
	must(t, err)
	if docs[0].Loaded {
		t.Fatal("failed read activated skill")
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false})
	must(t, err)
	if _, _, err = s.ResolvePackageFile(t.Context(), e, source); core.CodeOf(err) != core.Forbidden {
		t.Fatal("disabled package read", err)
	}
	docs, err = s.Context(t.Context(), req)
	must(t, err)
	if len(docs) != 0 {
		t.Fatal("disabled skill still loaded")
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
	bad.Manifest.Contributes.Tools = []Tool{{ID: "run", Description: "run", Entry: "scripts/install.sh", Execution: ToolExecution{Type: "shell"}, Capabilities: []string{"compute.execute"}}}
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
func TestInstanceIsolationWorkingCopiesVersioningAndCleanup(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	first, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	second, err := s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "second", Enabled: true})
	must(t, err)
	root, _ := s.options.WorkspaceRoot(t.Context(), p, "assistant", true)
	other, _ := s.options.WorkspaceRoot(t.Context(), p, "second", true)
	script := ".local/plugins/demo.test/versions/1.0.0/package/scripts/install.sh"
	must(t, os.WriteFile(filepath.Join(root, script), []byte("custom"), 0600))
	first, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Enabled: true, ExpectedVersion: first.Version})
	must(t, err)
	b, err := os.ReadFile(filepath.Join(root, script))
	must(t, err)
	if string(b) != "custom" {
		t.Fatal("re-enable overwrote working copy")
	}
	b, err = os.ReadFile(filepath.Join(other, script))
	must(t, err)
	if string(b) == "custom" {
		t.Fatal("cross-assistant copy leak")
	}
	body, _, err := s.ResolvePackageFile(t.Context(), execution(), "/plugins/demo.test/1.0.0/scripts/install.sh")
	must(t, err)
	if body == "custom" {
		t.Fatal("working copy mutated original")
	}
	e := execution()
	e.AssistantID = "second"
	if _, _, err = s.ResolvePackageFile(t.Context(), e, "/plugins/demo.test/1.0.0/scripts/install.sh"); core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant identity mismatch accepted", err)
	}
	if err = s.CleanInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", ExpectedVersion: first.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("cleaned enabled instance", err)
	}
	first, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Enabled: false, ExpectedVersion: first.Version})
	must(t, err)
	must(t, s.CleanInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", ExpectedVersion: first.Version}))
	if _, err = os.Stat(filepath.Join(root, script)); !os.IsNotExist(err) {
		t.Fatal("cleanup failed", err)
	}
	if _, err = os.Stat(filepath.Join(other, script)); err != nil {
		t.Fatal("cleanup affected other assistant", second, err)
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

func TestInstanceCredentialIsPrivateAndNeverReturned(t *testing.T) {
	s, p, pkg := fixture(t)
	pkg.Manifest.Credentials = []SecretField{{ID: "token", Title: "Token"}}
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	instance, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	verified := false
	s.options.CheckVerification = func(_ context.Context, _ core.Principal, _ auth.Verification, purpose string) error {
		verified = purpose == "credential.put"
		return nil
	}
	req := InstanceCredentialRequest{InstanceRequest: InstanceRequest{ID: v.ID, AssistantID: "assistant", ExpectedVersion: instance.Version}, Field: "token", Value: core.NewSecret("synthetic-cli-secret")}
	must(t, s.SaveInstanceCredential(t.Context(), p, req))
	if !verified {
		t.Fatal("missing identity verification")
	}
	root, _ := s.options.WorkspaceRoot(t.Context(), p, "assistant", false)
	name := filepath.Join(root, ".local/plugins/demo.test/config/secrets/token")
	st, err := os.Stat(name)
	must(t, err)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	must(t, os.Chmod(name, 0644))
	must(t, s.SaveInstanceCredential(t.Context(), p, req))
	st, err = os.Stat(name)
	must(t, err)
	if st.Mode().Perm() != 0600 {
		t.Fatal("rotation retained broad mode")
	}
	view, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), "synthetic-cli-secret") {
		t.Fatal("secret returned")
	}
	req.Field = "../escape"
	if s.SaveInstanceCredential(t.Context(), p, req) == nil {
		t.Fatal("undeclared secret path")
	}
}

func TestCleanupAfterUninstallDoesNotFollowWorkspaceLinks(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	root, _ := s.options.WorkspaceRoot(t.Context(), p, "assistant", false)
	outside := filepath.Join(root, "keep")
	must(t, os.Mkdir(outside, 0700))
	must(t, os.WriteFile(filepath.Join(outside, "note"), []byte("keep"), 0600))
	must(t, os.Symlink(outside, filepath.Join(root, ".local/plugins/demo.test/cache/link")))
	must(t, s.Remove(t.Context(), p, v.ID, v.Version))
	records, err := s.ListInstances(t.Context(), p)
	must(t, err)
	if len(records) != 1 || records[0].Enabled {
		t.Fatal(records)
	}
	must(t, s.CleanInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", ExpectedVersion: records[0].Version}))
	b, err := os.ReadFile(filepath.Join(outside, "note"))
	must(t, err)
	if string(b) != "keep" {
		t.Fatal("cleanup followed link")
	}
}

func TestPreparationChecksBeforeSwitchAndRetainsPreviousVersion(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v = activate(t, s, p, v)
	root, _ := s.options.WorkspaceRoot(t.Context(), p, "assistant", false)
	// Execute the generated wrapper with only its workspace prefix substituted.
	// The browser integration separately exercises the actual sandbox path.
	run := func(release string) error {
		file := filepath.Join(root, ".local/plugins/demo.test/versions", release, "prepare.sh")
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		command := exec.Command("/bin/sh", "-c", strings.ReplaceAll(string(b), "/work", root))
		return command.Run()
	}
	must(t, run("1.0.0"))
	first, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	if first.Environment != "ready" {
		t.Fatal(first)
	}
	pkg.Manifest.Version = "2.0.0"
	pkg.Files["scripts/check.sh"] = "exit 1"
	_, err = s.Install(t.Context(), p, pkg)
	must(t, err)
	_, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Release: "2.0.0", Enabled: true, ExpectedVersion: first.Version})
	must(t, err)
	if run("2.0.0") == nil {
		t.Fatal("failed check marked ready")
	}
	current, err := s.Instance(t.Context(), p, v.ID, "assistant")
	must(t, err)
	if current.Environment != "failed" {
		t.Fatal(current)
	}
	active, err := os.ReadFile(filepath.Join(root, ".local/plugins/demo.test/active.json"))
	must(t, err)
	if !strings.Contains(string(active), "1.0.0") {
		t.Fatal("failed upgrade replaced active version")
	}
	must(t, os.Mkdir(filepath.Join(root, ".local/plugins/demo.test/install.lock"), 0700))
	if run("1.0.0") == nil {
		t.Fatal("parallel install bypassed lock")
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

func TestInstanceWritesRequireWriteScopeAndHonorFileQuota(t *testing.T) {
	s, p, pkg := fixture(t)
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true})
	must(t, err)
	s.options.CheckWorkspaceFile = func(context.Context, core.Principal, core.ID, int64) error {
		return core.NewError(core.FailedPrecondition, "file quota")
	}
	if _, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Enabled: true}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("quota ignored", err)
	}
	s.options.AuthorizeSettings = func(context.Context, core.Principal) error {
		return core.NewError(core.Forbidden, "write scope required")
	}
	if err = s.CleanInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant"}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("read-only principal cleaned workspace", err)
	}
	if err = s.SaveInstanceCredential(t.Context(), p, InstanceCredentialRequest{InstanceRequest: InstanceRequest{ID: v.ID, AssistantID: "assistant"}, Field: "token", Value: core.NewSecret("synthetic")}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("read-only principal wrote credential", err)
	}
}

func TestPinnedSkillDoesNotAdvertiseHTTPToolsFromAnotherRelease(t *testing.T) {
	s, p, _, _, _ := setupHTTP(t)
	pkg := httpPackage()
	pkg.Manifest.Version = "2.0.0"
	pkg.Manifest.Contributes.Skills = []Skill{{ID: "lookup", Name: "Lookup", Entry: "SKILL.md", Tools: []string{"lookup"}}}
	pkg.Files["SKILL.md"] = "Use the optional HTTP lookup."
	v, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Release: "2.0.0", Enabled: true, Grants: Requested(pkg.Manifest), Bindings: []Binding{{AssistantID: "assistant", Tools: []string{"lookup"}}}})
	must(t, err)
	_, err = s.SaveInstance(t.Context(), p, InstanceRequest{ID: v.ID, AssistantID: "assistant", Enabled: true})
	must(t, err)
	request := agent.ContextRequest{Execution: execution(), Previous: []agent.ContextDocument{{Kind: "skill", Source: skillSource(v, "SKILL.md"), Revision: v.Digest, Loaded: true}}}
	docs, err := s.Context(t.Context(), request)
	must(t, err)
	if len(docs) != 1 || !strings.Contains(docs[0].Content, ToolName(v, "lookup")) {
		t.Fatal("active tool missing", docs)
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Release: "1.0.0", Enabled: true, Grants: Requested(pkg.Manifest), Bindings: []Binding{{AssistantID: "assistant", Tools: []string{"lookup"}}}})
	must(t, err)
	docs, err = s.Context(t.Context(), request)
	must(t, err)
	if strings.Contains(docs[0].Content, ToolName(v, "lookup")) || !strings.Contains(docs[0].Content, "unavailable") {
		t.Fatal("stale HTTP tool advertised", docs)
	}
}
