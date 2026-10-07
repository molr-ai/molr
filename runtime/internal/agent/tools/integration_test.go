package tools

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
)

func TestRemovedToolsCannotExecute(t *testing.T) {
	f := newContractFixture(t)
	for _, name := range []string{"task_create", "task_read", "task_report", "memory_read", "memory_write", "search"} {
		call := f.Call
		call.Name = name
		call.Arguments = json.RawMessage(`{}`)
		_, err := f.Executor.Execute(t.Context(), call)
		testkit.RequireCode(t, err, core.NotFound)
	}
	if f.Effects() != 0 {
		t.Fatal("removed tool caused side effects")
	}
}
