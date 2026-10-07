//go:build !linux || (!amd64 && !arm64)

package resourcequota

import (
	"fmt"
	"molr.ai/runtime/internal/policy"
)

type WorkspaceUsage struct {
	Supported                                 bool
	Reason                                    string
	UsedBytes, CapacityBytes, Files, MaxFiles int64
}

func WorkspaceStatus(string) (WorkspaceUsage, error) {
	return WorkspaceUsage{Reason: "workspace_quota_unsupported"}, fmt.Errorf("workspace project quotas require Linux")
}
func ApplyWorkspace(string, policy.StorageConstraints) (WorkspaceUsage, error) {
	return WorkspaceStatus("")
}
