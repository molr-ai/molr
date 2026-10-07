package conversation

import (
	"molr.ai/runtime/internal/core"
)

// Validate verifies shape and execution binding only. Current resource grants,
// conversation membership and archived state must still be checked by Service.
func (r PublishRequest) Validate(execution core.Execution) error {
	if err := execution.Validate(); err != nil {
		return err
	}
	if err := r.Mutation.Validate(false); err != nil {
		return err
	}
	if err := r.ConversationID.Validate(); err != nil {
		return err
	}
	if r.Source.OperationID != "" && r.Source.OperationID != execution.OperationID {
		return core.NewError(core.Forbidden, "publication operation mismatch")
	}
	for _, ref := range r.Source.References {
		if ref.Kind == "" || ref.ID.Validate() != nil {
			return core.Invalid("source", "invalid reference")
		}
	}
	return r.Content.Validate()
}
