package installrecord

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReceiptRejectsUnrelatedPathsAndWritableRecords(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "program/test")
	os.MkdirAll(program, 0755)
	r := Record{Schema: 1, State: "installed", Name: "molr-test", User: "fixture", UID: 1000, GID: 1000, OwnerUID: 1000, ProgramRoot: filepath.Dir(program), Paths: Paths{Program: program, Config: filepath.Join(dir, "config/molr/test"), Data: filepath.Join(dir, "data/molr/test"), Cache: filepath.Join(dir, "cache/molr/test"), Unit: filepath.Join(dir, "config/systemd/user/molr-test.service")}, DataDir: filepath.Join(dir, "data/molr/test"), ManagedData: true, Updates: Updates{Source: "https://example.com/releases", Channel: "preview"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	os.WriteFile(r.File(), data, 0600)
	got, err := Read(r.File())
	if err != nil || got.UpdateSource() != "https://example.com/releases/preview" {
		t.Fatal(got, err)
	}
	os.Chmod(r.File(), 0666)
	if _, err = Read(r.File()); err == nil {
		t.Fatal("writable receipt accepted")
	}
	os.Chmod(r.File(), 0600)
	other := filepath.Join(dir, "record.json")
	os.WriteFile(other, data, 0600)
	if _, err = Read(other); err == nil {
		t.Fatal("receipt relocated without instance")
	}
	r.Paths.Unit = filepath.Join(dir, "unrelated.service")
	if r.Validate() == nil {
		t.Fatal("unrelated unit accepted")
	}
	for _, name := range []string{"../molr", "molr-default", "molr-", "another"} {
		if _, err = Instance(name); err == nil {
			t.Fatal("accepted", name)
		}
	}
	if instance, err := Instance("molr"); err != nil || instance != "default" {
		t.Fatal(instance, err)
	}
}
