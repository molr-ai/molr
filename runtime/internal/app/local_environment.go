package app

import (
	"context"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"path/filepath"
	"runtime"
)

// Agent identity and all automatic sources live on this Runtime. Compute
// readiness is deliberately separate: an unavailable shell does not block chat.
func localEnvironment(cfg config.Config) func(context.Context) error {
	root := filepath.Dir(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, "owner", "assistant"))
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !cfg.Sandbox.Enabled || runtime.GOOS != "linux" {
			return core.WithReason(core.NewError(core.Unavailable, "local file environment unavailable"), "local_environment_unavailable")
		}
		dir, err := openResourceDirectory(root, ".")
		if err != nil {
			return core.WithReason(core.NewError(core.Unavailable, "local workspace volume unavailable"), "local_environment_unavailable")
		}
		dir.Close()
		return nil
	}
}
