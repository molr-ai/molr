package app

import (
	"context"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"strings"
)

type toolServices struct {
	model      model.Client
	operations operation.Service
	executor   tools.Executor
	requests   *permissionRequests
	profile    *profileTools
}

func (b *Bundle) buildTools(cfg config.Config, adapters adapters, permissions *policy.Manager, vault *credential.Vault, chat chatServices, planning planningServices, sandbox sandboxServices, wiring *buildWiring) (toolServices, error) {
	store := adapters.store
	access := access{store}
	sandboxRouter, fileTools, grants := sandbox.router, sandbox.files, sandbox.grants
	plans, fileService := planning.plans, chat.files
	conversations := chat.chat
	operations := operation.NewService(store, operation.Options{Authorizer: permissions, Reconcile: func(ctx context.Context, op operation.Operation) (operation.Completion, error) {
		if strings.HasPrefix(op.Action, "file_") || op.Action == "shell_execute" {
			if result, err := sandboxRouter.reconcile(ctx, op); !persistence.Missing(err) {
				return result, err
			}
		}
		return reconcileOperation(ctx, store, op)
	}})

	modelClient, err := b.buildModels(cfg, adapters, permissions, vault, conversations, fileTools, operations)
	if err != nil {
		return toolServices{}, err
	}
	permissionRequests := &permissionRequests{router: sandboxRouter, store: store, authorizer: permissions, grants: grants, chat: conversations, afterApproval: func(ctx context.Context, p core.Principal, rows []policy.Grant) error {
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
	extensions := externalTools{plugin: wiring.plugins, mcp: wiring.mcp}
	profile := &profileTools{workspace: fileTools}
	executor := tools.NewExecutor(store, tools.Options{ProfileUpdate: profile.update, CheckGroupQuota: func(ctx context.Context, c tools.Call) error { return checkGroupToolQuota(ctx, store, c) }, ExtensionDefinitions: extensions.Definitions, ExtensionValidate: extensions.ValidateCall, ExtensionPrepare: extensions.PrepareCall, ExtensionExecute: extensions.Execute, PermissionList: sandboxRouter.list, PermissionRequest: permissionRequests.create, Cancel: func(ctx context.Context, e core.Execution, id core.ID) error {
		if wiring.mcp.CancelOperation(e.Principal.OwnerID, id) || wiring.plugins.CancelOperation(e.Principal.OwnerID, id) {
			return nil
		}
		return sandboxRouter.cancel(ctx, e, id)
	}, ShellAvailable: sandboxRouter.available, ShellExecute: b.shellTasks.Execute, ShellTask: b.shellTasks.Tool, Browser: b.shellTasks.BrowserTool, Authorizer: permissions, Operations: operations, Conversations: conversations,
		FileSearch: fileTools.Search, FileTransfer: fileTools.transfer, FileRead: fileTools.Read, FileList: fileTools.List, FileWrite: fileTools.Write, FileEdit: fileTools.Edit, FileMove: fileTools.Move, FileDelete: fileTools.Delete,
		Plan:                    planTools{watches: planning.watches, store: store, plans: plans, todos: todo.NewService(store, todo.Options{Authorizer: permissions, CheckAssistant: access.assistant}), schedules: schedule.NewService(store, schedule.Options{Authorizer: permissions, CheckAssistant: access.assistant})}.execute,
		ConversationAttachments: &conversationAttachments{files: fileService, workspace: fileTools, conversations: conversations, maxBytes: cfg.Execution.MaxUploadBytes},
	})
	sandboxRouter.catalog = executor
	return toolServices{modelClient, operations, executor, permissionRequests, profile}, nil
}
