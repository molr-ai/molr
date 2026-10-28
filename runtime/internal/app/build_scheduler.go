package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"time"
)

func (b *Bundle) buildScheduler(cfg config.Config, permissions policy.Authorizer, chat conversation.Service, planning planningServices, sandboxRouter *sandboxRouter, engine *agent.Engine) (scheduler.Service, schedule.Service) {
	store := b.store
	access := access{store}
	plans, dreams := planning.plans, planning.dreams
	environment := localEnvironment(cfg)
	consumer := scheduler.NewMessageConsumer(store, scheduler.Options{Conversations: conversation.NewRepository(store)})
	schedules := schedule.NewService(store, schedule.Options{Authorizer: permissions, CheckAssistant: access.assistant})
	dispatcher := schedule.NewDispatcher(store)
	dispatcher.Ready = plans.ScheduleReady
	dispatcher.Start = plans.StartScheduled
	b.watchRuntime = &watchRuntime{store: store, watches: planning.watches, plans: plans, shell: b.shellTasks, active: map[string]*watchRun{}}
	worker := scheduler.NewService(store, scheduler.Options{Ready: func(ctx context.Context) bool { return environment(ctx) == nil }, Authorizer: permissions, Concurrency: cfg.Execution.MaxAssistants, RecoverInterrupted: engine.RecoverInterrupted, Handle: func(ctx context.Context, c scheduler.Claim) error {
		if c.Job.Event.Kind == event.UserMessage {
			if err := chat.AcceptUserMessage(ctx, core.Execution{Principal: c.Principal, AssistantID: c.Job.Event.AssistantID, OperationID: c.Job.Event.ID}, c.Job.Event.MessageID); err != nil {
				return err
			}
		}
		if err := plans.RecordWatchHandling(ctx, c.Principal.OwnerID, c.Job.Event.AssistantID, c.Job.Event.TaskID, task.Running, ""); err != nil {
			return err
		}
		_, err := engine.Handle(ctx, agent.Turn{Principal: c.Principal, Event: c.Job.Event, Attempt: c.Job.Attempts})
		if err != nil && ctx.Err() == nil && !scheduler.IsDeferred(err) && c.Job.Event.Kind == event.DreamDue {
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
		sandboxRouter.remoteRuntime.Tick(ctx, now)
		if err := b.watchRuntime.tick(ctx, now); err != nil {
			return err
		}
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
	return worker, schedules
}
