package app

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/shellsession"
)

func (s *shellService) watchAuthorization(ctx context.Context, e core.Execution, c platform.Command) (*sandbox.Authorization, error) {
	deadline := c.AuthorizationDeadline
	return executionAuthorization(s.store, e, func(ctx context.Context) error {
		next, err := s.authorizationDeadline(ctx, e, c)
		if err != nil {
			return err
		}
		if !platform.NativeSandbox() {
			for _, network := range c.NetworkAuthorizations {
				if _, err := s.networkAuthorization(ctx, e, network); err != nil {
					return err
				}
			}
		}
		if !next.Equal(deadline) {
			if scope := shellsession.ManagedFor(ctx, e); scope != nil && scope.AuthorizationExpiry != nil {
				if err = scope.AuthorizationExpiry(next); err != nil {
					return err
				}
			}
			deadline = next
		}
		return nil
	})
}
func executionAuthorization(store persistence.Store, e core.Execution, check func(context.Context) error) (*sandbox.Authorization, error) {
	// Subscribe before the first check. Bucket notifications also cover new grants
	// and resource replacement; record scopes keep unrelated actor activity quiet.
	keys := []persistence.SignalKey{
		persistence.RecordSignal("auth.accounts", string(e.Principal.OwnerID)),
		persistence.RecordSignal("business.assistants", persistence.Key(e.Principal.OwnerID, e.AssistantID)),
	}
	for _, bucket := range []string{"security.grants", "auth.groups", "business.resources", "business.bindings", "sandbox.connections", "sandbox.epochs"} {
		keys = append(keys, persistence.BucketSignal(bucket))
	}
	watch, err := store.Watch(keys...)
	if err != nil {
		return nil, err
	}
	return &sandbox.Authorization{Watch: watch, Check: func(ctx context.Context) (time.Time, error) {
		checkedAt := time.Now()
		if err := check(ctx); err != nil {
			return time.Time{}, err
		}
		grants, err := policy.ActiveGrants(ctx, store, e.Principal.OwnerID, policy.Query{AssistantID: e.AssistantID}, checkedAt)
		if err != nil {
			return time.Time{}, err
		}
		var next time.Time
		now := checkedAt
		consider := func(at time.Time) {
			if at.After(now) && (next.IsZero() || at.Before(next)) {
				next = at
			}
		}
		for _, g := range grants {
			if g.ExpiresAt != nil {
				consider(*g.ExpiresAt)
			}
			if g.Temporary != nil {
				consider(g.Temporary.Until)
			}
		}
		return next, nil
	}}, nil
}
