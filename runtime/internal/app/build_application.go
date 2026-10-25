package app

import (
	"context"
	"log"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/sqlite"
	"molr.ai/runtime/internal/updates"
	"path/filepath"
)

func (b *Bundle) buildApplication(cfg config.Config, identity *auth.Manager, auditService audit.Service, channels *chatChannels, updates *updates.Service, wiring *buildWiring, recoverClones func(context.Context) error, worker scheduler.Service) error {
	store := b.store
	var err error
	restore, err := sqlite.NewRestorer(filepath.Join(cfg.DataDir, "runtime.db"))
	if err != nil {
		return err
	}
	b.Application, err = NewApplication(Dependencies{Config: cfg, HTTP: b.HTTP, Auth: identity, Bootstrap: identity, Audit: auditService, Database: b.Database, Restore: restore, Scheduler: worker, Locker: platform.NewLocker()})
	if err != nil {
		return err
	}
	if b.sandbox != nil {
		b.Application.(*Runtime).prepareSandbox = b.sandbox.startup
	}
	b.Application.(*Runtime).recoverCalls = func(ctx context.Context) error {
		if err := recoverClones(ctx); err != nil {
			log.Printf("assistant clone recovery incomplete: %v", err)
		}
		if err := recoverCallUsage(ctx, store); err != nil {
			return err
		}
		if err := wiring.mcp.Recover(ctx); err != nil {
			return err
		}
		return wiring.plugins.RecoverHTTP(ctx)
	}
	b.Application.(*Runtime).collectAuth = identity.RunGC
	b.Application.(*Runtime).collectContexts = func(ctx context.Context) { contextstore.RunGC(ctx, store) }
	b.Application.(*Runtime).runChannels = channels.Run
	b.Application.(*Runtime).checkUpdates = updates.Run
	b.Application.(*Runtime).refreshPrices = b.priceUpdater.Run
	b.Application.(*Runtime).startShellTasks = b.shellTasks.Start
	b.Application.(*Runtime).stopShellTasks = b.shellTasks.Close
	return nil
}
