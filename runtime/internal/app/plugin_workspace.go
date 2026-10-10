package app

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

func (f *resourceFileTools) pluginWorkspaceRoot(ctx context.Context, p core.Principal, id core.ID, write bool) (string, error) {
	if err := (access{f.adapters.store}).assistant(ctx, p, id); err != nil {
		return "", err
	}
	reason, err := f.workspaceAvailability(ctx, core.Execution{Principal: p, AssistantID: id})
	if err != nil {
		return "", err
	}
	if reason != "" {
		return "", core.NewError(core.Unavailable, "assistant workspace unavailable")
	}
	root := platform.AssistantWorkspace(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, p.OwnerID, id)
	if write {
		spec, err := workspaceConstraints(ctx, f.adapters.store, p.OwnerID, id, "local")
		if err != nil {
			return "", err
		}
		if err = applyWorkspaceQuota(root, spec, f.adapters.storageConfig.WorkspaceProjectQuotas); err != nil {
			return "", err
		}
	}
	if err = platform.EnsureWorkspace(root); err != nil {
		return "", err
	}
	return root, nil
}

func (f *resourceFileTools) readPluginFile(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
	if f.plugins == nil || r.ResourceID != "" || r.FullAccess || (r.SandboxID != "" && r.SandboxID != "local") {
		return nil, denied()
	}
	if err := (access{f.adapters.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	if err := checkFilePolicy(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	reason, err := f.workspaceAvailability(ctx, e)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, denied()
	}
	body, digest, err := f.plugins.ResolvePackageFile(ctx, e, r.Path)
	if err != nil {
		return nil, err
	}
	offset, limit := r.Offset, r.Limit
	if offset == 0 {
		offset = 1
	}
	if limit == 0 {
		limit = 200
	}
	if offset < 1 || offset > 1_000_000_000 || limit < 1 || limit > 2000 {
		return nil, core.Invalid("range", "invalid read range")
	}
	text := ""
	start, end := 0, 0
	truncated := false
	nextByte := int64(0)
	if r.ByteOffset != nil {
		pos := *r.ByteOffset
		size := r.MaxBytes
		if size == 0 {
			size = 4096
		}
		if pos < 0 || size < 1 || size > 4096 || pos < int64(len(body)) && !utf8.RuneStart(body[pos]) {
			return nil, core.Invalid("range", "invalid UTF-8 byte range")
		}
		if pos < int64(len(body)) {
			tail := body[pos:]
			if len(tail) > size {
				tail = tail[:size]
				for !utf8.ValidString(tail) {
					tail = tail[:len(tail)-1]
				}
				truncated = true
			}
			text = tail
		}
		nextByte = pos + int64(len(text))
	} else {
		lines := strings.SplitAfter(body, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for i := offset - 1; i < len(lines); i++ {
			if i-offset+1 == limit || len(text)+len(lines[i]) > fileOutputLimit {
				truncated = true
				break
			}
			if start == 0 {
				start = i + 1
			}
			end = i + 1
			text += lines[i]
		}
	}
	// A final authority check prevents revocation during a read being presented as success.
	if _, _, err = f.plugins.ResolvePackageFile(ctx, e, r.Path); err != nil {
		return nil, err
	}
	if err = observeFileRead(ctx, e, r.FileTarget); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		SandboxID                         string
		Path, Version, PluginDigest, Text string
		StartLine, EndLine                int
		Truncated                         bool
		NextByteOffset                    int64
	}{"local", r.Path, digest, digest, text, start, end, truncated, nextByte})
}

func (f *resourceFileTools) checkPluginFileSize(ctx context.Context, p core.Principal, id core.ID, size int64) error {
	spec, err := workspaceConstraints(ctx, f.adapters.store, p.OwnerID, id, "local")
	if err != nil {
		return err
	}
	if spec != nil && size > spec.MaxFileBytes {
		return core.WithReason(core.NewError(core.FailedPrecondition, "workspace single-file quota exceeded"), "storage_file_limit")
	}
	return nil
}
