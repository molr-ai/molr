package platform

import (
	"path/filepath"
	"testing"
)

func TestAssistantWorkspaceIdentityAndOverrides(t *testing.T) {
	data := t.TempDir()
	root := filepath.Join(data, "sandbox", "work")
	base := AssistantWorkspace(data, "", "owner", "assistant")
	if filepath.Dir(base) != root || len(filepath.Base(base)) != 64 || base != AssistantWorkspace(data, root, "owner", "assistant") {
		t.Fatal("default and explicit workspace roots differ")
	}
	if base == AssistantWorkspace(data, "", "other", "assistant") || base == AssistantWorkspace(data, "", "owner", "other") {
		t.Fatal("workspace identities collide")
	}
	if got := AssistantWorkspace(data, t.TempDir(), "owner", "assistant"); got == base || filepath.Base(got) != filepath.Base(base) {
		t.Fatal("override changed workspace identity")
	}
}

func TestUserGlobalOwnerIdentity(t *testing.T) {
	root := t.TempDir()
	one := UserGlobal(root, "owner")
	if one == UserGlobal(root, "other") || filepath.Base(one) != "global" || filepath.Dir(filepath.Dir(one)) != filepath.Join(root, "users") {
		t.Fatal("global root must be owner scoped")
	}
	if one != UserGlobal(root, "owner") {
		t.Fatal("unstable global root")
	}
}
