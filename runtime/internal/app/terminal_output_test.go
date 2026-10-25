package app

import (
	"encoding/base64"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/shellsession"
	"strings"
	"testing"
)

func TestTerminalOutputPreservesBytesAcrossChunks(t *testing.T) {
	root := t.TempDir()
	prefix := ".molr/tool-shell-test"
	// Use the same archive naming as the runtime rather than a second protocol.
	e := core.Execution{Principal: core.Principal{OwnerID: "owner"}, OperationID: "bytes"}
	prefix = shellsession.OutputPrefix(e)
	out, stderr, path, errpath, err := createShellOutputsNamed(root, 1<<20, prefix)
	need(t, err)
	payload := []byte(strings.Repeat("a", (16<<10)-1) + "中文\x1b[31m\xff")
	_, err = out.Write(payload)
	need(t, err)
	out.close()
	stderr.close()
	var got []byte
	var cursor int64
	for len(got) < len(payload) {
		raw, err := shellsession.ReadOutput(root, path, errpath, tools.ShellTaskRequest{Action: "read", Encoding: "base64", Cursor: cursor})
		need(t, err)
		var chunk struct {
			Output     string
			NextCursor int64
		}
		need(t, json.Unmarshal(raw, &chunk))
		bytes, err := base64.StdEncoding.DecodeString(chunk.Output)
		need(t, err)
		got = append(got, bytes...)
		cursor = chunk.NextCursor
	}
	if string(got) != string(payload) {
		t.Fatal("terminal bytes changed")
	}
}
