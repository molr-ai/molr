package conversation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

// This is the external authorization dependency, not the conversation SUT.
// The fixture grants its seeded identities access; repository/participant checks
// must independently enforce the actual message ownership boundary.
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
		ctx := testkit.Context(t)
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "im.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		execution := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
		repo := NewRepository(db)
		chat := Conversation{ID: "chat", OwnerID: owner.OwnerID, PeerID: execution.AssistantID, Version: 1}
		for _, c := range []Conversation{chat, {ID: "second", OwnerID: owner.OwnerID, PeerID: "second-assistant", Version: 1}, {ID: "foreign", OwnerID: other.OwnerID, PeerID: "foreign-assistant", Version: 1}} {
			testkit.RequireNoError(t, repo.Insert(ctx, c))
		}
		visible := realtime.NewStore(db)
		return contractFixture{Service: NewService(db, Options{Authorizer: fixtureAuthorizer{}, Visible: visible}), Repository: repo, Outbox: NewOutbox(db), Owner: owner, Other: other, Execution: execution, Chat: chat,
			ObserveFeedback: func(id core.ID) Feedback {
				value, err := persistence.Load[Feedback](ctx, db, feedbackBucket, persistence.Key(owner.OwnerID, id))
				if persistence.Missing(err) {
					return NoFeedback
				}
				testkit.RequireNoError(t, err)
				return value
			},
			VisibleCount: func() int {
				page, err := visible.Replay(ctx, owner.OwnerID, realtime.Query{})
				testkit.RequireNoError(t, err)
				return len(page.Items)
			},
			FailNextCommit: func() { db.FailNextCommit(errors.New("injected SQLite commit failure")) },
		}
	}
}
