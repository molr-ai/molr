// Package tools exposes built-in capabilities and the controlled plugin adapter.
package tools

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
)

type Definition struct {
	Name            string
	Description     string
	InputSchema     json.RawMessage
	RequiredActions []string
	SideEffect      bool
}
type Call struct {
	ModelID   core.ID // Trusted loop selection; never supplied in tool arguments.
	Execution core.Execution
	Name      string
	Arguments json.RawMessage
}
type Result struct {
	Failure     *Failure `json:",omitempty"`
	OperationID core.ID
	Content     json.RawMessage
	Media       []media.Part `json:",omitempty"`
	FileIDs     []core.ID
}

// Executor combines compiled-in tools with immutable plugin contributions. Definitions are filtered by current
// permissions. Execute rechecks access and never accepts a Principal from tool
// arguments. Execute returns model-visible Content even on failure; the Go error
// is retained for diagnostics, not loop control. Results are sanitized and are
// not automatically chat messages.
// Plugins enter through the trusted extension adapter, never arbitrary Go code.
type Catalog interface {
	Catalog(context.Context, core.Principal) ([]Definition, error)
}

type Executor interface {
	Catalog
	Definitions(context.Context, core.Principal) ([]Definition, error)
	Execute(context.Context, Call) (Result, error)
	Cancel(context.Context, core.Execution, core.ID) error
}
