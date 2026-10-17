package updates

import (
	"encoding/json"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationReceiptRequiresMatchingExecutableAndCompletedInstall(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "program/test")
	config := filepath.Join(dir, "config/molr/test")
	os.MkdirAll(program, 0755)
	r := installrecord.Record{Mode: "server", Schema: 1, State: "installed", Name: "molr-test", User: "fixture", UID: 1000, GID: 1000, OwnerUID: 1000, ProgramRoot: filepath.Dir(program), Paths: installrecord.Paths{Program: program, Config: config, Data: filepath.Join(dir, "data/molr/test"), Cache: filepath.Join(dir, "cache/molr/test"), Unit: filepath.Join(dir, "config/systemd/user/molr-test.service")}, DataDir: filepath.Join(dir, "data/molr/test"), ManagedData: true, Updates: installrecord.Updates{Source: "https://example.com/releases", Channel: "preview"}}
	write := func() {
		data, _ := json.Marshal(r)
		if err := os.WriteFile(r.File(), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	exe := filepath.Join(program, "bin/molr")
	cfg := filepath.Join(config, "server.json")
	got := readInstallation(exe, cfg)
	if got == nil || got.Source != r.UpdateSource() || got.SetupPath != filepath.Join(program, "bin/molr-setup") {
		t.Fatal(got)
	}
	r.State = "installing"
	write()
	if readInstallation(exe, cfg) != nil {
		t.Fatal("incomplete receipt accepted")
	}
	r.State = "installed"
	write()
	if readInstallation(exe, filepath.Join(dir, "unrelated.json")) != nil {
		t.Fatal("unrelated config accepted")
	}
}
