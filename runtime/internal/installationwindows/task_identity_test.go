package installationwindows

import (
	"strings"
	"testing"
)

// Artificial paths/SIDs, matching schtasks' namespaced Task document format.
const queriedTaskWithoutRunLevel = `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
 <RegistrationInfo><URI>\molr-desktop-fixture</URI></RegistrationInfo>
 <Triggers><LogonTrigger><UserId>S-1-5-21-1-2-3-1001</UserId><Enabled>true</Enabled></LogonTrigger></Triggers>
 <Principals><Principal id="Owner"><UserId>S-1-5-21-1-2-3-1001</UserId><LogonType>InteractiveToken</LogonType></Principal></Principals>
 <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
 <Actions Context="Owner"><Exec><Command>C:\测试\versions\old\molr.exe</Command><Arguments>serve --config C:\测试\server.json --log-file C:\测试\runtime.log</Arguments><WorkingDirectory>C:\测试\versions\old</WorkingDirectory></Exec></Actions>
</Task>`

func explicitTask() string {
	return strings.Replace(queriedTaskWithoutRunLevel, "</LogonType>", "</LogonType><RunLevel>LeastPrivilege</RunLevel>", 1)
}
func TestWindowsTaskOmittedRunLevelMatchesInstalledTask(t *testing.T) {
	// Actual machine diagnostics showed every identity field equal except the
	// omitted default RunLevel. MS-TSCH 3.2.5.4.2 requires LeastPrivilege when absent:
	// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-tsch/849c131a-64e4-46ef-b015-9d4c599c5167
	for _, actual := range []string{queriedTaskWithoutRunLevel, explicitTask(), strings.Replace(queriedTaskWithoutRunLevel, "<LogonType>InteractiveToken</LogonType>", "", 1)} {
		if err := compareTaskIdentity([]byte(actual), []byte(explicitTask())); err != nil {
			t.Fatal(err)
		}
	}
}
func TestWindowsTaskIdentityRejectsSpecificMismatches(t *testing.T) {
	for _, tc := range []struct{ name, old, new, field string }{
		{"elevated", "</LogonType>", "</LogonType><RunLevel>HighestAvailable</RunLevel>", "RunLevel"},
		{"invalid privilege", "</LogonType>", "</LogonType><RunLevel>invalid</RunLevel>", "RunLevel"},
		{"different user", "<UserId>S-1-5-21-1-2-3-1001</UserId>", "<UserId>S-1-5-21-1-2-3-1002</UserId>", "UserId"},
		{"different logon", "InteractiveToken", "Password", "LogonType"},
		{"other program", `<Command>C:\测试\versions\old\molr.exe</Command>`, `<Command>C:\other\molr.exe</Command>`, "Command"},
		{"other config", `--config C:\测试\server.json`, `--config C:\other\server.json`, "Arguments"},
		{"other directory", `<WorkingDirectory>C:\测试\versions\old</WorkingDirectory>`, `<WorkingDirectory>C:\other</WorkingDirectory>`, "WorkingDirectory"},
		{"missing user", "<UserId>S-1-5-21-1-2-3-1001</UserId>", "", "UserId"},
		{"wrong context", `Context="Owner"`, `Context="Other"`, "Context"},
		{"additional action", "</Exec>", "</Exec><ComHandler><ClassId>fixture</ClassId></ComHandler>", "Actions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := strings.ReplaceAll(queriedTaskWithoutRunLevel, tc.old, tc.new)
			err := compareTaskIdentity([]byte(actual), []byte(explicitTask()))
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected %s diagnostic, got %v", tc.field, err)
			}
		})
	}
}
func TestWindowsTaskQueryDocumentShapes(t *testing.T) {
	bare := strings.TrimPrefix(queriedTaskWithoutRunLevel, `<?xml version="1.0" encoding="UTF-8"?>`)
	wrapped := "<Tasks>" + bare + "</Tasks>"
	if err := compareTaskIdentity([]byte(wrapped), []byte(explicitTask())); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"<Unknown/>", "<Task/>", "<Tasks/>", "<Tasks>" + bare + bare + "</Tasks>", bare + bare, strings.Replace(bare, "</Principal>", "</Principal><Principal/>", 1), strings.Replace(bare, "</Exec>", "</Exec><Exec/>", 1)} {
		if _, err := parseTaskXML([]byte(raw)); err == nil {
			t.Fatal("ambiguous or missing identity accepted")
		}
	}
}
