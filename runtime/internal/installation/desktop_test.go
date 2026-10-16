//go:build linux || darwin

package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopOptionsRejectUnsafeModes(t *testing.T) {
	base := []string{"desktop", "status", "--json", "--non-interactive", "--name", "molr-desktop-fixture"}
	if _, err := parseOptions(base, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--user", "other"}, {"--record", "/tmp/other"}, {"--config", "/tmp/other"}, {"--purge"}, {"--no-start"}, {"--allow-downgrade"}, {"--name", "molr"}} {
		if _, err := parseOptions(append(append([]string{}, base...), extra...), io.Discard); err == nil {
			t.Fatalf("accepted %v", extra)
		}
	}
	if _, err := parseOptions([]string{"desktop", "install", "--name", "molr-desktop-fixture"}, io.Discard); err == nil {
		t.Fatal("accepted interactive desktop request")
	}
}

func TestDesktopDoesNotTakeOverCLIInstallation(t *testing.T) {
	e, r := fixture(t)
	e.Options.ProgramRoot = r.ProgramRoot
	if _, err := e.desktopRecord(); err == nil {
		t.Fatal("accepted a CLI-owned instance")
	}
	r.Manager = "desktop"
	if err := writeRecord(r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.desktopRecord(); err != nil {
		t.Fatal(err)
	}
	e.Root = r.Paths.Program
	e.Options.Command = "update"
	if err := e.execute(); err == nil || !strings.Contains(err.Error(), "Molr Desktop") {
		t.Fatalf("web/CLI update was not rejected: %v", err)
	}
	if err := e.update(r); err == nil || !strings.Contains(err.Error(), "Molr Desktop") {
		t.Fatalf("interactive update was not rejected: %v", err)
	}
}

func TestDesktopStatusIsJSONAndContainsNoCredentials(t *testing.T) {
	e, r := fixture(t)
	r.Manager = "desktop"
	if err := writeRecord(r); err != nil {
		t.Fatal(err)
	}
	cfg := `{"http":{"public_url":"http://127.0.0.1:8787"},"private_fixture":"not-in-output"}`
	if err := os.WriteFile(filepath.Join(r.Paths.Config, "server.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	e.Options.Command, e.Options.TaskAction, e.Options.ProgramRoot = "desktop", "status", r.ProgramRoot
	var output bytes.Buffer
	e.machineOut = &output
	e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
		if runtime.GOOS == "darwin" {
			return nil, fmt.Errorf("Could not find service")
		}
		if !strings.Contains(strings.Join(args, " "), "show") {
			t.Fatalf("status mutated service: %v", args)
		}
		return []byte("LoadState=not-found\nActiveState=inactive\nMainPID=0\n"), nil
	}
	if err := e.desktopCommand(); err != nil {
		t.Fatal(err)
	}
	var result DesktopResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.URL != "http://127.0.0.1:8787" || result.LoginURL != "" || strings.Contains(output.String(), "not-in-output") {
		t.Fatalf("invalid result: %s", &output)
	}
}

func TestDesktopNewInstanceUsesLoopbackAndExplicitManager(t *testing.T) {
	e, r := fixture(t)
	e.Options.Command, e.Options.Name, e.Options.Port = "desktop", "molr-desktop-new", 18787
	e.Options.ProgramRoot = r.ProgramRoot
	p, err := e.plan(&Package{Build: r.Build}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := p.Config["http"].(map[string]any)
	if p.Record.Manager != "desktop" || h["tls_mode"] != "local" || h["listen"] != "127.0.0.1:18787" || h["public_url"] != "http://127.0.0.1:18787" || h["allowed_origins"] != nil {
		t.Fatalf("unsafe defaults: %#v", h)
	}
}
