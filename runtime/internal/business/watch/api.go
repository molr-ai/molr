// Package watch owns continuous plans and their durable watcher protocol state.
package watch

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"time"
)

const Bucket = "business.watches"
const MaxPending = 128
const MaxMessage = 64 << 10
const MaxState = 4 << 10

type File struct {
	ResourceID core.ID `json:"resource_id"`
	Path       string  `json:"path,omitempty"`
	Write      bool    `json:"write,omitempty"`
}
type Config struct {
	ComputeID     core.ID   `json:"compute_id"`
	SandboxID     core.ID   `json:"sandbox_id,omitempty"`
	Script        string    `json:"script"`
	Directory     string    `json:"directory,omitempty"`
	Files         []File    `json:"files,omitempty"`
	NetworkMode   string    `json:"network_mode,omitempty"`
	NetworkIDs    []core.ID `json:"network_ids,omitempty"`
	HealthSeconds int       `json:"health_seconds,omitempty"`
}
type Signal struct {
	Key  string
	Data json.RawMessage
	At   time.Time
}
type Watch struct {
	ID, OwnerID, AssistantID core.ID
	Title, Instructions      string
	Watcher                  Config
	StartAt, EndAt           *time.Time
	Paused                   bool
	Version                  core.Version
	AuthorizationFingerprint string
	LastInterruptionAt       *time.Time
	State, Reason            string
	HandlingTaskID           core.ID
	LastRunID                core.ID
	RunID                    core.ID
	Checkpoint               json.RawMessage
	LastHealthyAt, RetryAt   *time.Time
	Failures                 int
	FailureNotified          bool
	PendingCount             int
	Pending                  []Signal
	Recent                   []Signal
	Batch                    []Signal
	CreatedAt, UpdatedAt     time.Time
}
type Request struct {
	Mutation            core.Mutation
	ID, AssistantID     core.ID
	Title, Instructions string
	Watcher             Config
	StartAt, EndAt      *time.Time
	Paused              bool
}
type Options struct {
	Authorizer     policy.Authorizer
	CheckAssistant func(context.Context, core.Principal, core.ID) error
}
type Service struct {
	store   persistence.Store
	options Options
}

func New(store persistence.Store, options Options) *Service { return &Service{store, options} }
func Load(ctx context.Context, store persistence.Store, owner, id core.ID) (Watch, error) {
	return persistence.Load[Watch](ctx, store, Bucket, persistence.Key(owner, id))
}
