//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/execenv"
	"molr.ai/runtime/internal/platform"
)

func TestDirectEnvironmentRefreshFindsScriptInterpreter(t *testing.T) {
	private, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	executable := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(executable, []byte("#!/usr/bin/env molr-test-interpreter\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "molr-test-interpreter"), []byte("#!/bin/sh\nprintf '%s|%s|%s' \"$CUSTOM\" \"$2\" \"$PATH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	r := New(Options{PrivateRoot: private, NoWorkspace: true, QuotaStore: quotaTestStore(t), AllowFullAccess: true, Authorize: func(_ context.Context, _ core.Execution, command platform.Command) error {
		if command.Executable != executable {
			t.Fatal("authorization target changed")
		}
		return nil
	}})
	defer r.Close()
	c := platform.Command{FullAccess: true, Executable: executable, Args: []string{"literal $(not-a-shell)"}, Timeout: 10 * time.Second, MaxOutputBytes: 4096}
	run := func() (platform.Exit, string) {
		t.Helper()
		p, err := r.Start(t.Context(), testExecution(), c)
		if err != nil {
			t.Fatal(err)
		}
		exit, err := p.Wait(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(p.Stdout())
		if err != nil {
			t.Fatal(err)
		}
		return exit, string(data)
	}
	if exit, _ := run(); exit.Code != 127 {
		t.Fatal("missing interpreter did not fail", exit)
	}
	for _, value := range []string{"first", "second"} {
		data, err := execenv.EncodeUpdate(private, map[string]string{"PATH": tools + ":/usr/bin:/bin", "CUSTOM": value})
		if err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(private, "next-environment")
		if err := os.WriteFile(tmp, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(private, execenv.Filename)); err != nil {
			t.Fatal(err)
		}
		exit, output := run()
		if exit.Code != 0 || output != value+"|literal $(not-a-shell)|"+tools+":/usr/bin:/bin" {
			t.Fatal(exit, output)
		}
	}
	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatal("parent PATH modified")
	}
	if err := os.WriteFile(filepath.Join(private, execenv.Filename), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(t.Context(), testExecution(), c); err == nil || !strings.Contains(err.Error(), "execution environment") {
		t.Fatal("invalid environment did not block launch", err)
	}
}
