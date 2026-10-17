package config

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// SandboxPreset expands a deployment-time choice into ordinary editable settings.
// The choice itself is never persisted or consulted by the running service.
func SandboxPreset(name string) (Sandbox, error) {
	s := sandboxDefaults()
	s.Directories = []SandboxDirectory{}
	s.Networks = []SandboxNetwork{}
	s.NetworkPresets = []string{}
	s.RestrictDirectories = true
	s.Enabled = false
	s.AllowFullAccess = false
	switch name {
	case "least-privilege":
	case "permissive":
		s.Enabled = true
		s.HostTools = true
		s.RestrictDirectories = false
		s.AllowFullAccess = true
		if platform.NativeSandbox() {
			s.NetworkPresets = []string{"network"}
		} else {
			s.NetworkPresets = []string{"internet", "lan", "host"}
			s.AuthorizationCeilings = platform.Limits{}
			s.GPU = platform.GPUConfig{Enabled: true, AllDevices: true}
		}
	default:
		return Sandbox{}, core.Invalid("resource_preset", "permissive or least-privilege required")
	}
	return s, nil
}
