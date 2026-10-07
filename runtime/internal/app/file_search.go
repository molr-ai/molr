package app

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
)

func (f *resourceFileTools) search(ctx context.Context, e core.Execution, r tools.FileSearchRequest) (json.RawMessage, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if f.router != nil {
		result, remote, err := f.router.file(ctx, e, "file_search", r.FileTarget, r, "")
		if remote || err != nil {
			return result, err
		}
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	externalPath := func(child string) string {
		suffix := strings.TrimPrefix(strings.TrimPrefix(child, name), "/")
		if name == "." {
			suffix = child
		}
		if suffix == "" || suffix == "." {
			return r.Path
		}
		return path.Join(r.Path, suffix)
	}
	result, err := textfiles.Search(ctx, root, name, r.SearchOptions(), func(child string) error {
		target := r.FileTarget
		target.Path = externalPath(child)
		resolved, resolvedName, err := f.resolve(ctx, e, target, policy.Read)
		if err != nil {
			return err
		}
		if resolved != root || resolvedName != child {
			return core.NewError(core.Conflict, "search root changed")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	for i := range result.Matches {
		result.Matches[i].Path = externalPath(result.Matches[i].Path)
	}
	for {
		encoded, err := json.Marshal(struct {
			SandboxID  core.ID
			ResourceID core.ID
			Path       string
			textfiles.SearchResult
		}{targetSandbox(r.SandboxID), r.ResourceID, r.Path, result})
		if err != nil || len(encoded) <= 32<<10 || len(result.Matches) == 0 {
			return encoded, err
		}
		result.Matches = result.Matches[:len(result.Matches)-1]
		result.Complete, result.Truncated, result.StopReason = false, true, "output_limit"
	}
}
