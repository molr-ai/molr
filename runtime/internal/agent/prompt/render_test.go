package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectionPreservesMeaningAndExcludesInternalFields(t *testing.T) {
	cases := []struct {
		name, raw    string
		want, absent []string
	}{
		{"file_read", `{"OwnerID":"private","Path":"/work/a","Version":"v1","Text":"<p>\r\n\"quoted\"</p>","Truncated":false,"NextOffset":0}`, []string{`"Text":"<p>\r\n\"quoted\"</p>"`, `"Version":"v1"`, `"Truncated":false`}, []string{"OwnerID", `\u003c`}},
		{"file_read", `{"path":"/work/a.png","version":"v2","media_type":"image/png","size_bytes":42,"content_loaded":false,"reason":"unsupported","Media":[{"Data":"secret"}]}`, []string{`"content_loaded":false`, `"reason":"unsupported"`}, []string{"secret", "Media"}},
		{"file_search", `{"Path":"/work/notes.txt","TargetKind":"file","Matches":[]}`, []string{`"TargetKind":"file"`, `"Matches":[]`}, nil},
		{"archive", `{"full_result_path":"/work/result.json","details":{"Path":"/work/notes.txt","TargetKind":"file"}}`, []string{`"TargetKind":"file"`, `"Path":"/work/notes.txt"`}, nil},
		{"file_list", `{"Entries":[],"NextOffset":40,"Truncated":true}`, []string{`"Entries":[]`, `"NextOffset":40`, `"Truncated":true`}, nil},
		{"workspace", `{"Path":"/work","Available":false,"Reason":"sandbox_disabled","HostPath":"private","Directories":[{"Path":"/work/inbox","Purpose":"originals","Internal":"secret"}]}`, []string{`"Available":false`, `"Reason":"sandbox_disabled"`, "/work/inbox"}, []string{"HostPath", "private", "Internal", "secret"}},
		{"permissions", `{"Granted":[],"Total":0,"Truncated":false,"Revision":"internal"}`, []string{`"Granted":[]`, `"Total":0`, `"Truncated":false`}, []string{"Revision"}},
		{"permission_list", `{"Resources":[{"ResourceID":"r","Grants":[{"Actions":["read"],"Scope":{"PathPrefixes":["/reports"],"ReadOnly":true},"ExpiresAt":"2030-01-01T00:00:00Z"}]}],"Requestable":[{"ResourceID":"n","State":"active","SandboxID":"s","Actions":["execute"]}],"RequestableTruncated":true}`, []string{"/reports", "ExpiresAt", "RequestableTruncated", "SandboxID"}, nil},
		{"browser_session", `{"Available":true,"Task":{"ID":"browser-task","Kind":"browser","Browser":{"Control":"user","Generation":3,"NeedsSnapshot":true,"ControllerSession":"secret"},"OwnerID":"private"}}`, []string{"browser-task", `"Control":"user"`, `"Generation":3`}, []string{"secret", "OwnerID"}},
		{"browser_action", `{"ok":true,"data":{"snapshot":"textbox Name @e1","refs":{"e1":{"role":"textbox","name":"Name"}},"private":"secret"}}`, []string{"textbox Name @e1", `"refs"`}, []string{"secret"}},
		{"shell_execute", `{"Exit":{"Code":0,"TimedOut":false,"OutputTruncated":true,"NetworkReason":"denied"},"OutputComplete":false,"Stdout":"abc","StdoutPath":"/work/out","StdoutBytes":999,"OutputNote":"duplicate","OwnerID":"private"}`, []string{`"Code":0`, `"OutputComplete":false`, `"OutputTruncated":true`, "/work/out"}, []string{"OutputNote", "OwnerID"}},
		{"shell_execute", `{"Task":{"Forwards":[{"Protocol":"tcp","Port":5173,"Address":"192.168.1.10:23000","URL":"http://192.168.1.10:23000","State":"waiting"}],"ID":"job","Mode":"tty","State":"running","OwnerID":"private"},"Notice":"duplicate"}`, []string{`"Task":`, `"ID":"job"`, `"Mode":"tty"`, `"Forwards":`, `"Port":5173`, "http://192.168.1.10:23000"}, []string{"OwnerID", "Notice"}},
		{"shell_task", `{"Accepted":true,"Output":"","NextCursor":0}`, []string{`"Accepted":true`, `"NextCursor":0`}, nil},
		{"conversation_read", `{"Items":[{"ID":"m","Sender":"user","SenderID":"private","Content":{"Kind":"text","Text":"<user>\nhello","AttachmentIDs":["f"]}}],"Attachments":{"f":{"ID":"f","Name":"a","MediaType":"text/plain","OwnerID":"private"}},"NextCursor":"next"}`, []string{"<user>", "AttachmentIDs", "MediaType", `"NextCursor":"next"`}, []string{"OwnerID", "SenderID"}},
		{"plan_read", `{"PlanID":"todo:x","Version":"v2","Instructions":"do this","Ready":false,"CurrentExecution":{"ID":"run","State":"running","Task":{"OwnerID":"private"}},"ExecutionsTruncated":true}`, []string{"Version", "Instructions", `"Ready":false`, `"ID":"run"`, "ExecutionsTruncated"}, []string{"OwnerID", "Task"}},
		{"failure", `{"ok":false,"operation_id":"op","error":{"code":"canceled","execution_state":"unknown","side_effects_possible":true,"message":"uncertain","private":"secret"}}`, []string{`"ok":false`, `"execution_state":"unknown"`, `"side_effects_possible":true`}, []string{"private", "secret"}},
		{"archive", `{"summary":"Full result saved","full_result_path":"/work/result.json","bytes":9999,"preview":"serialized internals","tail":"more internals","details":{"Exit":{"Code":1},"NextByteOffset":4096}}`, []string{"/work/result.json", `"Code":1`, `"NextByteOffset":4096`}, []string{`"preview"`, `"tail"`, "internals"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := Structured(tc.name, tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid([]byte(text)) {
				t.Fatalf("invalid JSON: %s", text)
			}
			for _, v := range tc.want {
				if !strings.Contains(text, v) {
					t.Errorf("missing %s in %s", v, text)
				}
			}
			for _, v := range tc.absent {
				if strings.Contains(text, v) {
					t.Errorf("leaked %s in %s", v, text)
				}
			}
			again, err := Structured(tc.name, text)
			if err != nil || again != text {
				t.Fatal("presentation is not idempotent")
			}
		})
	}
}
func TestInvalidStructuredSourceIsNotSilentlyEmptied(t *testing.T) {
	for _, raw := range []string{`{"Granted":"bad"}`, `{"Granted":[{"Actions":{"read":true}}]}`, `{} {}`} {
		if _, err := Structured("permissions", raw); err == nil {
			t.Fatalf("accepted malformed source %s", raw)
		}
	}
}

func TestStringPreservesTextAndEscapesOnlyJSON(t *testing.T) {
	original := "<tag>\r\n\"quoted\"\\path\t中文 & >"
	rendered := String(original)
	var decoded string
	if err := json.Unmarshal([]byte(rendered), &decoded); err != nil || decoded != original || strings.Contains(rendered, `\u003c`) || strings.Contains(rendered, `\u0026`) {
		t.Fatalf("text changed: %s (%v)", rendered, err)
	}
}
