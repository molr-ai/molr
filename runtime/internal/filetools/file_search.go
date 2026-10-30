package filetools

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
	"path"
	"strings"
)

func (f *Tools) Search(ctx context.Context, e core.Execution, r tools.FileSearchRequest) (json.RawMessage, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if f.options.Remote != nil {
		result, remote, err := f.options.Remote(ctx, e, "file_search", r.FileTarget, r, "")
		if remote || err != nil {
			return result, err
		}
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	if err := f.begin(ctx, e, r.FileTarget); err != nil {
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
	authorize := func(child string) error {
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
	}
	var result textfiles.SearchResult
	if root == BuiltinRoot {
		result, err = textfiles.SearchFS(ctx, molrdocs.FS, molrdocs.Revision, name, r.SearchOptions(), authorize)
		if err != nil {
			return nil, builtinFileError(err)
		}
	} else {
		result, err = textfiles.Search(ctx, root, name, r.SearchOptions(), authorize)
	}
	if err != nil {
		return nil, err
	}
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range result.Matches {
		result.Matches[i].Path = externalPath(result.Matches[i].Path)
		if !seen[result.Matches[i].Path] {
			target := r.FileTarget
			target.Path = result.Matches[i].Path
			if _, err = ObserveRead(ctx, e, target); err != nil {
				return nil, err
			}
			seen[target.Path] = true
		}
	}
	outputLimit := 32 << 10
	if IsBuiltin(r.FileTarget) {
		outputLimit = 12 << 10
	}
	sandboxID := r.SandboxID
	if sandboxID == "" {
		sandboxID = "local"
	}
	for {
		encoded, err := json.Marshal(struct {
			SandboxID  core.ID
			ResourceID core.ID
			Path       string
			textfiles.SearchResult
		}{sandboxID, r.ResourceID, r.Path, result})
		if err != nil || len(encoded) <= outputLimit || len(result.Matches) == 0 {
			return encoded, err
		}
		result.Matches = result.Matches[:len(result.Matches)-1]
		result.Complete, result.Truncated, result.StopReason = false, true, "output_limit"
	}
}
