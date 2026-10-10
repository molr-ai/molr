package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
)

func TestFileSearchSingleFileRefreshesParentInstructions(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	files := &resourceFileTools{adapters: f.ad}
	for _, prefix := range []string{"/work", "/global"} {
		root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
		if prefix == "/global" {
			root = platform.UserGlobal(f.ad.dataDir, f.owner.OwnerID)
		}
		need(t, os.MkdirAll(filepath.Join(root, "project/nested"), 0700))
		need(t, os.WriteFile(filepath.Join(root, "project/AGENTS.md"), []byte("Project instructions"), 0600))
		need(t, os.WriteFile(filepath.Join(root, "project/nested/AGENTS.md"), []byte("Not visited"), 0600))
		need(t, os.WriteFile(filepath.Join(root, "project/notes.txt"), []byte("needle\n"), 0600))
		need(t, os.WriteFile(filepath.Join(root, "project/empty.txt"), nil, 0600))
		need(t, os.WriteFile(filepath.Join(root, "project/large.txt"), []byte(strings.Repeat("needle "+strings.Repeat("x", 1000)+"\n", 30)), 0600))
		for _, tc := range []struct {
			name, target, pattern, mode, kind string
			glob                              []string
			archive, remove                   bool
		}{
			{name: "file hit", target: "/project/notes.txt", pattern: "needle", kind: "file"},
			{name: "file miss", target: "/project/notes.txt", pattern: "missing", kind: "file"},
			{name: "empty file", target: "/project/empty.txt", pattern: "needle", kind: "file"},
			{name: "file filtered out", target: "/project/notes.txt", pattern: "needle", glob: []string{"*.md"}, kind: "file"},
			{name: "file path mode", target: "/project/notes.txt", mode: "files", kind: "file"},
			{name: "directory hit", target: "/project", pattern: "needle", kind: "directory"},
			{name: "directory miss", target: "/project", pattern: "missing", kind: "directory"},
			{name: "directory path mode", target: "/project", mode: "files", kind: "directory"},
			{name: "archived file result", target: "/project/large.txt", pattern: "needle", kind: "file", archive: true},
			{name: "removed file", target: "/project/notes.txt", pattern: "needle", kind: "file", remove: true},
		} {
			t.Run(prefix+"/"+tc.name, func(t *testing.T) {
				request := tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: prefix + tc.target}, Pattern: tc.pattern, Mode: tc.mode, Glob: tc.glob}
				result, err := files.search(t.Context(), f.execution, request)
				need(t, err)
				var search textfiles.SearchResult
				need(t, json.Unmarshal(result, &search))
				if search.TargetKind != tc.kind {
					t.Fatalf("target kind = %q, want %q", search.TargetKind, tc.kind)
				}
				if tc.archive {
					bounded, err := files.boundResult(t.Context(), f.execution, tools.Result{Content: result})
					need(t, err)
					result = bounded.Content
					if !strings.Contains(string(result), "full_result_path") {
						t.Fatal("fixture did not exercise archived search metadata")
					}
				}
				if tc.remove {
					need(t, os.Remove(filepath.Join(root, strings.TrimPrefix(tc.target, "/"))))
				}
				arguments, err := json.Marshal(request)
				need(t, err)
				docs, err := files.contextSources(t.Context(), agent.ContextRequest{Execution: f.execution, Inputs: []model.Input{
					{Role: model.Assistant, Calls: []model.ToolCall{{ID: "search", Name: "file_search", Arguments: arguments}}},
					{Role: model.Tool, ReplyTo: "search", Text: string(result)},
				}})
				if err != nil {
					t.Fatalf("context refresh after a successful search: %v", err)
				}
				found := false
				for _, doc := range docs {
					if doc.Kind != "project_instruction" {
						continue
					}
					if doc.Source != prefix+"/project/AGENTS.md" || doc.Scope != prefix+"/project" || doc.Content != "Project instructions" {
						t.Fatalf("unexpected instruction scope: %+v", doc)
					}
					found = true
				}
				if !found {
					t.Fatalf("search did not discover parent instructions: %+v", docs)
				}
			})
		}
	}
}

func TestFileSearchInstructionDiscoveryRejectsIneligibleResults(t *testing.T) {
	for _, variant := range []string{"remote target", "remote result", "external target", "external result", "full access", "failed", "uncompleted", "missing kind", "unknown kind"} {
		t.Run(variant, func(t *testing.T) {
			target := map[string]any{"path": "/work/project/notes.txt"}
			result := map[string]any{"Path": "/work/project/notes.txt", "TargetKind": "file"}
			switch variant {
			case "remote target":
				target["sandbox_id"] = "remote"
			case "remote result":
				result["SandboxID"] = "remote"
			case "external target":
				target["resource_id"] = "external"
			case "external result":
				result["ResourceID"] = "external"
			case "full access":
				target["full_access"] = true
			case "failed":
				result["ok"] = false
			case "missing kind":
				delete(result, "TargetKind")
			case "unknown kind":
				result["TargetKind"] = "link"
			}
			arguments, _ := json.Marshal(target)
			content, _ := json.Marshal(result)
			request := agent.ContextRequest{Inputs: []model.Input{
				{Role: model.Assistant, Calls: []model.ToolCall{{ID: "search", Name: "file_search", Arguments: arguments}}},
				{Role: model.Tool, ReplyTo: "search", Text: string(content)},
			}}
			if variant == "uncompleted" {
				request.Inputs = request.Inputs[:1]
			}
			dirs, err := instructionDirectories(request, "/work")
			need(t, err)
			if !reflect.DeepEqual(dirs, []string{"."}) {
				t.Fatalf("ineligible search activated instructions: %v", dirs)
			}
		})
	}
}
