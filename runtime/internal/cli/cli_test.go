package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/runtimeclient"
)

func isolateUserConfig(t *testing.T) string {
	t.Helper()
	// os.UserConfigDir ignores XDG_CONFIG_HOME on macOS and Windows.
	key := "XDG_CONFIG_HOME"
	if runtime.GOOS == "darwin" {
		key = "HOME"
	} else if runtime.GOOS == "windows" {
		key = "APPDATA"
	}
	t.Setenv(key, t.TempDir())
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunConnectionOptionsAndTerminal(t *testing.T) {
	t.Setenv("MOLR_SERVER", "")
	isolateUserConfig(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "interactive terminal"},
		{[]string{"--demo", "extra"}, "unexpected arguments"},
		{[]string{"--server", "https://example.com"}, "interactive terminal"},
		{[]string{"--demo"}, "interactive terminal"},
	} {
		var output, diagnostics bytes.Buffer
		err := Run(context.Background(), tc.args, strings.NewReader(""), &output, &diagnostics)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if output.Len() != 0 {
			t.Fatal("failed command wrote terminal output")
		}
	}
	var help bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, nil, &help, &help); err != nil || !strings.Contains(help.String(), "-demo") {
		t.Fatalf("help: %s, %v", help.String(), err)
	}
}

func TestRunDefaultConnectionsPreserveDesktopProfile(t *testing.T) {
	t.Setenv("MOLR_SERVER", "")
	dir := isolateUserConfig(t)
	desktop := []byte(`{"connections":[]}`)
	// These names alias on the default macOS/Windows filesystems. Seed both
	// spellings so Linux also catches accidental writes to the desktop profile.
	for _, name := range []string{"Molr", "molr"} {
		profile := filepath.Join(dir, name)
		if err := os.MkdirAll(profile, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "connections.json"), desktop, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output, diagnostics bytes.Buffer
	err := Run(t.Context(), []string{"--server", "https://example.com"}, strings.NewReader(""), &output, &diagnostics)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"Molr", "molr"} {
		got, err := os.ReadFile(filepath.Join(dir, name, "connections.json"))
		if err != nil || !bytes.Equal(got, desktop) {
			t.Fatalf("desktop profile changed: %s: %q, %v", name, got, err)
		}
	}
	store, err := runtimeclient.OpenConnections(filepath.Join(dir, "molr", "tui", "connections.json"))
	if err != nil || len(store.Items) != 1 || store.Items[0].Server != "https://example.com" {
		t.Fatalf("TUI connection was not saved independently: %+v, %v", store, err)
	}
	if got, want := store.SessionPath(store.Items[0]), filepath.Join(dir, "molr", "tui", "sessions", store.Items[0].ID+".json"); got != want {
		t.Fatalf("session path = %q, want %q", got, want)
	}
}
