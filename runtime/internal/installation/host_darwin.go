package installation

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
)

func hostLabel() string                                     { return "macOS" }
func preferredInterface() int                               { return 0 }
func missingDependencies() []string                         { return nil }
func (e *Engine) prepareDependencies(bool) error            { return nil }
func (e *Engine) preparePolicy(*installrecord.Record) error { return nil }
func (e *Engine) prepareBrowserDependencies(*installrecord.Record) error {
	return nil
}
func (e *Engine) checkServiceHost(r *installrecord.Record, sandbox bool) (bool, error) {
	if r.System || r.UID != os.Geteuid() {
		return false, fmt.Errorf("macOS requires the current user")
	}
	_, err := e.run([]string{"/bin/launchctl", "print", fmt.Sprintf("user/%d", r.UID)}, true)
	return false, err
}

// Detached workers inherit the instance lock. Task reconciliation tests that
// lock before consulting this hook; there is no launchd unit to inspect.
func unitActive(context.Context, *installrecord.Record, string) bool { return false }
