package sandbox

import (
	"context"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"runtime"
	"slices"
	"strings"
)

// Direct execution requires deployment opt-in and per-call authorization. It never serves
// as a fallback for a failed isolated backend.
func (r *Runner) checkMode(full bool) error {
	if full {
		return r.checkDirect()
	}
	return r.check()
}
func (r *Runner) validateMode(c platform.Command) error {
	if r.options.NoWorkspace && (c.Directory == "/work" || strings.HasPrefix(c.Directory, "/work/")) {
		return core.Invalid("directory", "remote computers have no assistant workspace")
	}
	if !c.FullAccess {
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			return validateSeatbelt(c)
		}
		return validate(c)
	}
	if !r.options.AllowFullAccess {
		return core.NewError(core.Forbidden, "Full Access is disabled by deployment")
	}
	if c.BrowserAssets || c.BrowserProxy != nil {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			return core.Invalid("browser", "unsupported browser platform")
		}
		if !c.BrowserAssets || c.BrowserProxy == nil || !c.Managed || c.TTY || !slices.Equal(c.ForwardPorts, []int{browser.CommandPort, browser.StreamPort}) || r.options.NoWorkspace || c.Script != "exec /molr/helper sandbox-helper browser" {
			return core.Invalid("browser", "trusted browser launch required")
		}
		if len(c.Mounts) != 2 {
			return core.Invalid("browser", "read-only browser assets required")
		}
		assets := map[string]bool{}
		for _, mount := range c.Mounts {
			if !mount.ReadOnly || assets[mount.Destination] || (mount.Destination != "/resources/browser-agent" && mount.Destination != "/resources/browser-chromium") {
				return core.Invalid("browser", "read-only browser assets required")
			}
			assets[mount.Destination] = true
		}
		proxy := *c.BrowserProxy
		if err := proxy.Validate(); err != nil {
			return err
		}
	}
	if c.RequireIsolation {
		return core.Invalid("mode", "direct execution cannot provide isolation")
	}
	if c.Script == "" || len(c.Script) > 65536 || strings.ContainsRune(c.Script, 0) || strings.ContainsRune(c.Directory, 0) || c.Timeout < 0 || c.MaxOutputBytes < 1 || c.MaxOutputBytes > 128<<10 {
		return core.Invalid("command", "invalid direct command")
	}
	return platform.ValidateForwardPorts(c.ForwardPorts)
}
func (r *Runner) startMode(ctx context.Context, e core.Execution, c platform.Command) (*process, error) {
	if c.FullAccess {
		return r.startDirect(ctx, e, c)
	}
	return r.start(ctx, e, c)
}
