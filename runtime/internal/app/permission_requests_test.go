package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/platform"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func newPermissionRequestsFixture(t *testing.T, f resourceAccessFixture) *permissionRequests {
	t.Helper()
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		a, err := assistant.NewRepository(f.b.store).Get(ctx, f.owner.OwnerID, f.execution.AssistantID)
		if err != nil {
			return err
		}
		if a.ConversationID == "" {
			a.ConversationID = "permission-chat"
			if err := conversation.NewRepository(f.b.store).Insert(ctx, conversation.Conversation{ID: a.ConversationID, OwnerID: f.owner.OwnerID, PeerID: a.ID, Version: 1}); err != nil {
				return err
			}
			return tx.Put(ctx, "business.assistants", persistence.Key(f.owner.OwnerID, a.ID), a)
		}
		return nil
	}))
	return &permissionRequests{store: f.b.store, authorizer: f.ad.policy, grants: f.b.Services.Grants, chat: f.b.Services.Conversations.(conversation.Publisher)}
}

func TestPermissionRequestApprovalIsExplicitScopedAndIdempotent(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := newPermissionRequestsFixture(t, f)
	expires := time.Now().Add(time.Hour).UTC()
	input := policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Analysis reports", Actions: []policy.Action{policy.Read, policy.Write}, PathPrefixes: []string{"/read"}, ExpiresAt: &expires, Reason: "Analyze the reports"}
	raw, err := s.create(t.Context(), f.execution, input)
	need(t, err)
	var request policy.PermissionRequest
	need(t, json.Unmarshal(raw, &request))
	if request.State != "pending" || request.Input.ResourceID != f.storage.ID {
		t.Fatalf("unexpected request: %+v", request)
	}
	if len(f.list(t).Resources) != 1 {
		t.Fatal("request granted access before approval")
	}
	duplicate := f.execution
	duplicate.OperationID = "duplicate"
	input.Reason = "Same task, clarified reason"
	raw, err = s.create(t.Context(), duplicate, input)
	need(t, err)
	var second policy.PermissionRequest
	need(t, json.Unmarshal(raw, &second))
	messages, err := f.b.Services.Conversations.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "permission-chat"})
	need(t, err)
	if len(messages.Items) != 1 || messages.Items[0].Content.Card == nil || messages.Items[0].Content.Card.Kind != conversation.PermissionRequestCard || request.MessageID != messages.Items[0].ID {
		t.Fatalf("request did not create exactly one chat card: %+v", messages.Items)
	}
	if second.ID != request.ID {
		t.Fatal("duplicate pending request was not reused")
	}
	notices := notification.New(f.b.store, f.ad.policy)
	pendingNotices, err := notices.List(t.Context(), f.owner, notification.Query{Kind: "permission.request", Filter: "active"})
	need(t, err)
	if len(pendingNotices.Items) != 1 || pendingNotices.Items[0].Source.ID != request.ID {
		t.Fatal("missing or duplicate request notification", pendingNotices)
	}

	q := policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: "approve", ExpectedVersion: 1}, RequestID: request.ID, Decision: "approve", ResourceID: f.storage.ID, Actions: []policy.Action{policy.Read}, Scope: policy.Scope{PathPrefixes: []string{"/read/subdir"}, ReadOnly: true}, ExpiresAt: &expires}
	for _, change := range []func(*policy.PermissionResolution){
		func(q *policy.PermissionResolution) { q.Actions = []policy.Action{policy.Execute} },
		func(q *policy.PermissionResolution) { q.Scope.PathPrefixes = []string{"/read/../secret"} },
	} {
		bad := q
		change(&bad)
		if _, err := s.Resolve(t.Context(), f.owner, bad); err == nil {
			t.Fatal("invalid approval accepted")
		}
	}
	if _, err := s.Resolve(t.Context(), f.execution.Principal, q); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("assistant resolved request: %v", err)
	}
	if _, err := s.List(t.Context(), f.owner, "another-assistant"); err == nil {
		t.Fatal("foreign assistant visible")
	}
	// A fresh service instance reads the same pending request after a restart.
	s = newPermissionRequestsFixture(t, f)
	approved, err := s.Resolve(t.Context(), f.owner, q)
	need(t, err)
	if approved.State != "approved" || approved.GrantID == "" || approved.Version != 2 || approved.Granted == nil || len(approved.Granted.Actions) != 1 {
		t.Fatalf("approval: %+v", approved)
	}
	for _, grant := range approved.GrantedAll {
		if grant.ExpiresAt == nil || !grant.ExpiresAt.Equal(expires) {
			t.Fatal("group deadlines differ", grant)
		}
	}
	resolvedNotice, err := notices.Get(t.Context(), f.owner, pendingNotices.Items[0].ID)
	need(t, err)
	if resolvedNotice.ResolvedAt == nil {
		t.Fatal("approved request notice still active")
	}
	again, err := s.Resolve(t.Context(), f.owner, q)
	need(t, err)
	if again.GrantID != approved.GrantID {
		t.Fatal("retry created a second grant")
	}
	page, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{AssistantID: f.execution.AssistantID, ResourceID: f.storage.ID})
	need(t, err)
	if len(page.Items) != 1 || len(page.Items[0].Actions) != 1 || page.Items[0].Scope.PathPrefixes[0] != "/read/subdir" {
		t.Fatalf("wrong effective grant: %+v", page.Items)
	}
	q.Decision = "reject"
	if _, err := s.Resolve(t.Context(), f.owner, q); core.CodeOf(err) != core.Conflict {
		t.Fatalf("changed retry accepted: %v", err)
	}
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), f.b.store, "agent.events")
	need(t, err)
	if len(events) != 1 || events[0].AssistantID != f.execution.AssistantID || events[0].Kind != event.ResumeRequested {
		t.Fatalf("wrong resume events: %+v", events)
	}
}

func TestPermissionRequestRejectionAndManualStop(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject resumes", true: "stop stays stopped"}[stopped], func(t *testing.T) {
			f := newResourceAccessFixture(t, nil)
			s := newPermissionRequestsFixture(t, f)
			raw, err := s.create(t.Context(), f.execution, policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Reports", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read"}, ExpiresAt: futurePermissionTime(), Reason: "Read reports"})
			need(t, err)
			var request policy.PermissionRequest
			need(t, json.Unmarshal(raw, &request))
			if stopped {
				need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
					return tx.Put(ctx, "agent.interrupt_boundaries", persistence.Key(f.owner.OwnerID, f.execution.AssistantID), event.InterruptBoundary{At: time.Now()})
				}))
			}
			out, err := s.Resolve(t.Context(), f.owner, policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: "reject", ExpectedVersion: 1}, RequestID: request.ID, Decision: "reject"})
			need(t, err)
			if out.State != "rejected" || out.GrantID != "" {
				t.Fatal("rejection created grant")
			}
			if len(f.list(t).Resources) != 1 {
				t.Fatal("rejected request gave access")
			}
			events, err := persistence.Scan[event.AssistantEvent](t.Context(), f.b.store, "agent.events")
			need(t, err)
			if (!stopped && len(events) != 1) || (stopped && len(events) != 0) {
				t.Fatalf("stop boundary violated: %+v", events)
			}
			docs, err := s.context(t.Context(), f.execution)
			need(t, err)
			var rows []policy.PermissionRequest
			need(t, json.Unmarshal([]byte(docs[0].Content), &rows))
			if len(rows) != 1 || rows[0].State != "rejected" {
				t.Fatal("model did not receive rejected decision")
			}
		})
	}
}

func TestPermissionListRetainsGrantDetailsWhenResourceUnavailable(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	expires := time.Now().Add(time.Hour).UTC()
	g := f.grant(t, f.storage.ID, "read", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/read"}, ReadOnly: true}, &expires)
	offline := f.storage
	offline.State = resource.Offline
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.resources", persistence.Key(f.owner.OwnerID, offline.ID), offline)
	}))
	// Deployment restrictions alone can make an operation unavailable. Regardless
	// of availability, the authorization itself retains its exact lifetime/scope.
	out := f.list(t)
	var storage resourceCapability
	for _, row := range out.Resources {
		if row.ResourceID == f.storage.ID {
			storage = row
		}
	}
	if len(storage.Grants) != 1 {
		t.Fatal("missing grant details")
	}
	if lookupOperation(t, out, f.storage.ID, "/read", policy.Read, "file_read").Available {
		t.Fatal("offline resource marked available")
	}
	actual := storage.Grants[0]
	if actual.ID != g.ID || actual.ExpiresAt == nil || !actual.ExpiresAt.Equal(expires) || !actual.Scope.ReadOnly {
		t.Fatalf("wrong grant: %+v", actual)
	}
}

func futurePermissionTime() *time.Time { value := time.Now().Add(time.Hour).UTC(); return &value }

func TestGroupedQuotaApprovalRollsBackAndRetriesAtomically(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		name := "finite"
		if permanent {
			name = "permanent"
		}
		t.Run(name, func(t *testing.T) {
			f := newResourceAccessFixture(t, nil)
			s := newPermissionRequestsFixture(t, f)
			compute := f.compute
			before, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{AssistantID: f.execution.AssistantID, ResourceID: compute.ID})
			need(t, err)
			previous := before.Items[0]
			network, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "quota-network"}, Name: "Network", Kind: resource.Network, Provider: "sandbox-network", Binding: resource.Binding{SandboxTarget: testNetworkPreset()}})
			need(t, err)
			expires := time.Now().Add(time.Hour)
			cpu := &policy.Constraints{Compute: &policy.ComputeConstraints{CPUPercent: 200, MemoryBytes: 1 << 30, Processes: 128, TempBytes: 64 << 20, MaxDurationSeconds: 1800}}
			net := &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1 << 30, Connections: 32, Period: "day", Timezone: "UTC"}}
			if platform.NativeSandbox() {
				cpu = testResourceConstraints(resource.Compute)
				cpu.Compute.MaxDurationSeconds = 3600
				net = nil
			}
			input := policy.PermissionRequestInput{GrantID: previous.ID, GrantVersion: previous.Version, ResourceKind: "compute", ResourceID: compute.ID, ResourceDescription: "Build", Actions: []policy.Action{policy.Execute}, ExpiresAt: &expires, Reason: "Build project", Constraints: cpu, Additional: []policy.PermissionRequestInput{{ResourceKind: "network", ResourceID: network.ID, ResourceDescription: "Download", Actions: []policy.Action{policy.Execute}, ExpiresAt: &expires, Reason: "Download dependencies", Constraints: net}}}
			raw, err := s.create(t.Context(), f.execution, input)
			need(t, err)
			var row policy.PermissionRequest
			need(t, json.Unmarshal(raw, &row))
			q := policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: "group", ExpectedVersion: row.Version}, RequestID: row.ID, Decision: "approve", GrantID: previous.ID, GrantVersion: previous.Version, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Constraints: cpu, ExpiresAt: &expires, Additional: []policy.PermissionResolution{{ResourceID: network.ID, Actions: []policy.Action{policy.Write}, Constraints: net, ExpiresAt: &expires}}}
			if permanent {
				q.ExpiresAt = nil
			}
			if _, err = s.Resolve(t.Context(), f.owner, q); err == nil {
				t.Fatal("invalid second grant accepted")
			}
			grants, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{AssistantID: f.execution.AssistantID, ResourceID: compute.ID})
			need(t, err)
			if len(grants.Items) != 1 || grants.Items[0].Constraints == nil || grants.Items[0].Version != previous.Version {
				t.Fatal("partial group approved")
			}
			q.Additional[0].Actions = []policy.Action{policy.Execute}
			otherExpiry := expires.Add(time.Hour)
			q.Additional[0].ExpiresAt = &otherExpiry
			approved, err := s.Resolve(t.Context(), f.owner, q)
			need(t, err)
			if len(approved.GrantedAll) != 2 {
				t.Fatal(approved)
			}
			for _, grant := range approved.GrantedAll {
				if (permanent && grant.ExpiresAt != nil) || (!permanent && (grant.ExpiresAt == nil || !grant.ExpiresAt.Equal(expires))) {
					t.Fatal("group deadlines differ", grant)
				}
			}
			again, err := s.Resolve(t.Context(), f.owner, q)
			need(t, err)
			if again.GrantedAll[1].ID != approved.GrantedAll[1].ID {
				t.Fatal("retry duplicated grouped grant")
			}
		})
	}
}

func TestPermissionRequestValidationFailsBeforePublishing(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		for _, tc := range []struct {
			name, field, message string
			change               func(*policy.PermissionRequestInput, resourceAccessFixture)
		}{
			{"missing-id", "resource_id", "existing resource ID", func(q *policy.PermissionRequestInput, f resourceAccessFixture) { q.ResourceID = "" }},
			{"unknown-resource", "resource_id", "does not exist", func(q *policy.PermissionRequestInput, f resourceAccessFixture) { q.ResourceID = "nonexistent" }},
			{"foreign-resource", "resource_id", "does not exist", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
					return tx.Put(ctx, "business.resources", "other/foreign", resource.Resource{ID: "foreign", OwnerID: "other", Kind: resource.Storage, State: resource.Available, Actions: []string{"read"}})
				}))
				q.ResourceID = "foreign"
			}},
			{"unknown-kind", "resource_kind", "resource", func(q *policy.PermissionRequestInput, f resourceAccessFixture) { q.ResourceKind = "filesystem" }},
			{"wrong-kind", "resource_kind", "kind mismatch", func(q *policy.PermissionRequestInput, f resourceAccessFixture) { q.ResourceID = f.compute.ID }},
			{"unsupported-action", "actions", "does not support", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				r := f.storage
				r.Actions = []string{"write"}
				r.Version++
				need(t, resource.NewRepository(f.b.store).Save(t.Context(), r, r.Version-1))
			}},
			{"disconnected", "resource_id", "disconnected", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				r := f.storage
				r.State = resource.Disconnected
				r.Version++
				need(t, resource.NewRepository(f.b.store).Save(t.Context(), r, r.Version-1))
			}},
			{"missing-grant", "grant_id", "does not exist", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				q.GrantID = "nonexistent"
				q.GrantVersion = 1
			}},
			{"wrong-grant", "grant_id", "belong", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				page, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{ResourceID: f.compute.ID})
				need(t, err)
				q.GrantID = page.Items[0].ID
				q.GrantVersion = page.Items[0].Version
			}},
			{"stale-grant", "grant_version", "grant changed", func(q *policy.PermissionRequestInput, f resourceAccessFixture) {
				g := f.grant(t, f.storage.ID, "old", []policy.Action{policy.Read}, policy.Scope{}, nil)
				q.GrantID = g.ID
				q.GrantVersion = g.Version + 1
			}},
		} {
			t.Run(fmt.Sprintf("grouped=%t/%s", grouped, tc.name), func(t *testing.T) {
				f := newResourceAccessFixture(t, nil)
				s := newPermissionRequestsFixture(t, f)
				valid := policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Reports", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read"}, ExpiresAt: futurePermissionTime(), Reason: "Read reports"}
				bad := valid
				tc.change(&bad, f)
				input, field := bad, tc.field
				if grouped {
					input = policy.PermissionRequestInput{ResourceKind: "compute", ResourceID: f.compute.ID, ResourceDescription: "Compute", Actions: []policy.Action{policy.Execute}, ExpiresAt: futurePermissionTime(), Reason: "Calculate", Constraints: testResourceConstraints(resource.Compute)}
					input.Additional = []policy.PermissionRequestInput{bad}
					field = "additional_requests[0]." + field
				}
				executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: operation.NewService(f.b.store, operation.Options{Authorizer: f.ad.policy}), PermissionRequest: s.create})
				raw, err := json.Marshal(input)
				need(t, err)
				result, err := executor.Execute(t.Context(), tools.Call{Execution: f.execution, Name: "permission_request", Arguments: raw})
				if err == nil || result.Failure == nil || result.Failure.Field != field || !strings.Contains(result.Failure.Message, tc.message) || result.Failure.SideEffectsPossible {
					t.Fatalf("expected safe failure at %s containing %q: %+v %v", field, tc.message, result.Failure, err)
				}
				// Validation is also enforced for callers bypassing the tool parser.
				if _, err := s.create(t.Context(), f.execution, input); err == nil {
					t.Fatal("direct call accepted invalid proposal")
				}
				rows, err := s.List(t.Context(), f.owner, f.execution.AssistantID)
				need(t, err)
				messages, err := f.b.Services.Conversations.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "permission-chat"})
				need(t, err)
				receipts, err := persistence.Scan[core.ID](t.Context(), f.b.store, "security.permission_operations")
				need(t, err)
				if len(rows) != 0 || len(messages.Items) != 0 || len(receipts) != 0 {
					t.Fatal("invalid proposal left request, card or receipt")
				}
			})
		}
	}
}

func TestPermissionRequestRejectsUnknownModel(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := newPermissionRequestsFixture(t, f)
	configureTestModel(t, f.b, f.owner)
	models, err := f.b.Services.Models.List(t.Context(), f.owner)
	need(t, err)
	input := policy.PermissionRequestInput{ResourceKind: "model", ResourceID: models[0].ResourceID, ModelID: "nonexistent", ResourceDescription: "Model", Actions: []policy.Action{policy.Execute}, ExpiresAt: futurePermissionTime(), Reason: "Use model"}
	_, err = s.create(t.Context(), f.execution, input)
	var detail *core.Error
	if !errors.As(err, &detail) || detail.Field != "model_id" || !strings.Contains(detail.Message, "does not belong") {
		t.Fatalf("unexpected error: %v", err)
	}
	input.ModelID = models[0].ID
	_, err = s.create(t.Context(), f.execution, input)
	need(t, err)
}
