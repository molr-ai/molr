// Package sandboxruntime owns durable remote execution and unavailable-node
// recovery. Application authorization and node transport are injected capabilities.
package sandboxruntime

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"sync"
	"time"
)

type Client interface {
	Execute(context.Context, sm.Request, func(context.Context) error) (json.RawMessage, error)
	Await(context.Context, sm.Identity, func(context.Context) error) (json.RawMessage, error)
}
type Host interface {
	Connect(context.Context, core.Principal, core.ID) (Client, sm.Info, error)
	Info(context.Context, core.Principal, core.ID) (sm.Info, error)
	Epoch(context.Context, core.ID) (uint64, error)
	CheckExecution(context.Context, core.Execution) error
	CheckConnection(context.Context, core.Principal, core.ID) error
	CheckMembership(context.Context, core.ID, core.ID) error
}
type Remote struct {
	store                   persistence.Store
	host                    Host
	waitMu                  sync.Mutex
	waitWG                  sync.WaitGroup
	waitClosed, waitPolling bool
	nextWaitPoll            time.Time
}

func New(store persistence.Store, host Host) *Remote { return &Remote{store: store, host: host} }
