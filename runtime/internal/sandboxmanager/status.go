package sandboxmanager

import (
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/platform"
)

// RuntimeStatus exposes the authorized local capabilities and configured resources.
type RuntimeStatus struct {
	Enabled      bool
	Storage      bool
	Capabilities platform.Capabilities
	Networks     []config.SandboxNetwork
	Directories  []config.SandboxDirectory
}
