package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/updates"
	"molr.ai/runtime/internal/webassets"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/internal/storage/sqlite"
)

// Bundle is the complete single-server composition. Bootstrap and Database are
// local CLI capabilities and never registered as HTTP routes or Agent tools.
type Bundle struct {
	weixin      *weixin.Service
	remote      *sandboxRouter
	shellTasks  *shellTasks
	sandbox     *shellService
	Application Application
	Services    api.Services
	Bootstrap   auth.Bootstrap
	Database    sqlite.Database
	HTTP        api.Server
	Pricing     *pricing.Catalog
	Exchange    *pricing.Exchange
	Trace       *debugtrace.Service
	store       persistence.Store
	objects     *files.ObjectStore
}

func (b *Bundle) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var first error
	if b.Application != nil {
		first = b.Application.Shutdown(ctx)
	}
	if b.shellTasks != nil {
		b.shellTasks.close()
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
		b.remote.closeWaits()
	}
	if b.Database != nil {
		if err := b.Database.Close(); first == nil {
			first = err
		}
	}
	return first
}

// Build wires only fixed in-process adapters. Network access is constrained at
// the actual dial; credentials never enter model/tool-visible configuration.
func Build(ctx context.Context, cfg config.Config) (bundle *Bundle, err error) {
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
	store := db.Store()
	access := access{store}
	var validateReferences func(context.Context, core.Principal, []auth.ResourceReference) error
	var pluginService *plugins.Service
	identity, err := auth.NewService(store, auth.Options{AfterAccessChange: func(ctx context.Context) {
		if pluginService != nil {
			pluginService.RevalidateRunning(ctx)
		}
	}, ValidateResources: func(ctx context.Context, p core.Principal, r []auth.ResourceReference) error {
		if validateReferences == nil {
			return core.NewError(core.Unavailable, "resource catalog unavailable")
		}
		return validateReferences(ctx, p, r)
	}, MasterKey: key, LocalHTTP: cfg.HTTP.TLSMode == config.LocalHTTP, LANHTTP: cfg.HTTP.TLSMode == config.LANHTTP, Origin: strings.TrimSuffix(cfg.HTTP.PublicURL, "/"), AllowedOrigins: cfg.HTTP.AllowedOrigins, SessionLifetime: cfg.Auth.SessionLifetime.Value(), SessionIdle: cfg.Auth.SessionIdle.Value(), VerificationLifetime: cfg.Auth.VerificationLifetime.Value(), LoginAttemptsPerMinute: cfg.Auth.LoginAttemptsPerMinute, ValidateAssistant: access.liveAssistant})
	if err != nil {
		return nil, err
	}
	b.Bootstrap = identity
	if err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "system.catalog", "enabled", true)
	}); err != nil {
		return nil, err
	}
	var exchangeRules io.Reader
	if cfg.Pricing.ExchangeRatesFile != "" {
		file, openErr := os.Open(cfg.Pricing.ExchangeRatesFile)
		if openErr != nil {
			return nil, core.Wrap(core.InvalidArgument, "cannot open exchange rates", openErr)
		}
		defer file.Close()
		exchangeRules = file
	}
	b.Exchange, err = pricing.NewExchange(exchangeRules)
	if err != nil {
		return nil, err
	}
	var resetContext func(context.Context, core.Principal, core.ID, core.Mutation, uint64) error
	b.Trace = debugtrace.New(store, debugtrace.Options{ResetContext: func(ctx context.Context, p core.Principal, id core.ID, m core.Mutation, sequence uint64) error {
		if err := identity.CheckScope(ctx, p, "debug.write"); err != nil {
			return err
		}
		if resetContext == nil {
			return core.NewError(core.FailedPrecondition, "context reset unavailable")
		}
		return resetContext(ctx, p, id, m, sequence)
	}, Administrator: identity.RequireAdministrator, Enabled: cfg.Debug.Enabled, MaxRuns: cfg.Debug.MaxRuns, MaxPayloadBytes: cfg.Debug.MaxPayloadBytes, Authorize: func(ctx context.Context, p core.Principal) error { return identity.CheckScope(ctx, p, "debug.read") }})
	if traceErr := b.Trace.Recover(ctx); traceErr != nil {
		log.Printf("debug trace recovery unavailable: %s", core.CodeOf(traceErr))
	}
	var grantRouter *sandboxRouter
	permissions, err := policy.NewService(store, policy.Options{CheckGroupBudget: func(ctx context.Context, r policy.ReserveRequest) error {
		return checkGroupModelBudget(ctx, store, r, b.Exchange.Snapshot())
	}, CheckAccess: func(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
		return groupAccess(ctx, store, p, q)
	}, ObserveBudget: func(ctx context.Context, id core.ID, data any, err error) {
		state := "succeeded"
		if err != nil {
			state = "failed"
		}
		b.Trace.Record(ctx, "budget.decision", state, id, map[string]any{"Decision": data, "Code": core.CodeOf(err), "Reason": core.ReasonOf(err)})
	}, Exchange: b.Exchange.Snapshot, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, Owns: access.owns, ResourceActions: access.resourceActions, ValidateGrantScope: func(ctx context.Context, p core.Principal, id core.ID, scope policy.Scope) error {
		if err := access.grantScope(ctx, p, id, scope); err != nil {
			return err
		}
		if scope.FullAccess {
			r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, id)
			if err != nil {
				return err
			}
			if grantRouter == nil {
				return core.NewError(core.Unavailable, "sandbox authorization not ready")
			}
			if err := grantRouter.checkFullAccessCapability(ctx, p, r); err != nil {
				return err
			}
		}
		return nil
	}, ValidateConstraints: func(ctx context.Context, p core.Principal, id, assistantID core.ID, scope policy.Scope, c *policy.Constraints) error {
		if scope.FullAccess {
			return nil
		}
		if err := validateGroupConstraints(ctx, store, p, id, assistantID, c); err != nil {
			return err
		}
		return validateResourceConstraints(ctx, store, p, id, c, cfg.Sandbox.ComputeCeilings(), cfg.Sandbox.WorkspaceProjectQuotas)
	}, ValidateExecution: access.execution, ValidateSettlement: access.historicalExecution, VerifyReceipt: access.verifyReceipt, NoCharge: access.noCharge})
	if err != nil {
		return nil, err
	}
	var rules io.Reader
	if cfg.Pricing.RulesFile != "" {
		file, err := os.Open(cfg.Pricing.RulesFile)
		if err != nil {
			return nil, core.Wrap(core.InvalidArgument, "cannot open pricing rules", err)
		}
		defer file.Close()
		rules = file
	}
	b.Pricing, err = pricing.New(rules)
	if err != nil {
		return nil, err
	}
	adapters := adapters{store: store, policy: permissions, dataDir: cfg.DataDir, networkConfig: cfg.Network, storageConfig: cfg.Sandbox, ownPort: cfg.OwnListenPort(), pricing: b.Pricing}

	vault, err := credential.NewService(store, credential.Options{RequireAdministrator: identity.RequireAdministrator, MasterKey: key, ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, CheckVerification: identity.CheckVerification, AuthorizeAccess: func(ctx context.Context, q credential.Access) error {
		p, err := q.EffectivePrincipal()
		if err != nil {
			return err
		}
		resourceValue, err := resource.NewRepository(store).Get(ctx, p.OwnerID, q.ResourceID)
		if err != nil {
			return err
		}
		if resourceValue.Kind != resource.Connection {
			return adapters.credentialAccess(ctx, q)
		}
		if pluginService == nil {
			return core.NewError(core.Unavailable, "plugin connections unavailable")
		}
		c, provider, err := pluginService.ConnectionAccess(ctx, p, q.ResourceID)
		if err != nil {
			return err
		}
		if c.CredentialID != q.CredentialID || provider.Origin != q.Origin {
			return denied()
		}
		value, err := credential.NewRepository(store).Get(ctx, p.OwnerID, q.CredentialID)
		if err != nil {
			return err
		}
		if value.Metadata.Provider != "plugin-http:"+string(c.ID) {
			return denied()
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	adapters.broker = vault
	auditService, err := audit.NewService(store, audit.Options{ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope})
	if err != nil {
		return nil, err
	}
	b.objects, err = files.NewObjects(filepath.Join(cfg.DataDir, "files"), store)
	if err != nil {
		return nil, err
	}
	if err = b.objects.Recover(ctx); err != nil {
		return nil, err
	}
	var channels *weixin.Service
	fileService := files.NewService(store, b.objects, files.Options{Authorizer: permissions, AuthorizeChannel: func(ctx context.Context, owner, chat, binding core.ID) error {
		if channels == nil {
			return core.NewError(core.Forbidden, "channel unavailable")
		}
		return channels.AuthorizeChannel(ctx, owner, chat, binding)
	}, MaxUploadBytes: cfg.Execution.MaxUploadBytes, DiskQuota: func() int64 { return cfg.Execution.MaxDiskBytes }})
	visible := realtime.NewStore(store)
	channels, err = weixin.New(store, weixin.Options{MasterKey: key, PublicURL: cfg.HTTP.PublicURL, Files: fileService.(files.ChannelStore), MaxMediaBytes: cfg.Execution.MaxUploadBytes,
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (weixin.Target, error) {
			if err := p.RequireOwner(); err != nil {
				return weixin.Target{}, err
			}
			action := policy.Read
			if write {
				action = policy.Write
			}
			if err := permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: id}, Action: action}); err != nil {
				return weixin.Target{}, err
			}
			a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, id)
			return weixin.Target{ConversationID: a.ConversationID, Active: a.State == assistant.Active}, err
		},
		Live: func(ctx context.Context, owner, id core.ID) error {
			account, err := auth.NewRepository(store).AccountByID(ctx, owner)
			if err != nil {
				return err
			}
			if account.Account.Disabled {
				return core.NewError(core.Forbidden, "account disabled")
			}
			a, err := assistant.NewRepository(store).Get(ctx, owner, id)
			if err != nil {
				return err
			}
			if a.State != assistant.Active {
				return core.NewError(core.Forbidden, "assistant inactive")
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	b.weixin = channels
	chat := conversation.NewService(store, conversation.Options{Authorizer: permissions, Visible: visible, CheckWebInput: channels.CheckWebInput, AuthorizeChannel: channels.AuthorizeChannel, ValidateChannelFile: fileService.(files.ChannelStore).ValidateChannelFile, Published: channels.Published})
	channels.SetReceiver(chat.(weixin.Receiver))
	providers := adapters.providers()
	for name, provider := range sandboxProviders(cfg) {
		providers[name] = provider
	}
	var resources resource.Service = resource.NewService(store, resource.Options{Authorizer: permissions, Providers: providers, Credentials: vault, CheckVerification: identity.CheckVerification, OnDeleteProvider: adapters.deleteProviderReferences})
	var grants policy.Service = permissions
	var shellExecute func(context.Context, core.Execution, tools.ShellRequest) (json.RawMessage, error)
	var shellAvailable func(context.Context, core.Principal) (bool, error)
	var cancelTool func(context.Context, core.Execution, core.ID) error
	if cfg.Sandbox.Enabled {
		b.sandbox, err = newShellService(store, permissions, cfg)
		if err != nil {
			return nil, err
		}
		b.sandbox.audit = auditService
		provider := providers["local-sandbox"]
		provider.Check = func(ctx context.Context, _ resource.Binding) (resource.Status, error) {
			caps, err := b.sandbox.runner.Capabilities(ctx)
			status := resource.Status{State: resource.Available, ReasonCode: caps.Reason, CheckedAt: time.Now()}
			if !caps.Shell {
				status.State = resource.Offline
			}
			return status, err
		}
		providers["local-sandbox"] = provider
		shellExecute = b.sandbox.execute
		shellAvailable = b.sandbox.available
		cancelTool = b.sandbox.cancel
		grants = sandboxGrants{Service: permissions, shell: b.sandbox}
		resources = sandboxResources{Service: resources, shell: b.sandbox}
	}
	sandboxRouter, err := newSandboxRouter(adapters, b.sandbox, key)
	if err != nil {
		return nil, err
	}
	grantRouter = sandboxRouter
	sandboxRouter.verify = identity.CheckVerification
	if err = sandboxRouter.migrate(ctx); err != nil {
		return nil, err
	}
	b.shellTasks = newShellTasks(store, sandboxRouter)
	b.shellTasks.forwardConfig = cfg.PortForward
	shellExecute = b.shellTasks.execute
	shellAvailable = sandboxRouter.available
	cancelTool = sandboxRouter.cancel
	grants = remoteSandboxGrants{Service: grants, router: sandboxRouter}
	resources = remoteSandboxResources{Service: resources, router: sandboxRouter}
	memoryFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(cfg.DataDir, "memory-operations"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return runtimeMemoryRoots(ctx, store, cfg.DataDir, adapters.storageConfig.WorkRoot, owner, query)
	}})
	memories := memory.NewService(store, memory.Options{Files: memoryFiles, Authorizer: permissions, CheckAssistant: access.assistant})
	dreams := dream.New(store, dream.Options{Authorizer: permissions, Memories: memories, Files: memoryFiles, Conversations: chat})
	memoryContext := memoryAccess{memories: memories}
	searching := search.NewService(store, search.Options{Authorizer: permissions, Memories: memories})
	indexer := search.NewIndexer(store, search.Options{Authorizer: permissions, Memories: memories})
	var plans *plan.Coordinator
	taskOptions := task.Options{BindPlan: func(ctx context.Context, p core.Principal, t *task.Task, r task.CreateRequest) error {
		return plans.BindTask(ctx, p, t, r)
	}, Authorizer: permissions, CheckAssistant: access.assistant, ValidateExecution: access.execution, Emit: func(ctx context.Context, p core.Principal, t task.Task, action string) error {
		if action != "created" && action != "revised" && action != "running" && action != "pending" {
			return nil
		}
		return event.NewWriter(store).AppendAssistant(ctx, event.AssistantEvent{ID: stableEventID(t.ID, t.Version), OwnerID: p.OwnerID, AssistantID: t.AssistantID, TaskID: t.ID, Kind: event.ResumeRequested, DeliveryMode: event.WhenIdle, CreatedAt: time.Now().UTC()})
	}, VerifyEvidence: func(ctx context.Context, e core.Execution, d task.Delivery) error {
		return verifyDelivery(ctx, store, e, d)
	}}
	tasks := task.NewService(store, taskOptions)
	plans = plan.New(store, permissions, tasks)
	if err := plans.Migrate(ctx); err != nil {
		return nil, err
	}
	operations := operation.NewService(store, operation.Options{Authorizer: permissions, Reconcile: func(ctx context.Context, op operation.Operation) (operation.Completion, error) {
		if strings.HasPrefix(op.Action, "file_") || op.Action == "shell_execute" {
			if result, err := sandboxRouter.reconcile(ctx, op); !persistence.Missing(err) {
				return result, err
			}
		}
		return reconcileOperation(ctx, store, op)
	}})
	var observeModel func(context.Context, string, core.ID, any)
	if cfg.Debug.Enabled {
		observeModel = func(ctx context.Context, kind string, id core.ID, data any) {
			state := "succeeded"
			if fields, ok := data.(map[string]any); ok {
				if code, ok := fields["Code"].(core.Code); ok && code != "" {
					state = "failed"
				}
				if status, ok := fields["Status"].(int); ok && status >= 400 {
					state = "failed"
				}
			}
			b.Trace.Record(ctx, kind, state, id, data)
		}
	}
	fileTools := &resourceFileTools{adapters: adapters, router: sandboxRouter}
	sandboxRouter.files = fileTools
	var responseLog func(model.Request, model.RawResponse) error
	if cfg.Diagnostics.ModelResponses {
		responseLog = model.NewResponseLogger(filepath.Join(cfg.DataDir, "logs", "model-responses")).Record
	}
	modelClient := model.NewClient(store, model.Options{ResponseLog: responseLog, Dispatched: func(ctx context.Context, request model.Request) error {
		var ids []core.ID
		for _, input := range request.Inputs {
			ids = append(ids, input.ReadMessageIDs...)
		}
		if len(ids) == 0 {
			return nil
		}
		return chat.ReadUserMessages(ctx, request.Execution, ids)
	}, PrepareInputs: fileTools.prepareMedia, Observe: observeModel, Authorizer: permissions, Operations: operations, ListModels: adapters.models, Resolve: adapters.provider, CheckReservation: func(ctx context.Context, r model.Request, provider model.Provider) error {
		return checkReservation(ctx, store, access, r, provider)
	}, Settle: func(ctx context.Context, e core.Execution, r model.Receipt) error {
		if r.Charged == nil {
			return core.NewError(core.FailedPrecondition, "provider cost unknown")
		}
		return settleReceipt(ctx, store, permissions, e, r)
	}, Broker: vault, ClientFor: adapters.client, CheckDestination: adapters.destination})
	permissionRequests := &permissionRequests{router: sandboxRouter, store: store, authorizer: permissions, grants: grants, chat: chat, afterApproval: func(ctx context.Context, p core.Principal, rows []policy.Grant) error {
		for _, g := range rows {
			if g.Scope.FullAccess {
				if b.sandbox != nil {
					if err := b.sandbox.runner.StopAccess(ctx, p.OwnerID, g.AssistantID, true); err != nil {
						return err
					}
				}
				continue
			}
			r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, g.ResourceID)
			if err != nil {
				return err
			}
			if r.Kind == resource.Compute || r.Kind == resource.Storage {
				if b.sandbox != nil {
					if err := (sandboxGrants{shell: b.sandbox}).stop(p.OwnerID); err != nil {
						return err
					}
				}
				return sandboxRouter.invalidate(ctx, p)
			}
		}
		return nil
	}}
	pluginService = plugins.New(store, plugins.Options{
		CheckPlugin: func(ctx context.Context, p core.Principal, id string) error {
			if err := identity.ValidatePrincipal(ctx, p); err != nil {
				return err
			}
			_, err := groupReference(ctx, store, p.OwnerID, "plugin", plugins.Key(id))
			return err
		},
		CheckTool: func(ctx context.Context, p core.Principal, name string) error {
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: core.ID(name)}, Action: policy.Execute})
		},
		Credentials: vault, Broker: vault,
		AuthorizeSettings: func(ctx context.Context, p core.Principal) error {
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "settings", ID: p.OwnerID}, Action: policy.Write})
		},
		CheckConnection: func(ctx context.Context, p core.Principal, id core.ID) error {
			if err := identity.ValidatePrincipal(ctx, p); err != nil {
				return err
			}
			_, err := groupReference(ctx, store, p.OwnerID, "connection", id)
			return err
		},
		CheckConnectionQuota: func(ctx context.Context, p core.Principal, id core.ID) error {
			return checkPluginConnectionQuota(ctx, store, p, id)
		},
		HTTPClient: func(ctx context.Context, p core.Principal, e core.Execution, id core.ID, origin string) (*http.Client, error) {
			binding := func(ctx context.Context, p core.Principal, id core.ID) (platform.NetworkBinding, error) {
				_, provider, err := pluginService.ConnectionAccess(ctx, p, id)
				return platform.NetworkBinding{Origin: provider.Origin}, err
			}
			guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: adapters.privateAddresses(), OwnerBinding: binding, Binding: func(ctx context.Context, e core.Execution, id core.ID) (platform.NetworkBinding, error) {
				if err := access.execution(ctx, e); err != nil {
					return platform.NetworkBinding{}, err
				}
				return binding(ctx, e.Principal, id)
			}})
			if p.Kind == core.ActorOwner {
				return guard.OwnerHTTPClient(p, id, origin), nil
			}
			return guard.HTTPClient(e, id, origin), nil
		},
		IsAdministrator: func(ctx context.Context, p core.Principal) (bool, error) {
			g, err := groupPolicy(ctx, store, p.OwnerID)
			return g.Administrator, err
		},
		UserBinding: func(ctx context.Context, p core.Principal, id core.ID, v plugins.Installation) (plugins.Binding, bool, error) {
			return groupPluginBinding(ctx, store, p, id, v)
		},
		Authorize: func(ctx context.Context, p core.Principal, write bool) error {
			if write {
				if err := identity.RequireAdministrator(ctx, p); err != nil {
					return err
				}
			}
			action := policy.Read
			if write {
				action = policy.Write
			}
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "settings", ID: p.OwnerID}, Action: action})
		},
		CheckAssistant: access.assistant,
		ReadPlans: func(ctx context.Context, p core.Principal) (any, error) {
			return plans.List(ctx, p, time.Time{}, time.Time{})
		},
	})
	profile := &profileTools{workspace: fileTools}
	executor := tools.NewExecutor(store, tools.Options{ProfileUpdate: profile.update, CheckGroupQuota: func(ctx context.Context, c tools.Call) error { return checkGroupToolQuota(ctx, store, c) }, ExtensionDefinitions: pluginService.Definitions, ExtensionValidate: pluginService.ValidateCall, ExtensionPrepare: pluginService.PrepareCall, ExtensionExecute: pluginService.Execute, PermissionList: sandboxRouter.list, PermissionRequest: permissionRequests.create, Cancel: func(ctx context.Context, e core.Execution, id core.ID) error {
		if pluginService.CancelOperation(e.Principal.OwnerID, id) {
			return nil
		}
		return cancelTool(ctx, e, id)
	}, ShellAvailable: shellAvailable, ShellExecute: shellExecute, ShellTask: b.shellTasks.tool, Authorizer: permissions, Operations: operations, Conversations: chat,
		FileSearch: fileTools.search, FileTransfer: fileTools.transfer, FileRead: fileTools.read, FileList: fileTools.list, FileWrite: fileTools.write, FileEdit: fileTools.edit, FileMove: fileTools.move, FileDelete: fileTools.delete,
		Plan:                    planTools{store: store, plans: plans, todos: todo.NewService(store, todo.Options{Authorizer: permissions, CheckAssistant: access.assistant}), schedules: schedule.NewService(store, schedule.Options{Authorizer: permissions, CheckAssistant: access.assistant})}.execute,
		ConversationAttachments: &conversationAttachments{files: fileService, workspace: fileTools, conversations: chat, maxBytes: cfg.Execution.MaxUploadBytes},
	})
	sandboxRouter.catalog = executor
	validateReferences = func(ctx context.Context, p core.Principal, refs []auth.ResourceReference) error {
		catalog, err := groupCatalog(ctx, p, adapters, sandboxRouter, executor)
		if err != nil {
			return err
		}
		available := map[string]groupCatalogItem{}
		for _, item := range catalog {
			available[item.Kind+":"+string(item.ID)] = item
		}
		for _, ref := range refs {
			item, ok := available[ref.Kind+":"+string(ref.ID)]
			if !ok {
				return core.Invalid("resource", "resource reference is not in the catalog")
			}
			if ref.Quota.Storage != nil && !item.WorkspaceQuotaSupported {
				return core.NewError(core.FailedPrecondition, "sandbox does not support storage hard quotas")
			}
			if ref.Quota.Compute != nil && item.Limits != nil {
				if err := applyComputeConstraints(&platform.Command{}, ref.Quota.Compute, *item.Limits); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if cfg.Sandbox.Enabled {
		root := filepath.Dir(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, "owner", "assistant"))
		if err := prepareDirectory(root); err != nil {
			return nil, err
		}
	}
	environment := localEnvironment(cfg)
	b.remote = sandboxRouter
	queue := scheduler.NewQueue(store)
	engine := agent.NewRuntime(store, agent.Options{ModelSource: func(ctx context.Context, p core.Principal, id core.ID) (model.StateSource, error) {
		provider, err := adapters.provider(ctx, p, id)
		return provider.StateSource(), err
	}, CheckEnvironment: environment, CompressionThreshold: cfg.Execution.ContextCompressionThreshold, ContextWindow: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		provider, err := adapters.provider(ctx, p, id)
		if core.CodeOf(err) == core.NotFound {
			err = core.WithReason(core.NewError(core.FailedPrecondition, "model unavailable"), core.ModelUnavailable)
		}
		return provider.Model.ContextWindow(), err
	}, OutputLimit: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		provider, err := adapters.provider(ctx, p, id)
		return provider.Model.OutputWindow(), err
	}, PrepareToolResult: fileTools.boundResult, Conversations: chat, DreamDeadline: dreams.Deadline, DreamActive: dreams.Active, DreamStop: dreams.Cancel, DreamPrepare: dreams.Prepare, DreamOutput: dreams.SavedOutput, DreamTools: []string{"file_list", "file_search", "file_read", "file_write", "file_edit", "file_move", "file_delete"}, DreamContext: func(ctx context.Context, p core.Principal, e event.AssistantEvent) context.Context {
		return dreamFileContext(ctx, dreams, p, e)
	}, DreamCommit: func(ctx context.Context, p core.Principal, e event.AssistantEvent, output string) error {
		if err := dreams.Commit(ctx, p, e, output); err != nil {
			return err
		}
		r, err := persistence.Load[dream.Run](ctx, store, "business.dream_runs", persistence.Key(p.OwnerID, e.OperationID))
		if err != nil {
			return err
		}
		b.Trace.Record(ctx, "dream.memory_changes", "succeeded", "", map[string]any{"SessionID": r.SessionID, "Batch": r.Batch, "MemoryFiles": r.MemoryFiles, "FileChanges": r.FileChanges})
		return nil
	}, ContextSources: func(ctx context.Context, r agent.ContextRequest) ([]agent.ContextDocument, error) {
		docs, err := fileTools.contextSources(ctx, r)
		if err != nil {
			return nil, err
		}
		permissionDocs, err := permissionRequests.context(ctx, r.Execution)
		if err != nil {
			return nil, err
		}
		profileDoc, err := profile.context(ctx, r.Execution)
		if err != nil {
			return nil, err
		}
		channelDoc, err := weixinContext(ctx, store, channels, r.Execution)
		if err != nil {
			return nil, err
		}
		if channelDoc != nil {
			docs = append(docs, *channelDoc)
		}
		docs = append(docs, profileDoc)
		docs = append(docs, permissionDocs...)
		mem, err := memoryContext.context(ctx, r)
		return append(docs, mem...), err
	}, InspectCapabilities: func(ctx context.Context, p core.Principal, id core.ID) (agent.Capabilities, error) {
		result, err := inspectCapabilities(ctx, store, executor, p, id, time.Now())
		if err == nil {
			root := platform.AssistantWorkspace(cfg.DataDir, adapters.storageConfig.WorkRoot, p.OwnerID, id)
			result.Skills.Items, err = workspaceSkills(ctx, root)
			if err == nil {
				global, e := scanRootContext(ctx, platform.UserGlobal(cfg.DataDir, p.OwnerID), agent.ContextRequest{}, ".agents/skills", "/global")
				err = e
				for _, d := range global {
					if d.Kind == "skill" {
						result.Skills.Items = append(result.Skills.Items, agent.SkillInfo{ID: d.Source, Name: path.Base(path.Dir(d.Source)), Description: "Shared workflow: " + d.Source, Version: d.Revision, Enabled: true, RequiredTools: []string{"file_read"}, RequiredActions: []string{"read"}})
					}
				}
			}
			result.Skills.Supported = true
		}
		if err == nil {
			err = sandboxRouter.inspect(ctx, p, id, &result)
		}
		if err == nil {
			err = pluginService.Inspect(ctx, p, id, &result)
		}
		return result, err
	}, ModelRetry: agent.RetryPolicy{MaxAttempts: cfg.Execution.ModelRetry.MaxAttempts, BaseDelay: cfg.Execution.ModelRetry.BaseDelay.Value(), MaxDelay: cfg.Execution.ModelRetry.MaxDelay.Value(), MaxWait: cfg.Execution.ModelRetry.MaxWait.Value(), Deadline: cfg.Execution.ModelRetry.Deadline.Value()}, Trace: b.Trace, Dependencies: agent.Dependencies{Models: modelLimit{modelClient, make(chan struct{}, cfg.Execution.MaxModelCalls)}, Tools: toolLimit{executor, make(chan struct{}, cfg.Execution.MaxToolCalls)}, Policy: permissions, Budget: permissions}, ResolveModel: func(ctx context.Context, p core.Principal, id core.ID) (core.ID, error) {
		a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, id)
		if err != nil {
			return "", err
		}
		if a.ModelID != "" {
			return a.ModelID, nil
		}
		defaults, err := settings.NewRepository(store).Get(ctx, p.OwnerID)
		if err != nil && !persistence.Missing(err) {
			return "", err
		}
		if defaults.DefaultModelID == "" {
			return "", core.WithReason(core.NewError(core.FailedPrecondition, "configure an assistant model"), core.ModelNotConfigured)
		}
		return defaults.DefaultModelID, nil
	}, ConversationID: func(ctx context.Context, p core.Principal, id core.ID) (core.ID, error) {
		a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, id)
		return a.ConversationID, err
	}, Estimate: adapters.estimate, Acknowledge: func(ctx context.Context, t agent.Turn) error {
		job, err := persistence.Load[scheduler.Job](ctx, store, "agent.jobs", persistence.Key(t.Principal.OwnerID, t.Event.ID))
		if err != nil {
			return err
		}
		return queue.Ack(ctx, t.Principal, t.Event.ID, job.Version)
	}})
	assistants := assistant.NewService(store, assistant.Options{CopyMemory: memoryFiles.Clone, Authorizer: permissions, Conversations: conversation.NewRepository(store), Grants: policy.NewRepository(store), Loop: engine.Lifecycle, CheckModel: func(ctx context.Context, p core.Principal, id core.ID) error {
		if id == "" {
			return nil
		}
		provider, err := adapters.provider(ctx, p, id)
		if err != nil {
			return err
		}
		return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: provider.Model.ResourceID}, Action: policy.Read, ResourceID: provider.Model.ResourceID})
	}, GrantModel: func(ctx context.Context, p core.Principal, a assistant.Assistant) error {
		provider, err := adapters.provider(ctx, p, a.ModelID)
		if err != nil {
			return err
		}
		_, err = permissions.Grant(ctx, p, policy.GrantRequest{
			Mutation:    core.Mutation{IdempotencyKey: "hire-model/" + string(a.ID)},
			AssistantID: a.ID, ResourceID: provider.Model.ResourceID,
			Actions: []policy.Action{policy.Read, policy.Execute},
			Scope:   policy.Scope{ModelID: a.ModelID},
		})
		return err
	}})
	profile.assistants = assistants
	consumer := scheduler.NewMessageConsumer(store, scheduler.Options{Conversations: conversation.NewRepository(store)})
	schedules := schedule.NewService(store, schedule.Options{Authorizer: permissions, CheckAssistant: access.assistant})
	dispatcher := schedule.NewDispatcher(store)
	dispatcher.Ready = plans.ScheduleReady
	dispatcher.Start = plans.StartScheduled
	worker := scheduler.NewService(store, scheduler.Options{Ready: func(ctx context.Context) bool { return environment(ctx) == nil }, Authorizer: permissions, Concurrency: cfg.Execution.MaxAssistants, Handle: func(ctx context.Context, c scheduler.Claim) error {
		// Upgrade already-queued legacy schedule events inside their current job,
		// without creating another wakeup or impersonating an owner session.
		if c.Job.Event.Kind == event.ScheduleDue && c.Job.Event.TaskID == "" {
			if err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				job, err := persistence.Load[scheduler.Job](ctx, store, "agent.jobs", persistence.Key(c.Principal.OwnerID, c.Job.Event.ID))
				if err != nil {
					return err
				}
				if job.Event.TaskID == "" {
					source, err := schedule.NewRepository(store).Get(ctx, c.Principal.OwnerID, job.Event.ScheduleID)
					if err != nil {
						return err
					}
					taskID, err := plans.StartScheduled(ctx, source, job.Event.OccurrenceAt)
					if err != nil {
						return err
					}
					job.Event.TaskID = taskID
					if err := event.SaveJob(ctx, tx, job); err != nil {
						return err
					}
					if err := tx.Put(ctx, "agent.events", persistence.Key(c.Principal.OwnerID, job.Event.ID), job.Event); err != nil {
						return err
					}
				}
				c.Job.Event = job.Event
				return nil
			}); err != nil {
				return err
			}
		}
		if c.Job.Event.Kind == event.UserMessage {
			if err := chat.AcceptUserMessage(ctx, core.Execution{Principal: c.Principal, AssistantID: c.Job.Event.AssistantID, OperationID: c.Job.Event.ID}, c.Job.Event.MessageID); err != nil {
				return err
			}
		}
		_, err := engine.Handle(ctx, agent.Turn{Principal: c.Principal, Event: c.Job.Event, Attempt: c.Job.Attempts})
		if err != nil && !scheduler.IsDeferred(err) && c.Job.Event.Kind == event.DreamDue {
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if saveErr := dreams.Failed(finishCtx, c.Job.Event, err); saveErr != nil {
				return saveErr
			}
		}
		return err
	}, DrainMessages: func(ctx context.Context) error {
		for {
			pending, err := conversation.NewOutbox(store).Pending(ctx, 100)
			if err != nil {
				return err
			}
			for _, m := range pending {
				if err := consumer.AcceptMessage(ctx, m); err != nil {
					return err
				}
			}
			if len(pending) < 100 {
				return nil
			}
		}
	}, Tick: func(ctx context.Context, now time.Time) error {
		sandboxRouter.pollRemoteWaits(ctx, now)
		if err := plans.Tick(ctx, now); err != nil {
			return err
		}
		if err := dispatcher.Tick(ctx, now); err != nil {
			return err
		}
		if err := dreams.Tick(ctx, now); err != nil {
			return err
		}
		return collectFiles(ctx, store, b.objects)
	}})
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
	resetContext = engine.ResetContext
	b.Services = api.Services{Dreams: dreams, Plans: plans, Debug: b.Trace, Exchange: b.Exchange, Auth: identity, Browser: identity, Audit: auditService, Assistants: assistants, Conversations: chat, Tasks: tasks, Todos: todo.NewService(store, todo.Options{Authorizer: permissions, CheckAssistant: access.assistant}), Schedules: schedules, Resources: resources, Memories: memories, Settings: preferences, Models: modelClient, Credentials: vault, Grants: grants, Budget: permissions, Agent: engine, Jobs: worker, Operations: operations, Search: searching, Index: indexer, Files: fileService, Realtime: realtime.NewFeed(visible, realtime.Options{Authorizer: permissions, WatchAuthorization: func(p core.Principal) (realtime.AuthorizationWatch, error) { return identity.WatchPrincipal(p) }})}
	limiter, err := api.NewRateLimiter(store, api.RateLimitOptions{Limit: cfg.HTTP.RequestsPerMinute, CategoryLimits: map[string]int{"login": cfg.Auth.LoginAttemptsPerMinute}})
	if err != nil {
		return nil, err
	}
	build := buildinfo.Current()
	build.EmbeddedWebUI = webassets.Embedded
	updateAddresses := adapters.privateAddresses()
	if u, err := url.Parse(cfg.Updates.Source); err == nil {
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && (ip.IsPrivate() || ip.IsLoopback()) {
			updateAddresses = append(updateAddresses, ip)
		}
	}
	updateService := updates.New(cfg.Updates.Source, cfg.Updates.AutoCheck, build, platform.ConfiguredHTTPClient(cfg.Updates.Source, updateAddresses))
	b.HTTP, err = api.NewServer(cfg, b.Services, api.Options{Updates: updateService, UpgradeInfo: updates.ReadInstallation(cfg.ConfigPath, cfg.Updates.Source), Notifications: notification.New(store, permissions), Weixin: channels, Accounts: identity, Setup: b.setup, DiscoverProviders: func(ctx context.Context, p core.Principal) (any, error) {
		if err := identity.RequireAdministrator(ctx, p); err != nil {
			return nil, err
		}
		return adapters.discoverLocalProviders(ctx, p)
	},
		AdminCatalog: func(ctx context.Context, p core.Principal) (any, error) {
			return groupCatalog(ctx, p, adapters, sandboxRouter, executor)
		},
		AdminUsage: func(ctx context.Context, p core.Principal) (any, error) { return adminUsage(ctx, store, identity, p) },
		AccountQuotas: func(ctx context.Context, p core.Principal) (any, error) {
			return accountQuotas(ctx, store, identity, p)
		}, Plugins: pluginService, ShellTasks: func(ctx context.Context, p core.Principal, id core.ID, raw json.RawMessage) (json.RawMessage, error) {
			if err := p.RequireOwner(); err != nil {
				return nil, err
			}
			q, err := tools.ParseShellTask(raw)
			if err != nil {
				return nil, err
			}
			return b.shellTasks.manage(ctx, p, id, q)
		}, PermissionRequests: permissionRequests, SandboxAdmin: sandboxRouter.admin, SandboxInfo: func(ctx context.Context, p core.Principal) (any, error) {
			if err := p.RequireOwner(); err != nil {
				return nil, err
			}
			if err := permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource"}, Action: policy.Read}); err != nil {
				return nil, err
			}
			caps := platform.Capabilities{Reason: "sandbox_disabled"}
			networks := []config.SandboxNetwork{}
			directories := []config.SandboxDirectory{}
			if b.sandbox != nil {
				caps, _ = b.sandbox.runner.Capabilities(ctx)
				if g, err := groupPolicy(ctx, store, p.OwnerID); err == nil && g.Administrator {
					networks = append(networks, cfg.Sandbox.Networks...)
					directories = append(directories, cfg.Sandbox.Directories...)
				}
			}
			return map[string]any{"Enabled": cfg.Sandbox.Enabled, "Capabilities": caps, "Networks": networks, "Directories": directories}, nil
		}, Limiter: limiter, PreLoginCSRF: identity.PreLoginCSRF})
	if err != nil {
		return nil, err
	}
	restore, err := sqlite.NewRestorer(filepath.Join(cfg.DataDir, "runtime.db"))
	if err != nil {
		return nil, err
	}
	b.Application, err = NewApplication(Dependencies{Config: cfg, HTTP: b.HTTP, Auth: identity, Bootstrap: identity, Audit: auditService, Database: db, Restore: restore, Scheduler: worker, Locker: platform.NewLocker()})
	if err != nil {
		return nil, err
	}
	if b.sandbox != nil {
		b.Application.(*Runtime).prepareSandbox = b.sandbox.startup
	}
	b.Application.(*Runtime).recoverCalls = func(ctx context.Context) error {
		if err := recoverCallUsage(ctx, store); err != nil {
			return err
		}
		return pluginService.RecoverHTTP(ctx)
	}
	b.Application.(*Runtime).collectAuth = identity.RunGC
	b.Application.(*Runtime).collectContexts = func(ctx context.Context) { contextstore.RunGC(ctx, store) }
	b.Application.(*Runtime).runChannels = channels.Run
	b.Application.(*Runtime).checkUpdates = updateService.Run
	b.Application.(*Runtime).startShellTasks = b.shellTasks.start
	b.Application.(*Runtime).stopShellTasks = b.shellTasks.close
	return b, nil
}
