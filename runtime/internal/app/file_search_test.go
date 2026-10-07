package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
)

func TestFileSearchWorkspaceGlobalAndExternalScope(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	files := &resourceFileTools{adapters: f.ad}
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	need(t, os.MkdirAll(filepath.Join(root, ".agents/memory/用户偏好"), 0700))
	need(t, os.WriteFile(filepath.Join(root, ".agents/memory/用户偏好/沟通偏好.md"), []byte("先给结论，再给必要细节。\n"), 0600))
	raw, err := files.search(t.Context(), f.execution, tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: "/work/.agents/memory"}, Pattern: "结论", Glob: []string{"**/*.md"}})
	need(t, err)
	var result textfiles.SearchResult
	need(t, json.Unmarshal(raw, &result))
	if !result.Complete || len(result.Matches) != 1 || result.Matches[0].Path != "/work/.agents/memory/用户偏好/沟通偏好.md" {
		t.Fatal(result)
	}
	global := platform.UserGlobal(f.ad.dataDir, f.owner.OwnerID)
	need(t, os.MkdirAll(global, 0700))
	need(t, os.WriteFile(filepath.Join(global, "MEMORY.md"), []byte("共享结论"), 0600))
	raw, err = files.search(t.Context(), f.execution, tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: "/global/MEMORY.md"}, Pattern: "共享"})
	need(t, err)
	need(t, json.Unmarshal(raw, &result))
	if len(result.Matches) != 1 || result.Matches[0].Path != "/global/MEMORY.md" {
		t.Fatal(result)
	}
	_, err = files.search(t.Context(), f.execution, tools.FileSearchRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/"}, Pattern: "secret"})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("ungranted storage searched", err)
	}
}

func TestMemoryFileRevisionsInvalidateOldSearchSnippets(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	files := &resourceFileTools{adapters: f.ad}
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	need(t, os.MkdirAll(filepath.Join(root, ".agents/memory"), 0700))
	name := filepath.Join(root, ".agents/memory/沟通偏好.md")
	need(t, os.WriteFile(name, []byte("先给结论"), 0600))
	req := tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: "/work/.agents/memory"}, Pattern: "结论"}
	raw, err := files.search(t.Context(), f.execution, req)
	need(t, err)
	args, _ := json.Marshal(req)
	request := agent.ContextRequest{Execution: f.execution, Inputs: []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "search", Name: "file_search", Arguments: args}}}, {Role: model.Tool, ReplyTo: "search", Text: string(raw)}}}
	need(t, os.WriteFile(name, []byte("先写风险"), 0600))
	docs, err := files.memoryFileRevisions(t.Context(), request)
	need(t, err)
	if len(docs) != 1 || !strings.Contains(docs[0].Content, "changed") {
		t.Fatal(docs)
	}
	need(t, os.Remove(name))
	docs, err = files.memoryFileRevisions(t.Context(), request)
	need(t, err)
	if !strings.Contains(docs[0].Content, "missing_or_inaccessible") {
		t.Fatal(docs)
	}
}
