package updates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationReceiptRequiresMatchingExecutableAndCompletedInstall(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "server.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Dir(filepath.Dir(executable))
	paths := map[string]string{"prefix": prefix, "config": dir, "data": filepath.Join(dir, "data"), "cache": filepath.Join(dir, "cache")}
	record := map[string]any{"schema": 1, "name": "molr-test", "user": "fixture", "complete": true, "program_root": filepath.Dir(prefix), "paths": paths}
	write := func() {
		t.Helper()
		data, _ := json.Marshal(record)
		if err := os.WriteFile(filepath.Join(dir, "installation.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	source := "https://example.com/releases/preview"
	got := ReadInstallation(config, source)
	if got == nil || got.Source != source || got.Name != "molr-test" {
		t.Fatal(got)
	}
	record["complete"] = false
	write()
	if ReadInstallation(config, source) != nil {
		t.Fatal("incomplete installation accepted")
	}
	record["complete"] = true
	paths["prefix"] = filepath.Join(dir, "other")
	write()
	if ReadInstallation(config, source) != nil {
		t.Fatal("unrelated executable accepted")
	}
	paths["prefix"] = prefix
	paths["data"] = "relative"
	write()
	if ReadInstallation(config, source) != nil {
		t.Fatal("relative data path accepted")
	}
}
