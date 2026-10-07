package resourcequota

import (
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceQuotaFailsClosedWithoutProvisioning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing")
	if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := WorkspaceStatus(root)
	if err == nil && status.Supported {
		t.Skip("fixture inherits a configured project quota")
	}
	if _, err = ApplyWorkspace(root, policy.StorageConstraints{CapacityBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxFiles: 100}); err == nil {
		t.Fatal("unprovisioned quota reported applied")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "preserve" {
		t.Fatal("quota failure damaged data", err)
	}
}
