package app

import (
	"context"
	"encoding/json"
	"strings"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
)

// Recheck recent filesystem references, not a second memory retrieval system.
// No remembered body is loaded; changed/missing files invalidate old tool text.
func (f *resourceFileTools) memoryFileRevisions(ctx context.Context, r agent.ContextRequest) ([]agent.ContextDocument, error) {
	calls := map[string]model.ToolCall{}
	versions := map[string]string{}
	order := []string{}
	remember := func(p, v string) {
		if v == "" {
			return
		}
		if p != "/work/MEMORY.md" && p != "/global/MEMORY.md" && !strings.HasPrefix(p, "/work/.agents/memory/") && !strings.HasPrefix(p, "/global/.agents/memory/") && !strings.HasPrefix(p, "/work/.agents/reviews/") {
			return
		}
		if _, ok := versions[p]; ok {
			for i, old := range order {
				if old == p {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
		order = append(order, p)
		versions[p] = v
	}
	for _, input := range r.Inputs {
		for _, call := range input.Calls {
			calls[call.ID] = call
		}
		if input.Role != model.Tool {
			continue
		}
		call, ok := calls[input.ReplyTo]
		if !ok || call.Name != "file_read" && call.Name != "file_search" {
			continue
		}
		var target tools.FileTarget
		if json.Unmarshal(call.Arguments, &target) != nil || target.ResourceID != "" || target.SandboxID != "" && target.SandboxID != "local" {
			continue
		}
		var result struct {
			SandboxID     core.ID
			ResourceID    core.ID
			Path, Version string
			Matches       []struct{ Path, Version string }
			Error         json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(input.Text), &result) != nil || len(result.Error) > 0 || result.ResourceID != "" || result.SandboxID != "" && result.SandboxID != "local" {
			continue
		}
		remember(result.Path, result.Version)
		for _, m := range result.Matches {
			remember(m.Path, m.Version)
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	if len(order) > 64 {
		order = order[len(order)-64:]
	}
	type revision struct {
		Version string `json:",omitempty"`
		State   string
	}
	current := map[string]revision{}
	for _, p := range order {
		root, name, err := f.resolve(ctx, r.Execution, tools.FileTarget{Path: p}, policy.Read)
		if err != nil {
			if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
				current[p] = revision{State: "inaccessible"}
				continue
			}
			return nil, err
		}
		file, err := openResourceFile(root, name)
		if err != nil {
			if core.CodeOf(err) == core.NotFound || core.CodeOf(err) == core.Forbidden {
				current[p] = revision{State: "missing_or_inaccessible"}
				continue
			}
			return nil, err
		}
		version, err := resourceFileVersion(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		state := "current"
		if version != versions[p] {
			state = "changed; discard earlier text and read again"
		}
		current[p] = revision{version, state}
	}
	data, _ := json.Marshal(struct {
		Files map[string]revision
		Rule  string
	}{current, "Only these recent paths were rechecked. Before relying on older unlisted references, read the current file. Changed or missing paths invalidate prior snippets and bodies; never guess a renamed location."})
	return []agent.ContextDocument{{Kind: "memory", Source: "read_memory_file_revisions", Scope: "assistant", Revision: sourceRevision(string(data)), Content: string(data)}}, nil
}
