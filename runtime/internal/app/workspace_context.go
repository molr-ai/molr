package app

import (
	"context"
	"encoding/json"
	"runtime"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

type workspaceLocation struct{ Path, Access, Lifetime string }
type workspaceDirectory struct{ Path, Purpose string }
type workspaceDescription struct {
	SandboxID         string
	Path              string
	Available         bool
	Reason            string `json:",omitempty"`
	Lifetime          string
	Shared, Temporary workspaceLocation
	Directories       []workspaceDirectory
}

// Workspace metadata is a small runtime-owned contract, never a directory tree
// or another store of assistant identity, user preferences or permissions.
func workspaceDocument(reason string) agent.ContextDocument {
	value := workspaceDescription{
		SandboxID: "local", Path: "/work", Available: reason == "", Reason: reason, Lifetime: "assistant",
		Shared:    workspaceLocation{"/global", "read-only in ordinary assistant operations", "owner"},
		Temporary: workspaceLocation{"/tmp", "shell only", "single execution"},
		Directories: []workspaceDirectory{
			{"/work/inbox", "Original attachments and imported materials"},
			{"/work/projects", "Work files, scripts and drafts organized by meaningful topic; ordinary folders, not task records"},
			{"/work/outputs", "Deliverables; saving here does not send them to the user"},
			{"/work/.agents/skills", "Assistant skills"},
			{"/work/.agents/memory", "Detailed memories; summary at /work/MEMORY.md"},
			{"/work/.agents/reviews", "Dream reviews"},
			{"/work/" + platform.WorkspaceStateDirectory, "Execution records: tool results and shell output retained across commands and restarts; no automatic cleanup"},
			{"/work/.cache", "Reproducible cache; persists across commands; no automatic cleanup"},
		},
	}
	raw, _ := json.Marshal(value)
	return agent.ContextDocument{Kind: "workspace", Source: "runtime/workspace", Scope: "assistant", Revision: sourceRevision(string(raw)), Content: string(raw)}
}

func (f *resourceFileTools) workspaceAvailability(ctx context.Context, e core.Execution) (string, error) {
	if _, err := groupReference(ctx, f.adapters.store, e.Principal.OwnerID, "sandbox", "local"); err != nil {
		if core.ReasonOf(err) == "group_resource_denied" {
			return "group_resource_denied", nil
		}
		return "", err
	}
	// Production routes storage through the sandbox router. Internal isolated
	// adapters used by tests have no router and retain their storage capability.
	if f.router != nil && !f.adapters.storageConfig.Enabled {
		return "sandbox_disabled", nil
	}
	if runtime.GOOS != "linux" {
		return "storage_unavailable", nil
	}
	return "", nil
}
