package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/textfiles"
)

const directoryReferencePrefix = "path_"

func withinDirectory(root, name string) bool {
	if root == "" || name == "" {
		return false
	}
	root, name = filepath.Clean(textfiles.SystemPath(root)), filepath.Clean(textfiles.SystemPath(name))
	rel, err := filepath.Rel(root, name)
	if err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return true
	}
	// Case-insensitive filesystems may spell the same ancestor differently.
	parentInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	for {
		info, err := os.Stat(name)
		if err == nil && os.SameFile(parentInfo, info) {
			return true
		}
		parent := filepath.Dir(name)
		if parent == name {
			return false
		}
		name = parent
	}
}

// A configured directory is a root boundary, including its descendants. Every
// use rechecks current deployment policy; a saved reference is not a grant.
func directoryPolicy(s config.Sandbox, root string) (bool, error) {
	allowed := !s.RestrictDirectories
	readOnly := false
	for _, d := range s.Directories {
		if withinDirectory(d.Path, root) {
			allowed = true
			readOnly = readOnly || d.ReadOnly
		}
	}
	if !allowed {
		return false, core.WithReason(core.NewError(core.Forbidden, "directory is outside deployment scope"), "storage_disabled")
	}
	return readOnly, nil
}

func resolveSandboxDirectory(ctx context.Context, a adapters, q sm.DirectoryRequest) (sm.Directory, error) {
	root := q.Path
	if q.SourceID != "" {
		root = ""
		for _, d := range a.storageConfig.Directories {
			if directoryID(d.Path) == q.SourceID {
				root = d.Path
				break
			}
		}
		if root == "" {
			return sm.Directory{}, denied()
		}
		if q.Path != "" && q.Path != "." {
			if !filepath.IsLocal(q.Path) || filepath.Clean(q.Path) != q.Path {
				return sm.Directory{}, core.Invalid("path", "clean relative subdirectory required")
			}
			root = filepath.Join(root, q.Path)
		}
	} else if strings.HasPrefix(root, "~/") {
		// Expansion is deliberately on the selected sandbox machine.
		home, err := os.UserHomeDir()
		if err != nil {
			return sm.Directory{}, err
		}
		root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || len(root) > 4096 {
		return sm.Directory{}, core.Invalid("path", "absolute directory path required")
	}
	ro, err := directoryPolicy(a.storageConfig, root)
	if err != nil {
		return sm.Directory{}, err
	}
	if _, err = a.providers()["local-directory"].Validate(ctx, resource.Binding{RootPath: root}); err != nil {
		return sm.Directory{}, err
	}
	ref := q
	// Save resolved absolute paths, not environment-dependent home aliases.
	if ref.SourceID == "" {
		ref.Path = root
	}
	id := q.SourceID
	if id == "" || (q.Path != "" && q.Path != ".") {
		raw, _ := json.Marshal(ref)
		id = directoryReferencePrefix + base64.RawURLEncoding.EncodeToString(raw)
	}
	return sm.Directory{Capacity: platform.FilesystemCapacity(root, 0), ID: id, Name: filepath.Base(root), ReadOnly: ro, Path: root}, nil
}

func directoryReference(id string) (sm.DirectoryRequest, error) {
	if !strings.HasPrefix(id, directoryReferencePrefix) {
		return sm.DirectoryRequest{SourceID: id}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, directoryReferencePrefix))
	if err != nil || len(raw) > 8192 {
		return sm.DirectoryRequest{}, denied()
	}
	var q sm.DirectoryRequest
	if err = decodeSandboxArgs(raw, &q); err != nil {
		return q, err
	}
	if q.SourceID == "" && q.Path == "" {
		return q, denied()
	}
	return q, nil
}
func (b *remoteSandboxBackend) Directory(ctx context.Context, q sm.DirectoryRequest) (sm.Directory, error) {
	if !b.cfg.Sandbox.Enabled {
		return sm.Directory{}, core.NewError(core.Unavailable, "sandbox disabled")
	}
	return resolveSandboxDirectory(ctx, b.files.adapters, q)
}
func (b *remoteSandboxBackend) directory(ctx context.Context, id string) (sm.Directory, error) {
	q, err := directoryReference(id)
	if err != nil {
		return sm.Directory{}, err
	}
	return b.Directory(ctx, q)
}
