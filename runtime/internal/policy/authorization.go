package policy

import (
	"context"

	"molr.ai/runtime/internal/core"
)

// Authorize checks the actor shape and optional owner restriction before calling
// the live authorizer. The caller retains its transaction and domain checks.
func Authorize(ctx context.Context, p core.Principal, authorizer Authorizer, request CheckRequest, ownerOnly bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ownerOnly {
		if err := p.RequireOwner(); err != nil {
			return err
		}
	}
	if authorizer == nil {
		return core.NewError(core.Unavailable, "authorization not configured")
	}
	return authorizer.Require(ctx, p, request)
}
