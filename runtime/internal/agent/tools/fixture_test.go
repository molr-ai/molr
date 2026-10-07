package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{ revoked *atomic.Bool }

func (a fixtureAuthorizer) Require(ctx context.Context, p core.Principal, req policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if a.revoked.Load() || req.Target.ID == "file_read" || req.ResourceID == "foreign-resource" {
		return core.NewError(core.Forbidden, "tool not authorized")
	}
	return nil
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "tools.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		execution := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
		var revoked, canceled atomic.Bool
		var effects atomic.Int64
		authorizer := fixtureAuthorizer{&revoked}
		operations := operation.NewService(db, operation.Options{Authorizer: authorizer})
		call := Call{Execution: execution, Name: "file_write", Arguments: json.RawMessage(`{"resource_id":"resource","path":"document","text":"safe text","overwrite":false}`)}
		options := Options{Authorizer: authorizer, Operations: operations, FileWrite: func(ctx context.Context, e core.Execution, request FileWriteRequest) (json.RawMessage, error) {
			effects.Add(1)
			return json.RawMessage(`{"written":true}`), nil
		}, FileRead: func(context.Context, core.Execution, FileReadRequest) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		}, Cancel: func(context.Context, core.Execution, core.ID) error { canceled.Store(true); return nil }}
		return contractFixture{Executor: NewExecutor(db, options), Owner: owner, Call: call, DeniedTool: "file_read", Effects: func() int { return int(effects.Load()) }, VisibleMessages: func() int { return 0 }, Revoke: func() { revoked.Store(true) }, BlockingCall: func() Call {
			blocked := call
			blocked.Execution.OperationID = "blocked-operation"
			_, err := operations.Begin(context.Background(), operation.BeginRequest{Execution: blocked.Execution, Mutation: core.Mutation{IdempotencyKey: "blocked"}, Action: blocked.Name, ResourceID: "resource", InputDigest: "blocked"})
			testkit.RequireNoError(t, err)
			return blocked
		}, WasCanceled: func(core.ID) bool { return canceled.Load() }, SecretSentinel: "credential-secret-must-not-escape"}
	}
}
