//go:build (!linux && !darwin) || (linux && !amd64 && !arm64)

package sandbox

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func (r *Runner) check() error                  { return unavailable("sandbox_platform_unsupported") }
func (r *Runner) prepare(context.Context) error { return unavailable("sandbox_platform_unsupported") }
func (r *Runner) recover() error                { return unavailable("sandbox_platform_unsupported") }
func (r *Runner) start(context.Context, core.Execution, platform.Command) (*process, error) {
	return nil, unavailable("sandbox_platform_unsupported")
}
func HelperMain([]string) int { return 125 }
func SupervisorMain() int     { return 125 }
