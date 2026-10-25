package app

import (
	"context"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
)

func (b *Bundle) buildModels(cfg config.Config, adapters adapters, permissions *policy.Manager, vault *credential.Vault, chat conversation.Service, fileTools *resourceFileTools, operations operation.Service) (model.Client, error) {
	store := adapters.store
	access := access{store}
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
	var responseLog func(model.Request, model.RawResponse) error
	if cfg.Diagnostics.ModelResponses {
		logger, err := model.NewResponseLogger(filepath.Join(cfg.DataDir, "logs", "model-responses"), cfg.Logging)
		if err != nil {
			return nil, err
		}
		responseLog = logger.Record
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
	}, PrepareInputs: fileTools.PrepareMedia, Observe: observeModel, Authorizer: permissions, Operations: operations, ListModels: adapters.models, Resolve: adapters.provider, CheckReservation: func(ctx context.Context, r model.Request, provider model.Provider) error {
		return checkReservation(ctx, store, access, r, provider)
	}, Settle: func(ctx context.Context, e core.Execution, r model.Receipt) error {
		if r.Charged == nil {
			return core.NewError(core.FailedPrecondition, "provider cost unknown")
		}
		return settleReceipt(ctx, store, permissions, e, r)
	}, Broker: vault, ClientFor: adapters.client, CheckDestination: adapters.destination})
	return modelClient, nil
}
