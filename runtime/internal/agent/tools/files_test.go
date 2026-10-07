package tools

import (
	"encoding/json"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestFileToolArgumentsAreSpecificAndRequireExplicitOverwrite(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"file_read", `{"resource_id":"r","path":"/a"}`, true},
		{"file_read", `{"resource_id":"r","path":"/a","offset":2,"limit":3}`, true},
		{"file_read", `{"resource_id":"r","path":"/a","offset":0}`, false},
		{"file_read", `{"resource_id":"r","path":"/a","text":"ignored"}`, false},
		{"file_read", `{"resource_id":"r","path":"/a","limit":2001}`, false},
		{"file_read", `{"resource_id":"r","path":"/a","limit":null}`, false},
		{"file_list", `{"resource_id":"r","path":"/"}`, true},
		{"file_list", `{"resource_id":"r","path":"/","offset":10}`, false},
		{"file_list", `{"resource_id":"r","path":"/","offset":10,"expected_version":"version"}`, true},
		{"file_write", `{"resource_id":"r","path":"/a","text":""}`, false},
		{"file_write", `{"resource_id":"r","path":"/a","text":"","overwrite":false}`, true},
		{"file_write", `{"resource_id":"r","path":"/a","text":"","overwrite":null}`, false},
		{"file_write", `{"resource_id":"r","path":"/a","overwrite":true}`, false},
		{"file_edit", `{"resource_id":"r","path":"/a","old_text":"a","new_text":"","expected_version":"v"}`, true},
		{"file_edit", `{"resource_id":"r","path":"/a","old_text":"a","new_text":"b"}`, false},
		{"file_edit", `{"resource_id":"r","path":"/a","old_text":"","new_text":"b","expected_version":"v"}`, false},
		{"file_read", `{"resource_id":"r","path":"../outside"}`, false},
		{"file_read", `null`, false},
	} {
		t.Run(tc.name+tc.raw, func(t *testing.T) {
			args, err := parse(Call{Name: tc.name, Arguments: json.RawMessage(tc.raw)})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if args.ResourceID != "r" {
					t.Fatal("missing scoped resource")
				}
			} else if core.CodeOf(err) != core.InvalidArgument {
				t.Fatalf("invalid arguments accepted: %v", err)
			}
		})
	}
}

func TestFileToolsDefaultWorkspaceAndBackendAbsolutePaths(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"path":"note.txt"}`, "/work/note.txt"},
		{`{"path":"/work/note.txt"}`, "/work/note.txt"},
		{`{"path":"/etc/passwd"}`, "/etc/passwd"},
		{`{"path":"/work/../other/note"}`, ""},
		{`{"path":"../other/note"}`, ""},
		{`{"path":"/tmp/note"}`, "/tmp/note"},
		{`{"path":"/work/note","assistant_id":"other"}`, ""},
		{`{"path":"/work/note","resource_id":""}`, ""},
	} {
		args, err := parse(Call{Name: "file_read", Arguments: json.RawMessage(tc.raw)})
		if tc.want == "" {
			if core.CodeOf(err) != core.InvalidArgument {
				t.Fatalf("unconfined workspace path: %s", tc.raw)
			}
			continue
		}
		if err != nil || args.ResourceID != "" || args.fileRead.Path != tc.want {
			t.Fatalf("workspace routing: %+v %v", args, err)
		}
	}
	args, err := parse(Call{Name: "file_list", Arguments: json.RawMessage(`{"path":"."}`)})
	if err != nil || args.fileList.Path != "/work" {
		t.Fatal("workspace root not browsable")
	}
}

func TestUnifiedPathsAndMediaOptions(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"path":"/resources/storage/read/image.png"}`, true},
		{`{"path":"/work/image.png","metadata_only":true}`, true},
		{`{"path":"image.png","detail":"high"}`, true},
		{`{"path":"clip.mp4","start_seconds":2,"end_seconds":5,"max_frames":3}`, true},
		{`{"path":"/resources/storage/../secret"}`, false},
		{`{"path":"/resources//file"}`, false},
		{`{"path":"/etc/passwd"}`, true},
		{`{"path":"image.png","offset":1,"detail":"high"}`, false},
		{`{"path":"clip.mp4","start_seconds":5,"end_seconds":2}`, false},
		{`{"path":"clip.mp4","max_frames":17}`, false},
		{`{"path":"clip.mp4","ModelID":"forged"}`, false},
	} {
		args, err := fileArguments(Call{Name: "file_read", Arguments: json.RawMessage(tc.raw)})
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if tc.valid && tc.raw == `{"path":"/resources/storage/read/image.png"}` && (args.ResourceID != "storage" || args.Path != "/read/image.png") {
			t.Fatalf("wrong resource resolution: %+v", args)
		}
	}
}
