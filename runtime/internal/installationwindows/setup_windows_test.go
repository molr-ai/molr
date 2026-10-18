package installationwindows

import (
	"encoding/xml"
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
