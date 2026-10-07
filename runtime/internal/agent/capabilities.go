package agent

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
)

type ToolCapability struct {
	tools.Definition
	Available   bool
	Reason      string
	ResourceIDs []core.ID
}
type SkillInfo struct {
	ID              string
	Name            string
	Description     string
	Version         string
	Enabled         bool
	RequiredTools   []string
	RequiredActions []string
}
type Capabilities struct {
	AssistantID core.ID
	Tools       []ToolCapability
	Skills      struct {
		Supported bool
		Items     []SkillInfo
	}
}

// Capabilities is an owner-facing inspection, never an assistant execution or
// a permission grant. The composition resolves current configuration/readiness.
func (e *Engine) Capabilities(ctx context.Context, p core.Principal, id core.ID) (Capabilities, error) {
	if err := p.RequireOwner(); err != nil {
		return Capabilities{}, err
	}
	if err := id.Validate(); err != nil {
		return Capabilities{}, err
	}
	if err := e.authorize(ctx, p, id, policy.Read); err != nil {
		return Capabilities{}, err
	}
	if e.options.InspectCapabilities == nil {
		return Capabilities{}, core.NewError(core.Unavailable, "capability inspection unavailable")
	}
	return e.options.InspectCapabilities(ctx, p, id)
}
