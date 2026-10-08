package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// AssistantWorkspace resolves a trusted runtime-managed workspace. Callers must
// authorize the execution identity before use; neither root comes from a tool.
// File tools and command runners deliberately share the same persistent path.
func AssistantWorkspace(dataRoot, workRoot string, owner, assistant core.ID) string {
	if workRoot == "" {
		workRoot = filepath.Join(dataRoot, "sandbox", "work")
	}
	sum := sha256.Sum256([]byte(string(owner) + "\x00" + string(assistant)))
	return filepath.Join(workRoot, hex.EncodeToString(sum[:]))
}

// UserGlobal contains one owner's shared instructions, skills and memories.
// Only the trusted Runtime chooses the owner; sandbox tools see it read-only.
func UserGlobal(dataRoot string, owner core.ID) string {
	sum := sha256.Sum256([]byte(string(owner)))
	return filepath.Join(dataRoot, "users", hex.EncodeToString(sum[:]), "global")
}

// WorkspaceStateDirectory holds execution records following the XDG state directory convention.
const WorkspaceStateDirectory = ".local/state/molr"

// WorkspaceDirectories is the default layout. Business folders remain conventions.
func WorkspaceDirectories() []string {
	return []string{"inbox", "projects", "outputs", ".agents/skills", ".agents/memory", ".agents/reviews", WorkspaceStateDirectory, ".cache"}
}

// EnsureWorkspace initializes only missing directories, using confined descriptor
// operations for all descendants. Existing files and instructions are never seeded
// or overwritten. Callers must authorize the assistant before invoking it.
func EnsureWorkspace(root string) error {
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	parent, err := textfiles.OpenDirectory(filepath.Dir(root), ".")
	if err != nil {
		return err
	}
	parent.Close()
	dir, err := textfiles.OpenDirectory(root, ".")
	if err == nil {
		dir.Close()
	} else {
		if core.CodeOf(err) != core.NotFound {
			return err
		}
		if err := textfiles.MkdirAll(filepath.Dir(root), filepath.Base(root)); err != nil {
			return err
		}
	}
	for _, name := range WorkspaceDirectories() {
		dir, err := textfiles.OpenDirectory(root, name)
		if err == nil {
			dir.Close()
			continue
		}
		if core.CodeOf(err) != core.NotFound {
			return err
		}
		if err := textfiles.MkdirAll(root, name); err != nil {
			return err
		}
	}
	return nil
}
