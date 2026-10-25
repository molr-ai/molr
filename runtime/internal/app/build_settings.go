package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

func buildSettings(cfg config.Config, store persistence.Store, identity *auth.Manager, permissions policy.Authorizer, fileService files.Service, planning planningServices) settings.Service {
	access := access{store}
	memoryFiles, memories := planning.memoryFiles, planning.memories
	preferences := settings.NewService(store, settings.Options{WorkbenchAvailable: cfg.Debug.Enabled, Authorizer: permissions, CheckVerification: identity.CheckVerification, SaveExport: func(ctx context.Context, p core.Principal, bundle settings.ExportBundle) (core.ID, error) {
		return saveExport(ctx, fileService, p, bundle, memoryFiles)
	}, PrepareMemoryCleanup: func(ctx context.Context, p core.Principal, ref memory.Ref) (string, error) {
		m, err := memories.Get(ctx, p, ref)
		return m.Version, err
	},
		CleanupMemory: func(ctx context.Context, p core.Principal, ref memory.Ref, version, key string) error {
			return memories.Delete(ctx, p, memory.DeleteRequest{Ref: ref, Forget: true, Mutation: memory.Mutation{IdempotencyKey: key, ExpectedVersion: version}})
		}, CleanupObject: func(ctx context.Context, p core.Principal, o core.Object, apply bool) error {
			return cleanupObject(ctx, store, access, p, o, apply)
		}})
	return preferences
}
