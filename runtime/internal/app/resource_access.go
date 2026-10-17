package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/textfiles"
	"path"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

// Resource paths live in a resource-local namespace, never the host root.
func resourcePath(name string) (string, error) {
	p := "/" + strings.TrimPrefix(name, "/")
	if name == "" || path.Clean(p) != p || strings.ContainsAny(p, "\x00\\") {
		return "", core.Invalid("path", "canonical resource-relative path required")
	}
	return p, nil
}

func resourceShellPath(id core.ID, name string) string {
	sum := sha256.Sum256([]byte(id))
	return path.Join("/resources/r_"+hex.EncodeToString(sum[:16]), name)
}

// Both file tools and Shell resolve storage through this authorization path.
// The OS confinement layer still validates the actual filesystem at use time.
func (a adapters) storageBinding(ctx context.Context, p core.Principal, id core.ID, name string, action policy.Action) (resource.Binding, error) {
	destination, err := resourcePath(name)
	if err != nil {
		return resource.Binding{}, err
	}
	if err = a.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: action, Destination: destination}); err != nil {
		return resource.Binding{}, err
	}
	r, b, err := a.binding(ctx, p, id)
	if err != nil {
		return b, err
	}
	if r.Provider == "managed-workspace" && sandboxOf(r) == "local" {
		b.RootPath = platform.AssistantWorkspace(a.dataDir, a.storageConfig.WorkRoot, p.OwnerID, p.SubjectID)
		return b, nil
	}
	if r.Kind != resource.Storage || r.Provider != "local-directory" {
		return b, core.Invalid("resource", "directory resource required")
	}
	if _, err = a.providers()["local-directory"].Validate(ctx, b); err != nil {
		return b, err
	}
	readOnly, err := directoryPolicy(a.storageConfig, b.RootPath)
	if err != nil {
		return b, err
	}
	if action != policy.Read {
		for _, d := range a.storageConfig.Directories {
			if d.ReadOnly && textfiles.Overlaps(d.Path, filepath.Join(b.RootPath, strings.TrimPrefix(destination, "/"))) {
				readOnly = true
			}
		}
		if readOnly {
			return b, core.WithReason(core.NewError(core.Forbidden, "storage is read-only by deployment"), "storage_read_only")
		}
	}

	return b, nil
}

// Workspace paths use the same visible /work namespace as Shell. They never
// select another assistant or an arbitrary host path.
func workspaceRelativePath(name string) (string, error) {
	if name == "." || name == "/work" {
		return "", nil
	}
	if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\x00\\") {
		return "", core.Invalid("path", "workspace path required")
	}
	if strings.HasPrefix(name, "/") {
		if !strings.HasPrefix(name, "/work/") {
			return "", core.Invalid("path", "only /work is available without a resource ID")
		}
		name = strings.TrimPrefix(name, "/work/")
	}
	if strings.HasPrefix(name, "/") || name == "." || name == ".." || strings.HasPrefix(name, "../") || path.Clean(name) != name {
		return "", core.Invalid("path", "confined workspace path required")
	}
	return name, nil
}
