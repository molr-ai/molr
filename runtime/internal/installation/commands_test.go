//go:build linux || darwin

package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func commandFixture(t *testing.T) (*Engine, string) {
	t.Helper()
	e, r := fixture(t)
	bin := filepath.Join(r.Paths.Program, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range commandNames {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'original\\n'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	e.Root = r.Paths.Program
	return e, r.File()
}

func TestCommandLinksFollowUpgradesAndPreserveOtherInstances(t *testing.T) {
	e, record := commandFixture(t)
	r, err := readRecord(record, true)
	if err != nil {
		t.Fatal(err)
	}
	directory, _ := commandDirectory(r)
	t.Setenv("PATH", "/usr/bin:/bin")
	e.installCommands(r)
	e.installCommands(r)
	for _, name := range commandNames {
		link := filepath.Join(directory, name)
		target := filepath.Join(r.Paths.Program, "bin", name)
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Fatalf("link %s: %q %v", name, got, err)
		}
		if err := atomicWrite(target, strings.NewReader("#!/bin/sh\nprintf 'updated\\n'\n"), 0755, r.OwnerUID, r.GID); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(link).CombinedOutput(); err != nil || string(output) != "updated\n" {
			t.Fatalf("updated command: %s %v", output, err)
		}
	}
	other := *r
	other.Paths.Program = filepath.Join(r.ProgramRoot, "other")
	if err := installCommandLinks(&other, directory); err == nil {
		t.Fatal("another instance replaced existing commands")
	}
	if err := removeCommandLinks(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "molr")); err != nil {
		t.Fatal("another instance removed our command", err)
	}
	profile := filepath.Join(r.Home, ".bashrc")
	data, err := os.ReadFile(profile)
	if err != nil || strings.Count(string(data), pathMarker) != 1 {
		t.Fatalf("PATH setup not idempotent: %s %v", data, err)
	}
	cmd := exec.Command("/bin/sh", "-c", `. "$1"; . "$1"; command -v molr; printf '%s\n' "$PATH"`, "sh", profile)
	cmd.Env = []string{"HOME=" + r.Home, "PATH=/usr/bin:/bin"}
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != filepath.Join(directory, "molr")+"\n"+directory+":/usr/bin:/bin\n" {
		t.Fatalf("fresh shell command lookup: %s %v", output, err)
	}
	if err := removeCommandLinks(r); err != nil {
		t.Fatal(err)
	}
	for _, name := range commandNames {
		if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatal("command not removed", err)
		}
	}
	if after, _ := os.ReadFile(profile); !bytes.Equal(data, after) {
		t.Fatal("shared PATH configuration removed")
	}
}

func TestCommandLinksNeverReplaceExistingEntries(t *testing.T) {
	for _, kind := range []string{"file", "directory", "other-link", "dangling-link"} {
		t.Run(kind, func(t *testing.T) {
			_, record := commandFixture(t)
			r, _ := readRecord(record, true)
			directory, _ := commandDirectory(r)
			os.MkdirAll(directory, 0755)
			conflict := filepath.Join(directory, "molr-setup")
			switch kind {
			case "file":
				os.WriteFile(conflict, []byte("user command"), 0755)
			case "directory":
				os.Mkdir(conflict, 0755)
			case "other-link":
				os.Symlink(filepath.Join(r.Paths.Program, "bin/molr"), conflict)
			case "dangling-link":
				os.Symlink(filepath.Join(r.Home, "missing"), conflict)
			}
			before, err := os.Lstat(conflict)
			if err != nil {
				t.Fatal(err)
			}
			if err := installCommandLinks(r, directory); err == nil {
				t.Fatal("conflict accepted")
			}
			if _, err := os.Lstat(filepath.Join(directory, "molr")); !os.IsNotExist(err) {
				t.Fatal("partial command pair created", err)
			}
			if err := removeCommandLinks(r); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(conflict)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("unrelated entry changed", err)
			}
		})
	}
}

func TestCommandPathsRejectSymlinkedParents(t *testing.T) {
	_, r := fixture(t)
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Symlink(outside, filepath.Join(r.Home, ".local")); err != nil {
		t.Fatal(err)
	}
	directory, _ := commandDirectory(r)
	if err := installCommandLinks(r, directory); err == nil {
		t.Fatal("followed symlinked command directory")
	}
	if err := removeCommandLinks(r); err == nil {
		t.Fatal("followed symlink during cleanup")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("modified outside directory")
	}
	r.System = true
	if directory, err := commandDirectory(r); err != nil || directory != "/usr/local/bin" {
		t.Fatal(directory, err)
	}
}

func TestShellProfilesRespectLoginSelectionAndCustomDirectories(t *testing.T) {
	_, r := fixture(t)
	home := r.Home
	for _, name := range []string{".bash_login", ".bash_profile"} {
		os.WriteFile(filepath.Join(home, name), []byte("# user settings\n"), 0600)
		paths, _, err := shellProfiles(home, "/bin/bash", "", "")
		if err != nil || paths[0] != filepath.Join(home, name) || paths[1] != filepath.Join(home, ".bashrc") {
			t.Fatal(paths, err)
		}
	}
	zdir := filepath.Join(home, "zsh settings")
	paths, block, err := shellProfiles(home, "/bin/zsh", zdir, "")
	if err != nil || len(paths) != 2 || paths[0] != filepath.Join(zdir, ".zprofile") || paths[1] != filepath.Join(zdir, ".zshrc") || block != shellPath {
		t.Fatal(paths, err)
	}
	paths, block, err = shellProfiles(home, "/usr/bin/fish", "", filepath.Join(home, "config"))
	if err != nil || paths[0] != filepath.Join(home, "config/fish/config.fish") || block != fishPath {
		t.Fatal(paths, err)
	}
	if _, _, err := shellProfiles(home, "/bin/unknown", "", ""); err == nil {
		t.Fatal("unsupported shell silently accepted")
	}
}

func TestShellPathPreservesContentAndRejectsUnsafeProfiles(t *testing.T) {
	_, r := fixture(t)
	path := filepath.Join(r.Home, ".profile")
	original := "# existing settings without trailing newline"
	os.WriteFile(path, []byte(original), 0600)
	for range 2 {
		if err := appendShellPath(path, shellPath, r.OwnerUID); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != original+"\n"+shellPath || info.Mode().Perm() != 0600 {
		t.Fatal("profile content or mode changed")
	}
	alias := filepath.Join(r.Home, ".zshrc")
	os.Symlink(path, alias)
	if err := appendShellPath(alias, shellPath, r.OwnerUID); err == nil {
		t.Fatal("followed profile symlink")
	}
	os.Remove(alias)
	os.Link(path, alias)
	if err := appendShellPath(alias, shellPath, r.OwnerUID); err == nil {
		t.Fatal("modified hard-linked profile")
	}
	os.Remove(alias)
	os.Chmod(path, 0666)
	if err := appendShellPath(path, shellPath, r.OwnerUID); err == nil {
		t.Fatal("modified writable profile")
	}
	os.Chmod(path, 0600)
	os.WriteFile(path, []byte(pathMarker+"\n# user edited this block\n"), 0600)
	if err := appendShellPath(path, shellPath, r.OwnerUID); err == nil {
		t.Fatal("overwrote edited PATH block")
	}
}

func TestCommandSetupDryRunAndExistingPATH(t *testing.T) {
	e, r := fixture(t)
	e.Options.DryRun = true
	e.installCommands(r)
	if _, err := os.Lstat(filepath.Join(r.Home, ".local")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote command directory")
	}
	e.Options.DryRun = false
	directory, _ := commandDirectory(r)
	t.Setenv("PATH", directory+":/usr/bin:/bin")
	e.installCommands(r)
	if _, err := os.Lstat(filepath.Join(r.Home, ".bashrc")); !os.IsNotExist(err) {
		t.Fatal("modified profile with PATH already configured")
	}
}

func TestCommandSymlinkUsesInstalledIdentity(t *testing.T) {
	if os.Getenv("MOLR_TEST_COMMAND_ENTRY") == "1" {
		if err := Run(context.Background(), []string{"status", "--dry-run"}, strings.NewReader(""), os.Stdout, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
	e, record := commandFixture(t)
	r, _ := readRecord(record, true)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(r.Paths.Program, "bin/molr-setup"), exe, 0755, r.UID, r.GID); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", filepath.Join(r.Paths.Program, "bin/molr-setup")).CombinedOutput(); err != nil {
			t.Fatalf("sign fixture: %s %v", output, err)
		}
	}
	directory, _ := commandDirectory(r)
	e.installCommands(r)
	cmd := exec.Command(filepath.Join(directory, "molr-setup"), "-test.run=^TestCommandSymlinkUsesInstalledIdentity$")
	cmd.Env = append(os.Environ(), "MOLR_TEST_COMMAND_ENTRY=1")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Instance: "+r.Name) || !strings.Contains(string(output), "Program: "+r.Paths.Program) {
		t.Fatalf("symlink did not resolve installed identity: %s %v", output, err)
	}
}

func TestInstallationPublishesCommandsOnlyAfterSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			e, r := fixture(t)
			e.Options.NoStart = true
			e.Options.SkipDependencies = true
			exe, _ := os.Executable()
			root := filepath.Join(r.Home, "package")
			os.MkdirAll(filepath.Join(root, "bin"), 0755)
			for _, name := range commandNames {
				path := filepath.Join(root, "bin", name)
				if err := copyFile(path, exe, 0755, r.UID, r.GID); err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS == "darwin" {
					if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", path).CombinedOutput(); err != nil {
						t.Fatalf("sign fixture: %s %v", output, err)
					}
				}
			}
			for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md", "release.json"} {
				if fail && name == "THIRD_PARTY_NOTICES.md" {
					continue
				}
				os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0644)
			}
			os.Remove(r.File())
			e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
				if slicesContains(args, "version") {
					return json.Marshal(r.Build)
				}
				return []byte("systemd 257"), nil
			}
			p := &Plan{Record: r, Package: &Package{Root: root, Build: r.Build}, Config: map[string]any{"data_dir": r.DataDir}}
			err := e.apply(p)
			if (err != nil) != fail {
				t.Fatalf("apply failure=%t: %v", fail, err)
			}
			directory, _ := commandDirectory(r)
			for _, name := range commandNames {
				_, err := os.Lstat(filepath.Join(directory, name))
				if fail != os.IsNotExist(err) {
					t.Fatalf("command publication before success=%t: %v", fail, err)
				}
			}
		})
	}
}
