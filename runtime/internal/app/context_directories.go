package app

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// Instructions are discovered along explicitly visited local directories, never
// by traversing arbitrary project trees. Keep previously loaded scopes so edits
// and removals still refresh after checkpoint recovery and context compaction.
func instructionDirectories(request agent.ContextRequest, prefix string) ([]string, error) {
	directories := map[string]bool{".": true}
	add := func(name string) error {
		if name == prefix {
			name = "."
		} else if strings.HasPrefix(name, prefix+"/") {
			name = strings.TrimPrefix(name, prefix+"/")
		} else if strings.HasPrefix(name, "/") || prefix != "/work" {
			return nil
		}
		if name == "" {
			name = "."
		}
		if path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00") {
			return nil
		}
		parts := strings.Split(name, "/")
		if len(parts) > 32 {
			return core.Invalid("context_source", "instruction directory depth exceeded")
		}
		for _, part := range parts {
			switch part {
			case ".agents", ".git", "node_modules", ".cache", ".venv", "vendor", "dist", "build":
				return nil
			}
		}
		if name == platform.WorkspaceStateDirectory || strings.HasPrefix(name, platform.WorkspaceStateDirectory+"/") || parts[0] == "inbox" || parts[0] == "outputs" {
			return nil
		}
		for ; name != "."; name = path.Dir(name) {
			directories[name] = true
			if len(directories) > 256 {
				return core.Invalid("context_source", "too many active instruction directories")
			}
		}
		return nil
	}
	for _, d := range request.Previous {
		if d.Kind == "project_instruction" && strings.HasPrefix(d.Source, prefix+"/") && d.Source == path.Join(d.Scope, "AGENTS.md") {
			if err := add(d.Scope); err != nil {
				return nil, err
			}
		}
	}
	calls := map[string]model.ToolCall{}
	for _, input := range request.Inputs {
		if input.Role == model.Assistant {
			for _, call := range input.Calls {
				calls[call.ID] = call
			}
		}
		if input.Role != model.Tool {
			continue
		}
		call, ok := calls[input.ReplyTo]
		if !ok {
			continue
		}
		// Only successful local file reads/lists/searches activate scopes. A write,
		// external resource, remote operation or arbitrary output is not discovery.
		if call.Name != "file_list" && call.Name != "file_read" && call.Name != "file_search" {
			continue
		}
		var target struct {
			SandboxID  string `json:"sandbox_id"`
			ResourceID string `json:"resource_id"`
			Path       string `json:"path"`
			FullAccess bool   `json:"full_access"`
		}
		if json.Unmarshal(call.Arguments, &target) != nil {
			continue
		}
		if targetSandbox(core.ID(target.SandboxID)) != "local" || target.ResourceID != "" || target.FullAccess {
			continue
		}
		var result struct {
			SandboxID, ResourceID, Path string
			FullResultPath              string          `json:"full_result_path"`
			Details                     json.RawMessage `json:"details"`
			Error                       json.RawMessage `json:"error"`
			OK                          *bool           `json:"ok"`
		}
		if json.Unmarshal([]byte(input.Text), &result) != nil {
			continue
		}
		// Bounded tool replies preserve execution metadata in the archive envelope.
		// Directory listings can exceed the inline budget even with one page.
		if result.Path == "" && strings.HasPrefix(result.FullResultPath, "/work/"+platform.WorkspaceStateDirectory+"/tool-result-") && len(result.Details) != 0 {
			if json.Unmarshal(result.Details, &result) != nil {
				continue
			}
		}
		if result.Path == "" || len(result.Error) != 0 || result.OK != nil && !*result.OK || result.ResourceID != "" || targetSandbox(core.ID(result.SandboxID)) != "local" {
			continue
		}
		name := target.Path
		if call.Name == "file_read" {
			name = path.Dir(name)
		}
		if err := add(name); err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(directories))
	for directory := range directories {
		result = append(result, directory)
	}
	sort.Strings(result)
	return result, nil
}
