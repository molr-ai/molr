package policy

import (
	"context"
	"errors"
	"testing"

	"molr.ai/runtime/internal/core"
)

type authorizationProbe func(context.Context, core.Principal, CheckRequest) error

func (f authorizationProbe) Require(ctx context.Context, p core.Principal, q CheckRequest) error {
	return f(ctx, p, q)
}

func TestAuthorizePreservesChecksAndRequest(t *testing.T) {
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	assistant := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	request := CheckRequest{Target: core.Object{Kind: "resource", ID: "resource"}, Action: Execute, ResourceID: "resource", ModelID: "model", TaskID: "task", Destination: "https://example.com", FullAccess: true}
	denied := core.WithReason(core.NewError(core.Forbidden, "live access denied"), "grant_changed")
	for _, tc := range []struct {
		name      string
		principal core.Principal
		ownerOnly bool
		missing   bool
		code      core.Code
		called    bool
	}{
		{"invalid-before-missing", core.Principal{}, true, true, core.Unauthenticated, false},
		{"invalid-before-authorizer", core.Principal{}, false, false, core.Unauthenticated, false},
		{"owner-before-missing", assistant, true, true, core.Forbidden, false},
		{"owner-before-authorizer", assistant, true, false, core.Forbidden, false},
		{"missing-authorizer", owner, true, true, core.Unavailable, false},
		{"owner-delegates", owner, true, false, core.Forbidden, true},
		{"assistant-delegates", assistant, false, false, core.Forbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			calls := 0
			var authorizer Authorizer
			if !tc.missing {
				authorizer = authorizationProbe(func(gotContext context.Context, gotPrincipal core.Principal, gotRequest CheckRequest) error {
					calls++
					if gotContext != ctx || gotPrincipal != tc.principal || gotRequest != request {
						t.Fatal("authorization changed context, identity or request")
					}
					return denied
				})
			}
			err := Authorize(ctx, tc.principal, authorizer, request, tc.ownerOnly)
			if core.CodeOf(err) != tc.code || (calls == 1) != tc.called || calls > 1 {
				t.Fatalf("error=%v, calls=%d", err, calls)
			}
			if tc.called && !errors.Is(err, denied) {
				t.Fatal("authorization replaced the live error")
			}
		})
	}
}

func TestAuthorizeRechecksLiveGrant(t *testing.T) {
	f := fixture(t)
	ctx := t.Context()
	g := grant(t, f)
	request := CheckRequest{Target: core.Object{Kind: "resource", ID: f.ResourceID}, ResourceID: f.ResourceID, Action: Read, Destination: "/documents/report.txt"}
	if err := Authorize(ctx, f.Assistant, f.Service, request, false); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.Revoke(ctx, f.Owner, RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-through-helper", ExpectedVersion: g.Version}, GrantID: g.ID}); err != nil {
		t.Fatal(err)
	}
	if err := Authorize(ctx, f.Assistant, f.Service, request, false); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("revoked grant authorized: %v", err)
	}
}
