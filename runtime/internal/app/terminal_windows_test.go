package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "sandbox-helper" {
		os.Exit(sandbox.HelperMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func TestWindowsTerminalDefaultShellRouting(t *testing.T) {
	directory, err := filepath.EvalSymlinks(os.TempDir())
	need(t, err)
	t.Setenv("TEMP", directory)
	t.Setenv("TMP", directory)
	b, owner, e := taskFixture(t)
	need(t, b.sandbox.runner.Prepare(t.Context()))
	compute, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "terminal-compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Compute"})
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "terminal-grant"}, AssistantID: e.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{}, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}}})
	need(t, err)
	// An omitted model-tool script must resolve to PowerShell on this node.
	_, err = b.shellTasks.Execute(t.Context(), e, tools.ShellRequest{ComputeID: compute.ID, TTY: true})
	need(t, err)
	_, err = b.shellTasks.Manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, Generation: 1, Data: "Write-Output ('native-' + $PSVersionTable.PSEdition + '-ok')\r"})
	need(t, err)
	deadline := time.Now().Add(15 * time.Second)
	cursor := int64(0)
	var output strings.Builder
	for {
		raw, err := b.shellTasks.Manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: e.OperationID, Generation: 1, Cursor: cursor})
		need(t, err)
		var result struct {
			Output     string
			NextCursor int64
		}
		need(t, json.Unmarshal(raw, &result))
		cursor = result.NextCursor
		output.WriteString(result.Output)
		if strings.Contains(output.String(), "native-Desktop-ok") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("default shell did not run PowerShell", output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, err = b.shellTasks.Manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "stop", TaskID: e.OperationID, Generation: 1})
	need(t, err)
}
