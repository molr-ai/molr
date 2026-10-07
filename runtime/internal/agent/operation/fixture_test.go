package operation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{}

func (fixtureAuthorizer) Require(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.Validate()
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "operations.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		execution := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
		var mu sync.Mutex
		evidence := map[core.ID]bool{}
		options := Options{Authorizer: fixtureAuthorizer{}, Reconcile: func(ctx context.Context, op Operation) (Completion, error) {
			mu.Lock()
			defer mu.Unlock()
			if !evidence[op.ID] {
				return Completion{}, core.NewError(core.FailedPrecondition, "no provider evidence")
			}
			return Completion{State: Succeeded, ReceiptID: "receipt", SafeResult: json.RawMessage(`{"ok":true}`)}, nil
		}}
		return contractFixture{Service: NewService(db, options), Repository: NewRepository(db), Owner: owner, Other: other, Begin: BeginRequest{Mutation: core.Mutation{IdempotencyKey: "begin"}, Execution: execution, ResourceID: "resource", Action: "execute", InputDigest: "digest"}, ProviderSettled: func(id core.ID) { mu.Lock(); defer mu.Unlock(); evidence[id] = true }, ExternalCalls: func() int { return 0 }}
	}
}
