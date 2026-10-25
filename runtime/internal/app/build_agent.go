package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"path"
	"path/filepath"
	"time"
)

type agentServices struct {
	engine        *agent.Engine
	assistants    assistant.Service
	recoverClones func(context.Context) error
}

func (b *Bundle) buildAgent(cfg config.Config, adapters adapters, permissions *policy.Manager, chat conversation.Service, planning planningServices, sandbox sandboxServices, toolset toolServices, wiring *buildWiring) agentServices {
	store := adapters.store
	sandboxRouter, fileTools := sandbox.router, sandbox.files
	dreams, memoryContext := planning.dreams, memoryAccess{memories: planning.memories}
	executor, profile, permissionRequests, modelClient := toolset.executor, toolset.profile, toolset.requests, toolset.model
	environment := localEnvironment(cfg)
	queue := scheduler.NewQueue(store)
	engine := agent.NewRuntime(store, agent.Options{RecoveryAllowed: func(ctx context.Context, t agent.Turn) (bool, error) {
		if t.Event.TaskID == "" {
			return true, nil
		}
		v, err := persistence.Load[task.Task](ctx, store, "business.tasks", persistence.Key(t.Principal.OwnerID, t.Event.TaskID))
		if persistence.Missing(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return v.AssistantID == t.Event.AssistantID && v.State != task.Paused && v.State != task.Canceled && v.State != task.Completed && v.State != task.Failed, nil
	}, ModelSource: func(ctx context.Context, p core.Principal, id core.ID) (model.StateSource, error) {
		provider, err := adapters.provider(ctx, p, id)
		return provider.StateSource(), err
	}, CheckEnvironment: environment, CompressionTokens: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		a, err := assistant.NewRepository(store).Get(ctx, p.OwnerID, id)
		return a.ContextCompressionTokens, err
	}, ContextWindow: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		provider, err := adapters.provider(ctx, p, id)
		if core.CodeOf(err) == core.NotFound {
			err = core.WithReason(core.NewError(core.FailedPrecondition, "model unavailable"), core.ModelUnavailable)
		}
		return provider.Model.ContextWindow(), err
	}, OutputLimit: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		provider, err := adapters.provider(ctx, p, id)
		return provider.Model.OutputWindow(), err
	}, PrepareToolResult: fileTools.BoundResult, Conversations: chat, DreamDeadline: dreams.Deadline, DreamActive: dreams.Active, DreamStop: dreams.Cancel, DreamPrepare: dreams.Prepare, DreamOutput: dreams.SavedOutput, DreamTools: []string{"file_list", "file_search", "file_read", "file_write", "file_edit", "file_move", "file_delete"}, DreamContext: func(ctx context.Context, p core.Principal, e event.AssistantEvent) context.Context {
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
		channelDoc, err := weixinContext(ctx, store, b.weixin, r.Execution)
		if err == nil && channelDoc == nil {
			channelDoc, err = qqContext(ctx, store, b.qq, r.Execution)
		}
		if err == nil && channelDoc == nil {
			channelDoc, err = feishuContext(ctx, store, b.feishu, r.Execution)
		}
		if err == nil && channelDoc == nil {
			channelDoc, err = telegramContext(ctx, store, b.telegram, r.Execution)
		}
		if err == nil && channelDoc == nil {
			channelDoc, err = dingtalkContext(ctx, store, b.dingtalk, r.Execution)
		}
		if err != nil {
			return nil, err
		}
		if channelDoc != nil {
			docs = append(docs, *channelDoc)
		}
		slackDoc, err := slackContext(ctx, store, b.slack, r.Execution)
		if err != nil {
			return nil, err
		}
		if slackDoc != nil {
			docs = append(docs, *slackDoc)
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
			if err == nil {
				result.Skills.Supported = true
				result.Skills.Items = append([]agent.SkillInfo{builtinSkillInfo()}, result.Skills.Items...)
			}
		}
		if err == nil {
			err = wiring.plugins.Inspect(ctx, p, id, &result)
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
	cloneFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(cfg.DataDir, "memory-operations"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return memoryRoots(ctx, store, cfg.DataDir, adapters.storageConfig.WorkRoot, owner, query, true)
	}})
	assistants := assistant.NewService(store, assistant.Options{ContextWindow: func(ctx context.Context, p core.Principal, id core.ID) (int, error) {
		provider, err := adapters.provider(ctx, p, id)
		return provider.Model.ContextWindow(), err
	}, CopyMemory: cloneFiles.CloneStaged, Authorizer: permissions, Conversations: conversation.NewRepository(store), Grants: policy.NewRepository(store), Loop: engine.Lifecycle, CheckModel: func(ctx context.Context, p core.Principal, id core.ID) error {
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
	return agentServices{engine, assistants, assistants.RecoverClones}
}
