package hostprograms

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"molr.ai/runtime/internal/core"
)

func writeProgram(t *testing.T, dir, command string, mode os.FileMode) string {
	t.Helper()
	name := command
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 99\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestCatalogDiscoveryNeverExecutesAndKeepsInstallations(t *testing.T) {
	first, second, private := t.TempDir(), t.TempDir(), t.TempDir()
	resolvedFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "bin")
		if err := os.Symlink(resolvedFirst, alias); err != nil {
			t.Fatal(err)
		}
		first = alias
	}
	c := New([]string{private})
	c.directories = func() []searchDirectory {
		return []searchDirectory{{".", "path"}, {first, "path"}, {second, "installation"}, {private, "path"}, {first, "path"}}
	}
	for _, command := range []string{"codex", "claude", "opencode", "kimi", "docker"} {
		writeProgram(t, first, command, 0755)
	}
	writeProgram(t, second, "codex", 0755)
	writeProgram(t, private, "claude", 0755)
	marker := filepath.Join(first, "executed")
	p := writeProgram(t, first, "custom-tool", 0755)
	if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := c.List(t.Context(), Request{})
	if err != nil || len(got.Programs) != 6 {
		t.Fatalf("%+v: %v", got, err)
	}
	custom, err := c.List(t.Context(), Request{Command: "custom-tool"})
	if err != nil || len(custom.Programs) != 1 || custom.Programs[0].Executable != p {
		t.Fatalf("%+v: %v", custom, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("discovery executed the program")
	}
	if runtime.GOOS != "windows" {
		writeProgram(t, first, "kubectl", 0644)
		if err := os.Symlink(filepath.Join(private, "claude"), filepath.Join(first, "helm")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(first, "codex"), filepath.Join(first, "terraform")); err != nil {
			t.Fatal(err)
		}
		got, err = c.List(t.Context(), Request{Refresh: true})
		if err != nil || len(got.Programs) != 8 {
			t.Fatalf("%+v: %v", got, err)
		}
		for _, p := range got.Programs {
			if p.Command == "kubectl" && (p.Available || p.Reason != "not_executable") {
				t.Fatal(p)
			}
			if p.Command == "terraform" && (p.ResolvedPath != filepath.Join(resolvedFirst, "codex") || p.Executable != filepath.Join(first, "terraform")) {
				t.Fatal(p)
			}
		}
	}
}
func TestCatalogCacheIdleRefreshAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := writeProgram(t, dir, "codex", 0755)
		c := New(nil)
		scans := 0
		c.directories = func() []searchDirectory { scans++; return []searchDirectory{{dir, "path"}} }
		first, err := c.List(t.Context(), Request{})
		if err != nil || len(first.Programs) != 1 {
			t.Fatal(first, err)
		}
		first.Programs[0].Executable = "must-not-poison-cache"
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				got, err := c.List(t.Context(), Request{})
				if err != nil || len(got.Programs) != 1 || got.Programs[0].Executable != path {
					t.Error(got, err)
				}
			})
		}
		wg.Wait()
		if scans != 1 {
			t.Fatal("duplicate scans", scans)
		}
		// Virtual time passes with no UI requests: expiry must not schedule a scan.
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if scans != 1 {
			t.Fatal("idle scan")
		}
		got, err := c.List(t.Context(), Request{})
		if err != nil || len(got.Programs) != 0 || scans != 2 {
			t.Fatal(got, err, scans)
		}
		writeProgram(t, dir, "kimi", 0755)
		got, err = c.List(t.Context(), Request{Refresh: true})
		if err != nil || len(got.Programs) != 1 || scans != 3 {
			t.Fatal(got, err, scans)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := c.List(ctx, Request{Refresh: true}); err != context.Canceled || scans != 3 {
			t.Fatal("canceled scan", err, scans)
		}
		for _, command := range []string{"../codex", "/usr/bin/codex", "codex;touch x", "a/b", "a\\b"} {
			if _, err := c.List(t.Context(), Request{Command: command}); core.CodeOf(err) != core.InvalidArgument {
				t.Fatal(command, err)
			}
		}

	})
}

func TestCatalogSystemPathsAndDescriptions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "."+string(os.PathListSeparator)+dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	program := writeProgram(t, dir, "codex", 0755)
	c := New(nil)
	got, err := c.List(t.Context(), Request{Command: "codex"})
	if err != nil || len(got.Programs) == 0 || got.Programs[0].Executable != program {
		t.Fatal(got, err)
	}
	for _, d := range systemDirectories() {
		if !filepath.IsAbs(d.path) {
			t.Fatal("relative PATH accepted", d)
		}
	}
	for input, want := range map[string]string{"/usr/bin/claude": "Claude Code", `C:\Tools\codex.exe`: "Codex", "/opt/kimi": "Kimi Code", "/bin/custom": "custom"} {
		name, _ := Describe(input)
		if name != want {
			t.Fatal(input, name)
		}
	}
}
