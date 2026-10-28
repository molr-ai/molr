package app

import (
	"context"
	"log/slog"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/im/feishu"
	"molr.ai/runtime/internal/im/qq"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/im/telegram"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/shellsession"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/internal/storage/sqlite"
	"path/filepath"
	"time"
)

// Bundle is the complete single-server composition. Bootstrap and Database are
// local CLI capabilities and never registered as HTTP routes or Agent tools.
type Bundle struct {
	channelStatus func(context.Context, core.Principal, core.ID) (any, error)
	dingtalk      *dingtalk.Service
	feishu        *feishu.Service
	qq            *qq.Service
	weixin        *weixin.Service
	telegram      *telegram.Service
	slack         *slack.Service
	remote        *sandboxRouter
	shellTasks    *shellsession.Manager
	watchRuntime  *watchRuntime
	sandbox       *shellService
	Application   Application
	Services      api.Services
	Bootstrap     auth.Bootstrap
	Database      sqlite.Database
	HTTP          api.Server
	Pricing       *pricing.Catalog
	priceUpdater  *pricing.Updater
	Exchange      *pricing.Exchange
	Trace         *debugtrace.Service
	store         persistence.Store
	objects       *files.ObjectStore
}

func (b *Bundle) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var first error
	if b.Application != nil {
		first = b.Application.Shutdown(ctx)
	}
	if b.watchRuntime != nil {
		b.watchRuntime.close()
	}
	if b.shellTasks != nil {
		b.shellTasks.Close()
	}
	if b.sandbox != nil {
		if err := b.sandbox.runner.Close(); first == nil {
			first = err
		}
	}
	if b.objects != nil {
		if err := b.objects.Close(); first == nil {
			first = err
		}
	}
	if b.remote != nil {
		b.remote.remoteRuntime.Close()
	}
	if b.Database != nil {
		if err := b.Database.Close(); first == nil {
			first = err
		}
	}
	return first
}

type BuildOptions struct {
	RequestLogger *slog.Logger
}

// Build orders construction and owns cleanup until the complete bundle is returned.
func Build(ctx context.Context, cfg config.Config, options ...BuildOptions) (bundle *Bundle, err error) {
	var requestLogger *slog.Logger
	if len(options) > 0 {
		requestLogger = options[0].RequestLogger
	}
	maintenance := newMaintenance(cfg.ConfigPath, cfg.Browser.AgentDirectory != "" || cfg.Browser.ChromiumDirectory != "")
	cfg.Sandbox.ProtectedRoots = maintenance.ProtectedRoots()
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	if err = prepareDirectory(cfg.DataDir); err != nil {
		return nil, err
	}
	key, err := masterKey(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	db, err := sqlite.Open(filepath.Join(cfg.DataDir, "runtime.db"))
	if err != nil {
		return nil, err
	}
	b := &Bundle{Database: db, store: db.Store()}
	defer func() {
		if err != nil {
			_ = b.Close()
		}
	}()
	if err = db.Migrate(ctx); err != nil {
		return nil, err
	}
	store := b.store
	access := access{store}
	wiring := &buildWiring{}
	identity, err := buildIdentity(store, cfg, key, wiring)
	if err != nil {
		return nil, err
	}
	b.Bootstrap = identity
	if err = b.buildPricing(ctx, cfg); err != nil {
		return nil, err
	}
	permissions, err := b.buildPolicy(ctx, cfg, identity, wiring)
	if err != nil {
		return nil, err
	}
	adapters := adapters{store: store, policy: permissions, dataDir: cfg.DataDir, networkConfig: cfg.Network, storageConfig: cfg.Sandbox, ownPort: cfg.OwnListenPort(), pricing: b.Pricing}
	vault, oauth, err := buildCredentials(adapters, identity, key, wiring)
	if err != nil {
		return nil, err
	}
	adapters.broker = vault
	auditService, err := audit.NewService(store, audit.Options{ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope})
	if err != nil {
		return nil, err
	}
	chats, err := b.buildConversations(ctx, cfg, key, permissions)
	if err != nil {
		return nil, err
	}
	sandbox, err := b.buildSandbox(cfg, key, adapters, permissions, identity, vault, auditService, maintenance)
	if err != nil {
		return nil, err
	}
	wiring.router = sandbox.router
	planning := buildPlanning(cfg, adapters, chats.chat)
	buildExtensions(adapters, permissions, identity, vault, planning.plans, wiring)
	sandbox.grants = mcpGrants{Service: sandbox.grants, mcp: wiring.mcp}
	toolset, err := b.buildTools(cfg, adapters, permissions, vault, chats, planning, sandbox, wiring)
	if err != nil {
		return nil, err
	}
	wiring.validateReferences = func(ctx context.Context, p core.Principal, refs []auth.ResourceReference) error {
		return validateCatalogReferences(ctx, adapters, sandbox.router, toolset.executor, p, refs)
	}
	root := filepath.Dir(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, "owner", "assistant"))
	if err = prepareDirectory(root); err != nil {
		return nil, err
	}
	execution := b.buildAgent(cfg, adapters, permissions, chats.chat, planning, sandbox, toolset, wiring)
	engine, assistants := execution.engine, execution.assistants
	wiring.resetContext = engine.ResetContext
	worker, schedules := b.buildScheduler(cfg, permissions, chats.chat, planning, sandbox.router, engine)
	preferences := buildSettings(cfg, store, identity, permissions, chats.files, planning)
	b.Services = api.Services{
		Dreams: planning.dreams, Plans: planning.plans, Watches: planning.watches, Debug: b.Trace, Exchange: b.Exchange,
		Auth: identity, Browser: identity, Audit: auditService, Assistants: assistants, Conversations: chats.chat,
		Tasks: planning.tasks, Todos: todo.NewService(store, todo.Options{Authorizer: permissions, CheckAssistant: access.assistant}),
		Schedules: schedules, Resources: sandbox.resources, Memories: planning.memories, Settings: preferences,
		Models: toolset.model, Credentials: vault, Grants: sandbox.grants, Budget: permissions, Agent: engine,
		Jobs: worker, Operations: toolset.operations, Search: planning.searching, Index: planning.indexer, Files: chats.files,
		Realtime: realtime.NewFeed(realtime.NewStore(store), realtime.Options{Authorizer: permissions, WatchAuthorization: func(p core.Principal) (realtime.AuthorizationWatch, error) { return identity.WatchPrincipal(p) }}),
	}
	updateService, err := b.buildHTTP(cfg, adapters, identity, permissions, oauth, maintenance, toolset, wiring, requestLogger)
	if err != nil {
		return nil, err
	}
	if err = b.buildApplication(cfg, identity, auditService, chats.router, updateService, wiring, execution.recoverClones, worker); err != nil {
		return nil, err
	}
	return b, nil
}
