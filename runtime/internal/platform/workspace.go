package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"molr.ai/runtime/internal/core"
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
