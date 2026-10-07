package assistant

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
)

func TestHireRollsBackWhenModelAuthorizationFails(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "authorization rejected"
		if missing {
			name = "authorization not configured"
		}
		t.Run(name, func(t *testing.T) {
			db := fixtureDB(t)
			loopStarted := false
			opts := Options{Authorizer: fixtureAuthorizer{}, CheckModel: func(context.Context, core.Principal, core.ID) error { return nil }, Loop: func(context.Context, core.Principal, core.ID, string) error {
				loopStarted = true
				return nil
			}}
			if !missing {
				opts.GrantModel = func(ctx context.Context, p core.Principal, a Assistant) error {
					// Simulate a downstream failure after a grant has been persisted.
					if err := policy.NewRepository(db).InsertGrant(ctx, policy.Grant{ID: "model-grant", OwnerID: p.OwnerID, AssistantID: a.ID, ResourceID: "provider", Actions: []policy.Action{policy.Read, policy.Execute}, Version: 1}); err != nil {
						return err
					}
					return core.NewError(core.Forbidden, "grant rejected")
				}
			}
			_, err := NewService(db, opts).Hire(t.Context(), owner(), HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: mutation("hire", 0), Name: "Assistant", ModelID: "model"})
			code := core.Forbidden
			if missing {
				code = core.Unavailable
			}
			wantCode(t, err, code)
			if loopStarted {
				t.Fatal("started agent loop without model access")
			}
			for _, bucket := range []string{"business.assistants", "im.conversations", "security.grants"} {
				if fixtureCount(t, db, bucket) != 0 {
					t.Fatalf("failed hire left rows in %s", bucket)
				}
			}
		})
	}
}
