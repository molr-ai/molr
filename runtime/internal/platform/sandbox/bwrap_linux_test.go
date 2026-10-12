//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/platform"
)

func TestLinuxBwrapProfileSelection(t *testing.T) {
	r := integrationRunner(t, nil)
	if os.Getenv("MOLR_TEST_CHROMIUM_SANDBOX") == "1" && chromeProfileCandidate(r.options.Bwrap) && r.bwrapProfile != "chrome" {
		t.Fatal("native sandbox acceptance requires the default chrome profile selection")
	}
	if r.options.Bwrap != "/usr/bin/bwrap" && r.bwrapProfile != "" {
		t.Fatal("explicit launcher policy overridden")
	}
	profile := r.bwrapProfile
	c := testCommand()
	dir := t.TempDir()
	c.Mounts = []platform.Mount{{Source: dir, Destination: "/resources/result"}}
	c.Script = "echo once >> /resources/result/attempts; exit 42"
	_, result := executeTest(t, r, c)
	if result.Code != 42 || r.bwrapProfile != profile {
		t.Fatalf("command failure changed policy or exit: %+v", result)
	}
	b, err := os.ReadFile(filepath.Join(dir, "attempts"))
	if err != nil || string(b) != "once\n" {
		t.Fatalf("business command retried: %q %v", b, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.prepareBwrap(ctx, []string{"molr-test-unloaded-profile", ""}); !errors.Is(err, context.Canceled) || r.bwrapProfile != profile {
		t.Fatalf("cancelled preparation changed policy: %v", err)
	}
}

func TestLinuxBwrapProfileFallback(t *testing.T) {
	r := integrationRunner(t, nil)
	if r.options.Bwrap != "/usr/bin/bwrap" {
		t.Skip("explicit launcher owns profile selection")
	}
	// This exercises an actual failed aa-exec, without editing host policies.
	if err := r.prepareBwrap(t.Context(), []string{"molr-test-unloaded-profile", ""}); err != nil {
		t.Fatal(err)
	}
	if r.bwrapProfile != "" {
		t.Fatal("unloaded profile selected")
	}
	c := testCommand()
	c.Script = "/molr/helper sandbox-test-syscalls; test ! -e /etc/shadow; test ! -e /run/docker.sock"
	output, result := executeTest(t, r, c)
	if result.Code != 0 || !strings.Contains(output, "boundaries-ok") {
		t.Fatalf("fallback lost isolation: %+v %q", result, output)
	}
	// If no candidate passes, readiness and commands must both fail closed.
	r.prepareErr = r.prepareBwrap(t.Context(), []string{"molr-test-unloaded-profile"})
	if r.prepareErr == nil {
		t.Fatal("unloaded profile accepted")
	}
	caps, err := r.Capabilities(t.Context())
	if err != nil || caps.Shell {
		t.Fatalf("failed preparation advertised Shell: %+v %v", caps, err)
	}
	dir := t.TempDir()
	c.Mounts = []platform.Mount{{Source: dir, Destination: "/resources/result"}}
	c.Script = "touch /resources/result/should-not-run"
	if _, err := r.Start(t.Context(), testExecution(), c); err == nil {
		t.Fatal("execution allowed after all probes failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "should-not-run")); !os.IsNotExist(err) {
		t.Fatalf("command escaped failed startup: %v", err)
	}
}
