package agent

import (
	"molr.ai/runtime/internal/core"
)

// Validate binds a turn to trusted event/assistant identity. Persistence must
// additionally reject stale execution IDs and incompatible checkpoint formats.
func (t Turn) Validate() error {
	if err := t.Principal.Validate(); err != nil {
		return err
	}
	if err := t.Event.Validate(); err != nil {
		return err
	}
	if t.Principal.Kind != core.ActorAssistant || t.Principal.OwnerID != t.Event.OwnerID || t.Principal.SubjectID != t.Event.AssistantID {
		return core.NewError(core.Forbidden, "event identity mismatch")
	}
	if c := t.Checkpoint; c != nil {
		if c.OwnerID != t.Principal.OwnerID || c.AssistantID != t.Principal.SubjectID {
			return core.NewError(core.Forbidden, "checkpoint identity mismatch")
		}
		if c.FormatVersion == 0 {
			return core.Invalid("checkpoint", "format version required")
		}
	}
	return nil
}
