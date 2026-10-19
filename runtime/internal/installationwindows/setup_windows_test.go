package installationwindows

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsTaskIdentity(t *testing.T) {
	r := record{Binary: `C:\Users\Test & User\Molr\molr.exe`}
	raw := taskXML(r, `C:\Users\Test & User\Molr\server.json`, "S-1-5-21-1-2-3-1001")
	var task struct {
		Principals struct {
			Principal struct {
				UserID   string `xml:"UserId"`
				Logon    string `xml:"LogonType"`
				RunLevel string
			}
		}
		Actions struct {
			Exec struct{ Command, Arguments string }
		}
	}
	if err := xml.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	if task.Principals.Principal.Logon != "InteractiveToken" || task.Principals.Principal.RunLevel != "LeastPrivilege" || task.Actions.Exec.Command != r.Binary || !strings.Contains(task.Actions.Exec.Arguments, `"C:\Users\Test & User\Molr\server.json"`) {
		t.Fatal(task)
	}
}

func TestWindowsBackupRestoreRejectsMissingOrInvalidInputWithoutChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	archive := filepath.Join(t.TempDir(), "corrupt.gz")
	if err := os.WriteFile(archive, []byte("invalid archive"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"backup", "--name", "molr-backup-test", "--program-root", root, "--output", archive},
		{"restore", "--name", "molr-backup-test", "--program-root", root, "--archive", archive, "--yes"},
	} {
		if err := Run(t.Context(), args, io.Discard, io.Discard); err == nil {
			t.Fatal("invalid request succeeded", args)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid request changed target")
	}
}
