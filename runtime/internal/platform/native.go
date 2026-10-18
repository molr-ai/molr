package platform

import "runtime"

// NativeSandbox selects native file scopes and a network toggle. Capabilities still
// report whether startup probes succeeded; this function is not an availability check.
func NativeSandbox() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

func NativeNetworkBackend() string {
	switch runtime.GOOS {
	case "darwin":
		return "seatbelt-toggle"
	case "windows":
		return "appcontainer-toggle"
	default:
		return ""
	}
}

// NetworkToggle describes authorization semantics, not backend readiness.
func (c Capabilities) NetworkToggle() bool {
	return c.NetworkBackend == "seatbelt-toggle" || c.NetworkBackend == "appcontainer-toggle"
}

// ControlledReady checks the advertised backend contract without claiming Linux
// process namespaces or hard quotas for native macOS/Windows execution.
func (c Capabilities) ControlledReady() bool {
	if c.Reason != "available" || !c.Shell || !c.FileIsolation || !c.NetworkIsolation {
		return false
	}
	return (c.OS == "darwin" && c.NetworkBackend == "seatbelt-toggle") ||
		(c.OS == "windows" && c.NetworkBackend == "appcontainer-toggle") ||
		(c.ProcessIsolation && c.ResourceLimits)
}
