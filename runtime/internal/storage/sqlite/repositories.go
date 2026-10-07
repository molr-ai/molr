package sqlite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

type repositories struct{ store persistence.Store }

func (r *repositories) Auth() auth.Repository            { return auth.NewRepository(r.store) }
func (r *repositories) Audit() audit.Repository          { return audit.NewRepository(r.store) }
func (r *repositories) Assistants() assistant.Repository { return assistant.NewRepository(r.store) }
func (r *repositories) Conversations() conversation.Repository {
	return conversation.NewRepository(r.store)
}
func (r *repositories) IMOutbox() conversation.Outbox      { return conversation.NewOutbox(r.store) }
func (r *repositories) Tasks() task.Repository             { return task.NewRepository(r.store) }
func (r *repositories) Todos() todo.Repository             { return todo.NewRepository(r.store) }
func (r *repositories) Schedules() schedule.Repository     { return schedule.NewRepository(r.store) }
func (r *repositories) Resources() resource.Repository     { return resource.NewRepository(r.store) }
func (r *repositories) Settings() settings.Repository      { return settings.NewRepository(r.store) }
func (r *repositories) Credentials() credential.Repository { return credential.NewRepository(r.store) }
func (r *repositories) Policy() policy.Repository          { return policy.NewRepository(r.store) }
func (r *repositories) Operations() operation.Repository   { return operation.NewRepository(r.store) }
func (r *repositories) ModelReceipts() model.Receipts      { return model.NewReceipts(r.store) }
func (r *repositories) Checkpoints() agent.Checkpoints     { return agent.NewCheckpoints(r.store) }
func (r *repositories) AssistantEvents() event.Writer      { return event.NewWriter(r.store) }
func (r *repositories) Realtime() realtime.Store           { return realtime.NewStore(r.store) }
func (r *repositories) Queue() scheduler.Queue             { return scheduler.NewQueue(r.store) }
func (r *repositories) Files() files.Repository            { return files.NewRepository(r.store) }
func (r *repositories) Idempotency() Idempotency           { return &idempotency{store: r.store} }

type idempotency struct{ store persistence.Store }

func receiptKey(owner core.ID, action, key string) string {
	return string(owner) + "/" + base64.RawURLEncoding.EncodeToString([]byte(action)) + "/" + base64.RawURLEncoding.EncodeToString([]byte(key))
}
func validReceiptKey(owner core.ID, action, key string) error {
	if owner.Validate() != nil || strings.TrimSpace(action) == "" || len(action) > 128 {
		return core.Invalid("receipt", "invalid owner or action")
	}
	return (core.Mutation{IdempotencyKey: key}).Validate(false)
}
func (r *idempotency) Get(ctx context.Context, owner core.ID, action, key string) (Receipt, error) {
	if err := validReceiptKey(owner, action, key); err != nil {
		return Receipt{}, err
	}
	return persistence.Load[Receipt](ctx, r.store, "system.idempotency", receiptKey(owner, action, key))
}
func (r *idempotency) Insert(ctx context.Context, value Receipt) error {
	if err := validReceiptKey(value.OwnerID, value.Action, value.Key); err != nil {
		return err
	}
	if value.InputDigest == "" || len(value.InputDigest) > 256 || !json.Valid(value.Result) || len(value.Result) > 1<<20 || value.CreatedAt.IsZero() {
		return core.Invalid("receipt", "digest, bounded JSON result and timestamp required")
	}
	// Auth/credential issuance returns raw secrets and cannot be cached here.
	if strings.HasPrefix(value.Action, "auth.") || strings.HasPrefix(value.Action, "credential.") {
		return core.NewError(core.Forbidden, "secret-bearing actions cannot use result receipts")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := r.Get(ctx, value.OwnerID, value.Action, value.Key)
		if err == nil {
			if old.InputDigest == value.InputDigest && bytes.Equal(old.Result, value.Result) {
				return nil
			}
			return persistence.Conflict("idempotency key already used")
		}
		if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, "system.idempotency", receiptKey(value.OwnerID, value.Action, value.Key), value)
	})
}
