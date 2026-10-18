package installationwindows

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsUpgradeRejectsUnownedTaskWithoutRollback(t *testing.T) {
	runtimeBinary := os.Getenv("MOLR_TEST_BINARY")
	if runtimeBinary == "" {
		t.Skip("native Runtime payload required")
	}
	setup := filepath.Join(filepath.Dir(runtimeBinary), "molr-setup.exe")
	root := filepath.Join(t.TempDir(), "program")
	binary := filepath.Join(root, "versions", "fixture", "molr.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	r := record{Manager: "desktop", Name: fmt.Sprintf("molr-desktop-%016x", uint64(time.Now().UnixNano())), Installed: true, Binary: binary, DataDir: filepath.Join(root, "data"), Version: "fixture"}
	r.Paths.Unit = filepath.Join(root, "task.xml")
	config := filepath.Join(root, "server.json")
	receipt := filepath.Join(root, "installation.json")
	if err := writeJSON(receipt, r); err != nil {
		t.Fatal(err)
	}
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	noTriggers := func(xml string) string {
		start, end := strings.Index(xml, "<Triggers>"), strings.Index(xml, "</Triggers>")+len("</Triggers>")
		return xml[:start] + "<Triggers/>" + xml[end:]
	}
	saved := taskXMLFile(noTriggers(taskXML(r, config, sid)))
	if err = os.WriteFile(r.Paths.Unit, saved, 0600); err != nil {
		t.Fatal(err)
	}
	// Register a different harmless executable but never start it. Validation
	// must leave this task unchanged, even though an owned task.xml is on disk.
	other := r
	other.Binary = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	foreignXML := filepath.Join(root, "different.xml")
	if err = os.WriteFile(foreignXML, taskXMLFile(noTriggers(taskXML(other, config, sid))), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if _, err = scheduler(ctx, "/Create", "/TN", r.Name, "/XML", foreignXML, "/F"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := scheduler(ctx, "/Delete", "/TN", r.Name, "/F"); err != nil {
			t.Error(err)
		}
	})
	beforeTask, err := scheduler(ctx, "/Query", "/TN", r.Name, "/XML")
	if err != nil {
		t.Fatal(err)
	}
	beforeRecord, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = command(ctx, setup, "desktop", "upgrade", "--name", r.Name, "--program-root", root, "--json", "--non-interactive", "--no-browser", "--yes"); err == nil || !strings.Contains(err.Error(), "Exec.Command mismatch") {
		t.Fatalf("expected command ownership refusal, got %v", err)
	}
	afterTask, err := scheduler(ctx, "/Query", "/TN", r.Name, "/XML")
	if err != nil {
		t.Fatal(err)
	}
	afterRecord, _ := os.ReadFile(receipt)
	afterXML, _ := os.ReadFile(r.Paths.Unit)
	if !bytes.Equal(beforeTask, afterTask) || !bytes.Equal(beforeRecord, afterRecord) || !bytes.Equal(saved, afterXML) {
		t.Fatal("failed ownership check triggered a task/record rollback mutation")
	}
}
