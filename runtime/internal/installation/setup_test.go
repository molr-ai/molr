//go:build linux || darwin

package installation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/installrecord"
)

func fixture(t *testing.T) (*Engine, *installrecord.Record) {
	t.Helper()
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	a, err := currentAccount()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	paths, base, err := layout("molr-test", dir, false, filepath.Join(dir, "program"))
	if err != nil {
		t.Fatal(err)
	}
	r := &installrecord.Record{Mode: "server", Home: dir, Schema: 1, State: "installed", Name: "molr-test", User: a.Name, UID: a.UID, GID: a.GID, OwnerUID: a.UID, ProgramRoot: base, Paths: paths, DataDir: paths.Data, ManagedData: true, Build: installrecord.Build{Version: "0.1.0", Commit: strings.Repeat("a", 40), OS: runtime.GOOS, Arch: runtime.GOARCH}, Updates: installrecord.Updates{Source: "https://example.com/releases", Channel: "stable"}}
	for _, p := range []string{paths.Program, paths.Config, paths.Data, paths.Cache, filepath.Dir(paths.Unit)} {
		if err = os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err = writeRecord(r); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Ctx: context.Background(), UI: &UI{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, Input: strings.NewReader(""), Options: Options{Name: "molr-test", Permissions: "ask"}, Command: func(context.Context, []string, io.Reader, []string, io.Writer, io.Writer) ([]byte, error) {
		t.Fatal("unexpected host command")
		return nil, nil
	}}
	t.Cleanup(func() {
		if e.lock != nil {
			e.lock.Close()
		}
	})
	return e, r
}
func TestOptionsRejectConflicts(t *testing.T) {
	for _, args := range [][]string{{"install"}, {"--interactive", "--non-interactive"}, {"--purge"}, {"--version", "0.2.0"}, {"--directory-permissions", "change"}, {"--name", "molr-default"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestInstalledIdentityUsesExecutableRootAndRejectsCorruptRecord(t *testing.T) {
	e, r := fixture(t)
	e.Root = r.Paths.Program
	e.Options.Command = "status"
	e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
		if runtime.GOOS == "darwin" {
			return nil, fmt.Errorf("Could not find service")
		}
		return []byte("LoadState=not-found\nActiveState=inactive\nMainPID=0\n"), nil
	}
	if err := e.execute(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.File(), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.execute(); err == nil {
		t.Fatal("corrupt installed record treated as package")
	}
}
func TestDryRunDoesNotChangePermissionsOrFiles(t *testing.T) {
	e, r := fixture(t)
	e.Options.DryRun = true
	e.Options.Command = "setup"
	if err := os.Chmod(r.Paths.Program, 0775); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(r.File())
	if err := e.install(&Package{Build: r.Build}, r); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(r.File())
	info, _ := os.Stat(r.Paths.Program)
	if !bytes.Equal(before, after) || info.Mode().Perm() != 0775 {
		t.Fatal("dry run mutated instance")
	}
	if _, err := os.Stat(filepath.Join(r.Paths.Program, ".setup.lock")); !os.IsNotExist(err) {
		t.Fatal("dry run acquired lock")
	}
}
func TestSilentPermissionsRequirePolicy(t *testing.T) {
	e, r := fixture(t)
	os.Chmod(r.Paths.Program, 0775)
	p, err := e.plan(&Package{Build: r.Build}, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.resolvePermissions(p.Choices); err == nil {
		t.Fatal("silent permissions guessed")
	}
	info, _ := os.Stat(r.Paths.Program)
	if info.Mode().Perm() != 0775 {
		t.Fatal("permissions changed before approval")
	}
	e.Options.Permissions = "fix"
	if err = e.resolvePermissions(p.Choices); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Choices {
		if err = applyPermission(c); err != nil {
			t.Fatal(err)
		}
	}
	info, _ = os.Stat(r.Paths.Program)
	if info.Mode().Perm() != 0755 {
		t.Fatal(info.Mode())
	}
}
func TestLocksUseStableInode(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "instance")
	first, err := lockInstance(p)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockInstance(p); err == nil {
		second.Close()
		t.Fatal("concurrent lock accepted")
	}
	info, _ := first.Stat()
	first.Close()
	second, err := lockInstance(p)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	next, _ := second.Stat()
	if !os.SameFile(info, next) {
		t.Fatal("lock inode replaced")
	}
}
func TestWritesRejectSymlinkAndPermissionRace(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0755)
	alias := filepath.Join(dir, "alias")
	os.Symlink(target, alias)
	if err := atomicWrite(filepath.Join(alias, "file"), strings.NewReader("bad"), 0600, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("followed symlink")
	}
	info, _ := os.Stat(target)
	choice := &permissionChoice{path: target, info: info, action: "fix"}
	os.Chmod(target, 0775)
	if err := applyPermission(choice); err == nil {
		t.Fatal("accepted changed permissions")
	}
}
func TestUninstallRetainsDataAndPurgesOnlyManagedFiles(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "purge"}[purge], func(t *testing.T) {
			e, r := fixture(t)
			e.Options.Purge = purge
			e.Options.Permissions = "fix"
			e.Options.Yes = true
			e.UI.Yes = true
			e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _ io.Writer, _ io.Writer) ([]byte, error) {
				if runtime.GOOS == "darwin" && slicesContains(args, "print") {
					return nil, fmt.Errorf("Could not find service")
				}
				if strings.Contains(strings.Join(args, " "), " show ") {
					return []byte("LoadState=not-found\nActiveState=inactive\nMainPID=0\n"), nil
				}
				return nil, nil
			}
			for _, p := range []string{filepath.Join(r.Paths.Config, "server.json"), filepath.Join(r.Paths.Data, "retained"), filepath.Join(r.Paths.Program, "unknown")} {
				os.WriteFile(p, []byte("{}"), 0600)
			}
			if err := e.uninstall(r); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(r.Paths.Program, "unknown")); err != nil {
				t.Fatal("unknown file removed")
			}
			_, err := os.Stat(r.Paths.Data)
			if purge != os.IsNotExist(err) {
				t.Fatal("data retention mismatch", err)
			}
			if !purge {
				record, err := readRecord(r.File(), true)
				if err != nil || record.State != "uninstalled" {
					t.Fatal(record, err)
				}
			}
		})
	}
}
func TestUninstallRefusesWorkspaceInsideRemovalTree(t *testing.T) {
	e, r := fixture(t)
	e.Options.Purge = true
	e.Options.Yes = true
	data, _ := json.Marshal(map[string]any{"sandbox": map[string]any{"work_root": filepath.Join(r.Paths.Cache, "workspace")}})
	os.WriteFile(filepath.Join(r.Paths.Config, "server.json"), data, 0600)
	if err := e.uninstall(r); err == nil {
		t.Fatal("workspace would be removed")
	}
}
func TestDownloadLimitsAndRedirects(t *testing.T) {
	e, _ := fixture(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "file:///etc/passwd", 302)
			return
		}
		io.WriteString(w, "too large")
	}))
	defer s.Close()
	for _, path := range []string{"/", "/redirect"} {
		if err := e.fetch(s.URL+path, io.Discard, 4, false); err == nil {
			t.Fatal("accepted", path)
		}
	}
	for _, raw := range []string{"http://example.com/releases", "https://user:secret@example.com/", "file:///tmp/release", "https://example.com/#fragment"} {
		if validateURL(raw) == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestArchiveRejectsUnsafeMembersBeforeExecution(t *testing.T) {
	for _, header := range []tar.Header{{Name: "../escape", Mode: 0755, Typeflag: tar.TypeReg}, {Name: "molr/bin/molr", Typeflag: tar.TypeSymlink, Linkname: "/tmp/escape"}, {Name: "molr/bin/molr", Typeflag: tar.TypeLink, Linkname: "/tmp/escape"}, {Name: "molr/bin/molr", Typeflag: tar.TypeReg, Size: 1 << 31}} {
		dir := t.TempDir()
		dir, _ = filepath.EvalSymlinks(dir)
		file, err := os.Create(filepath.Join(dir, "package.gz"))
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(file)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&header)
		tw.Close()
		gz.Close()
		file.Close()
		if _, err = extractPackage(file.Name(), dir); err == nil {
			t.Fatal("unsafe archive accepted", header)
		}
	}
}
func TestUpdatePreviewCanSelectStableAndDryRunDoesNotExecute(t *testing.T) {
	e, r := fixture(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(releaseManifest{Schema: 1, Version: "0.2.0", Commit: strings.Repeat("b", 40), Channel: "stable", Platforms: []releaseAsset{{OS: runtime.GOOS, Arch: runtime.GOARCH, File: "fixture.tar.gz", URL: "https://example.com/fixture.tar.gz", SHA256: strings.Repeat("a", 64)}}})
	}))
	defer s.Close()
	r.Updates = installrecord.Updates{Source: s.URL, Channel: "preview"}
	e.Options.DryRun = true
	if err := e.update(r); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateVerifiesArchiveBeforeHandingOffToNewSetup(t *testing.T) {
	e, r := fixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		// Match release packaging: Go only signs test binaries automatically on arm64.
		// Sign a private copy so the running test executable is never modified.
		fixtureBinary := filepath.Join(t.TempDir(), "molr-setup")
		if err = os.WriteFile(fixtureBinary, binary, 0755); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", fixtureBinary).CombinedOutput(); err != nil {
			t.Fatalf("sign fixture: %v: %s", err, output)
		}
		binary, err = os.ReadFile(fixtureBinary)
		if err != nil {
			t.Fatal(err)
		}
	}
	build := r.Build
	build.Version = "0.2.0"
	build.Commit = strings.Repeat("b", 40)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	metadata, _ := json.Marshal(build)
	for name, data := range map[string][]byte{"LICENSE": []byte("fixture"), "THIRD_PARTY_NOTICES.md": []byte("fixture"), "release.json": metadata, "bin/molr": binary, "bin/molr-setup": binary} {
		if err = tw.WriteHeader(&tar.Header{Name: "molr/" + name, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(archive.Bytes())
	valid := true
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/package.tar.gz" {
			w.Write(archive.Bytes())
			return
		}
		digest := hex.EncodeToString(sum[:])
		if !valid {
			digest = strings.Repeat("0", 64)
		}
		json.NewEncoder(w).Encode(releaseManifest{Schema: 1, Version: build.Version, Commit: build.Commit, Channel: "stable", Platforms: []releaseAsset{{OS: runtime.GOOS, Arch: runtime.GOARCH, File: "package.tar.gz", URL: server.URL + "/package.tar.gz", SHA256: digest}}})
	}))
	defer server.Close()
	r.Updates.Source = server.URL
	if err = writeRecord(r); err != nil {
		t.Fatal(err)
	}
	e.Options.Permissions = "fix"
	e.Options.NonInteractive = true
	called := false
	e.handoff = func(_ context.Context, args []string, lock *os.File) error {
		if lock == nil {
			t.Fatal("missing lock handoff")
		}
		called = true
		if filepath.Base(args[0]) != "molr-setup" || !strings.Contains(strings.Join(args, " "), "--record "+r.File()) {
			t.Fatal(args)
		}
		if err := checkExecutable(args[0], runtime.GOARCH); err != nil {
			t.Fatal(err)
		}
		return nil
	}
	if err = e.update(r); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("new package installer not invoked")
	}
	valid = false
	called = false
	if err = e.update(r); err == nil || called {
		t.Fatal("unverified package executed", err)
	}
}
func TestAlreadyCurrentCanPersistSubscription(t *testing.T) {
	e, r := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(releaseManifest{Schema: 1, Version: r.Build.Version, Commit: r.Build.Commit, Channel: "stable"})
	}))
	defer server.Close()
	e.Options.Source = server.URL
	e.Options.Permissions = "fix"
	if err := e.update(r); err != nil {
		t.Fatal(err)
	}
	got, err := readRecord(r.File(), true)
	if err != nil || got.Updates.Source != server.URL {
		t.Fatal(got, err)
	}
}
func TestCommandHelpAndVersionWithoutPackage(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version", "--json"}} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("missing output")
		}
	}
	if !strings.Contains((&VerificationPending{}).Error(), "verification is pending") {
		t.Fatal("missing result distinction")
	}
}

func slicesContains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}

func TestRecoveryRestoresOnlyManagedInstallationFiles(t *testing.T) {
	_, r := fixture(t)
	binary := filepath.Join(r.Paths.Program, "bin/molr")
	needDir := filepath.Dir(binary)
	if err := os.MkdirAll(needDir, 0755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(r.Paths.Config, "server.json")
	for _, p := range []string{binary, config, filepath.Join(r.DataDir, "user-data")} {
		if err := os.WriteFile(p, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(binary, 0755); err != nil {
		t.Fatal(err)
	}
	backup, files, err := snapshotInstallation(r)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(backup)
	for _, p := range []string{binary, config, filepath.Join(r.DataDir, "user-data")} {
		if err := os.WriteFile(p, []byte("after"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	added := filepath.Join(r.Paths.Program, "release.json")
	os.WriteFile(added, []byte("new release"), 0600)
	if err := restoreInstallation(r, files); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{binary, config} {
		b, _ := os.ReadFile(p)
		if string(b) != "before" {
			t.Fatal("managed file not restored")
		}
	}
	info, _ := os.Stat(binary)
	if info.Mode().Perm() != 0755 {
		t.Fatal("binary mode not restored")
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatal("new managed file retained")
	}
	b, _ := os.ReadFile(filepath.Join(r.DataDir, "user-data"))
	if string(b) != "after" {
		t.Fatal("user data rolled back")
	}
}
