package app

import (
	"context"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/search"
	"path/filepath"
	"time"
)

type planningServices struct {
	watches     *watch.Service
	tasks       task.Service
	plans       *plan.Coordinator
	memories    memory.Service
	memoryFiles *memory.Files
	dreams      *dream.Coordinator
	searching   search.Service
	indexer     search.Indexer
}

func buildPlanning(cfg config.Config, adapters adapters, chat conversation.Service) planningServices {
	store, permissions := adapters.store, adapters.policy
	access := access{store}
	memoryFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(cfg.DataDir, "memory-operations"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return runtimeMemoryRoots(ctx, store, cfg.DataDir, adapters.storageConfig.WorkRoot, owner, query)
	}})
	memories := memory.NewService(store, memory.Options{Files: memoryFiles, Authorizer: permissions, CheckAssistant: access.assistant})
	dreams := dream.New(store, dream.Options{Authorizer: permissions, Memories: memories, Files: memoryFiles, Conversations: chat})
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
	return planningServices{watch.New(store, watch.Options{Authorizer: permissions, CheckAssistant: access.assistant}), tasks, plans, memories, memoryFiles, dreams, searching, indexer}
}
