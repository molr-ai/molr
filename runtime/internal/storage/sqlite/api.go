// Package sqlite defines transactional persistence and offline backup/restore.
package sqlite

import (
	"context"
	"encoding/json"
	"time"

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
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

// Repositories is scoped to one transaction. It must not escape the callback,
// be used after completion, or be shared with asynchronous/external operations.
// All services use the same transaction for logically atomic cross-module work.
type Repositories interface {
	Auth() auth.Repository
	Audit() audit.Repository
	Assistants() assistant.Repository
	Conversations() conversation.Repository
	IMOutbox() conversation.Outbox
	Tasks() task.Repository
	Todos() todo.Repository
	Schedules() schedule.Repository
	Resources() resource.Repository
	Settings() settings.Repository
	Credentials() credential.Repository
	Policy() policy.Repository
	Operations() operation.Repository
	ModelReceipts() model.Receipts
	Checkpoints() agent.Checkpoints
	AssistantEvents() event.Writer
	Realtime() realtime.Store
	Queue() scheduler.Queue
	Files() files.Repository
	Idempotency() Idempotency
}

// Transactions rolls back on callback error or canceled context and propagates
// commit failures. External side effects must never occur inside a callback.
type Transactions interface {
	Within(context.Context, func(Repositories) error) error
}
type Schema struct {
	Version           uint32
	CheckpointVersion uint32
}
type Backup struct {
	ID        core.ID
	Schema    Schema
	CreatedAt time.Time
}

// Database owns one local DB connection pool. Backup returns an opaque artifact,
// not a public filesystem URL. Restore is offline-only and invalidates auth data.
type Database interface {
	Transactions
	Schema(context.Context) (Schema, error)
	Migrate(context.Context) error
	Check(context.Context) error
	Backup(context.Context) (Backup, error)
	Close() error
}
type Restorer interface {
	Restore(context.Context, Backup) error
}

// Receipt is transactionally stored with the mutation. Different payloads under
// the same owner/action/key conflict. It must never cache issued auth secrets.
type Receipt struct {
	OwnerID     core.ID
	Action      string
	Key         string
	InputDigest string
	Result      json.RawMessage
	CreatedAt   time.Time
}
type Idempotency interface {
	Get(context.Context, core.ID, string, string) (Receipt, error)
	Insert(context.Context, Receipt) error
}
