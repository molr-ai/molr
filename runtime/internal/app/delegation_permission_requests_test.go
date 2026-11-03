package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

func delegationRequestService(f collaborationFixture) *permissionRequests {
	return &permissionRequests{store: f.b.store, authorizer: f.b.Services.Grants.(policy.Authorizer), grants: f.b.Services.Grants, assistants: f.b.Services.Assistants, chat: f.b.Services.Conversations.(conversation.Publisher)}
}
func createDelegationRequest(t *testing.T, s *permissionRequests, e core.Execution, input policy.PermissionRequestInput) policy.PermissionRequest {
	t.Helper()
	raw, err := s.create(t.Context(), e, input)
	need(t, err)
	var r policy.PermissionRequest
	need(t, json.Unmarshal(raw, &r))
	return r
}
func TestDelegationPermissionApprovalAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"temporary_hiring", "assistant_communication"} {
		t.Run(kind, func(t *testing.T) {
			f := collaborationTestFixture(t, true)
			s := delegationRequestService(f)
			ctx := t.Context()
			proposal := policy.PermissionRequestInput{PermissionType: kind, Reason: "Delegate the research", ExpiresAt: nil}
			if kind == "temporary_hiring" {
				proposal.Hiring = &policy.HiringPermissionProposal{MaxAssistants: 3, Budget: core.Money{Currency: "USD", Micros: 2000000}, GrantIDs: []core.ID{f.grant.ID}}
			} else {
				proposal.Communication = &policy.CommunicationPermissionProposal{AllAssistants: true}
			}
			request := createDelegationRequest(t, s, f.execution, proposal)
			duplicate := f.execution
			duplicate.OperationID = "duplicate-delegation"
			proposal.Reason = "Clarified purpose"
			if next := createDelegationRequest(t, s, duplicate, proposal); next.ID != request.ID {
				t.Fatal("duplicate request was not reused")
			}
			q := policy.PermissionResolution{RequestID: request.ID, Decision: "approve", AssistantVersion: f.parent.Version, Hiring: proposal.Hiring, Communication: proposal.Communication, Mutation: core.Mutation{IdempotencyKey: "approve-delegation", ExpectedVersion: request.Version}}
			if q.Hiring != nil {
				edited := *q.Hiring
				edited.MaxAssistants = 4
				q.Hiring = &edited
			}
			stale := q
			stale.AssistantVersion--
			if _, err := s.Resolve(ctx, f.owner, stale); core.CodeOf(err) != core.Conflict {
				t.Fatalf("stale approval: %v", err)
			}
			if _, err := s.Resolve(ctx, f.execution.Principal, q); core.CodeOf(err) != core.Forbidden {
				t.Fatalf("self approval: %v", err)
			}
			f.b.store.(*sqlitestore.Store).FailNextCommit(errors.New("approval commit failed"))
			if _, err := s.Resolve(ctx, f.owner, q); err == nil {
				t.Fatal("failed commit accepted")
			}
			unchanged, err := f.b.Services.Assistants.Get(ctx, f.owner, f.parent.ID)
			need(t, err)
			rows, err := s.List(ctx, f.owner, f.parent.ID)
			need(t, err)
			if unchanged.Version != f.parent.Version || len(rows) != 1 || rows[0].State != "pending" {
				t.Fatal("partial approval survived rollback")
			}
			approved, err := s.Resolve(ctx, f.owner, q)
			need(t, err)
			current, err := f.b.Services.Assistants.Get(ctx, f.owner, f.parent.ID)
			need(t, err)
			if approved.State != "approved" || approved.ApprovedDelegation == nil || approved.ApprovedAssistantVersion != current.Version || approved.Granted != nil || approved.GrantID != "" {
				t.Fatalf("wrong approval snapshot: %+v", approved)
			}
			if kind == "temporary_hiring" {
				if current.Hiring.ID != f.parent.Hiring.ID || current.Hiring.MaxAssistants != 4 || approved.Input.Hiring.MaxAssistants != 3 || approved.ApprovedDelegation.Hiring.MaxAssistants != 4 {
					t.Fatal("hiring identity or edited proposal lost")
				}
			} else if !current.PeerCommunication.Enabled || !current.PeerCommunication.AllAssistants {
				t.Fatal("communication not enabled")
			}
			again, err := s.Resolve(ctx, f.owner, q)
			need(t, err)
			after, err := f.b.Services.Assistants.Get(ctx, f.owner, f.parent.ID)
			need(t, err)
			if again.Version != approved.Version || after.Version != current.Version {
				t.Fatal("retry changed authorization")
			}
			if err := s.checkExistingDelegation(ctx, f.execution, proposal); core.ReasonOf(err) != "permission_already_granted" {
				t.Fatalf("existing coverage: %v", err)
			}
			snapshot, err := currentDelegationPermissions(ctx, f.b.store, f.execution.Principal, time.Now())
			need(t, err)
			if snapshot.AssistantVersion != current.Version || kind == "temporary_hiring" && !snapshot.Hiring.Active || kind == "assistant_communication" && !snapshot.Communication.Active {
				t.Fatalf("missing current state: %+v", snapshot)
			}
			events, err := persistence.Scan[event.AssistantEvent](ctx, f.b.store, "agent.events")
			need(t, err)
			count := 0
			for _, e := range events {
				if e.Kind == event.ResumeRequested && e.AssistantID == f.parent.ID {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("resume event count: %d", count)
			}
		})
	}
}
func TestDelegationPermissionReviewSelectionRejectionAndTemporaryIsolation(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t, true)
	s := delegationRequestService(f)
	ctx := t.Context()
	unspecified := policy.PermissionRequestInput{PermissionType: "temporary_hiring", Reason: "Choose delegated permissions during review", Hiring: &policy.HiringPermissionProposal{MaxAssistants: 1, Budget: core.Money{Currency: "USD", Micros: 1}}}
	if err := s.checkExistingDelegation(ctx, f.execution, unspecified); err != nil {
		t.Fatalf("unspecified scope assumed covered: %v", err)
	}
	worker := f.hire(t, "review-worker")
	workerExecution := f.running(t, worker, "review-worker-run")
	input := policy.PermissionRequestInput{PermissionType: "assistant_communication", Communication: &policy.CommunicationPermissionProposal{}, Reason: "Ask the user to choose a collaborator"}
	if _, err := s.create(ctx, workerExecution, input); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("temporary assistant requested permission: %v", err)
	}
	request := createDelegationRequest(t, s, f.execution, input)
	q := policy.PermissionResolution{RequestID: request.ID, Decision: "approve", AssistantVersion: f.parent.Version, Communication: input.Communication, Mutation: core.Mutation{IdempotencyKey: "choose-peer", ExpectedVersion: request.Version}}
	for _, ids := range [][]core.ID{nil, {f.parent.ID}, {worker.ID}, {"unknown-peer"}} {
		q.Communication = &policy.CommunicationPermissionProposal{AssistantIDs: ids}
		if _, err := s.Resolve(ctx, f.owner, q); err == nil {
			t.Fatalf("invalid selection approved: %v", ids)
		}
	}
	// The owner chooses a peer; approval never changes that peer's authorization.
	peer, err := f.b.Services.Assistants.Hire(ctx, f.owner, assistant.HireRequest{Name: "Peer", ModelID: f.parent.ModelID, Budget: f.parent.Budget, Mutation: core.Mutation{IdempotencyKey: "review-peer"}})
	need(t, err)
	q.Communication = &policy.CommunicationPermissionProposal{AssistantIDs: []core.ID{peer.ID}}
	_, err = s.Resolve(ctx, f.owner, q)
	need(t, err)
	peer, err = f.b.Services.Assistants.Get(ctx, f.owner, peer.ID)
	need(t, err)
	if peer.PeerCommunication != nil && peer.PeerCommunication.Enabled {
		t.Fatal("approval expanded counterpart permission")
	}
	current, err := f.b.Services.Assistants.Get(ctx, f.owner, f.parent.ID)
	need(t, err)
	if assistant.CanContact(current, peer, time.Now()) {
		t.Fatal("one-sided authorization allowed communication")
	}
	// Refusing a broader request leaves existing authorization and manual stop intact.
	input.Communication = &policy.CommunicationPermissionProposal{AllAssistants: true}
	e := f.execution
	e.OperationID = "broader-request"
	rejected := createDelegationRequest(t, s, e, input)
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.interrupt_boundaries", persistence.Key(f.owner.OwnerID, f.parent.ID), event.InterruptBoundary{At: time.Now()})
	}))
	_, err = s.Resolve(ctx, f.owner, policy.PermissionResolution{RequestID: rejected.ID, Decision: "reject", Mutation: core.Mutation{IdempotencyKey: "reject-broader", ExpectedVersion: rejected.Version}})
	need(t, err)
	after, err := f.b.Services.Assistants.Get(ctx, f.owner, f.parent.ID)
	need(t, err)
	if after.Version != current.Version || after.PeerCommunication.AllAssistants {
		t.Fatal("rejection changed permissions")
	}
	events, err := persistence.Scan[event.AssistantEvent](ctx, f.b.store, "agent.events")
	need(t, err)
	for _, e := range events {
		if e.ID == core.ID("permission-"+string(rejected.ID)) {
			t.Fatal("rejection resumed manually stopped work")
		}
	}
}
