package platform

import "runtime"

// NativeSandbox selects the native Seatbelt execution contract. Capabilities still
// report whether startup probes succeeded; this function is not an availability check.
func NativeSandbox() bool { return runtime.GOOS == "darwin" }

// ControlledReady checks the advertised backend contract without claiming Linux
// process namespaces or hard quotas for native macOS execution.
func (c Capabilities) ControlledReady() bool {
	if c.Reason != "available" || !c.Shell || !c.FileIsolation || !c.NetworkIsolation {
		return false
	}
	return (c.OS == "darwin" && c.NetworkBackend == "seatbelt-toggle") || (c.ProcessIsolation && c.ResourceLimits)
}
