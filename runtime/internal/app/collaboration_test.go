package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
	"molr.ai/runtime/internal/storage/files"
	"strings"
	"sync"
	"testing"
	"time"
)

type collaborationFixture struct {
	b         *Bundle
	owner     core.Principal
	parent    assistant.Assistant
	execution core.Execution
	grant     policy.Grant
}

func collaborationTestFixture(t *testing.T, permanent ...bool) collaborationFixture {
	t.Helper()
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	t.Cleanup(func() { b.Close() })
	p, _ := ownerSession(t, b)
	modelID := configureTestModel(t, b, p)
	a, err := b.Services.Assistants.Hire(t.Context(), p, assistant.HireRequest{Name: "Coordinator", ModelID: modelID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 10000000}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "parent"}})
	need(t, err)
	grants, err := b.Services.Grants.List(t.Context(), p, policy.Query{AssistantID: a.ID})
	need(t, err)
	expires := time.Now().Add(time.Hour)
	deadline := &expires
	if len(permanent) > 0 && permanent[0] {
		deadline = nil
	}
	a, err = b.Services.Assistants.SaveHiring(t.Context(), p, assistant.HiringRequest{AssistantID: a.ID, Mutation: core.Mutation{IdempotencyKey: "hiring", ExpectedVersion: a.Version}, Authorization: assistant.HiringAuthorization{Enabled: true, MaxAssistants: 2, Budget: core.Money{Currency: "USD", Micros: 1000000}, GrantIDs: []core.ID{grants.Items[0].ID}, ExpiresAt: deadline}})
	if err != nil {
		t.Fatalf("save hiring: %v", err)
	}
	f := collaborationFixture{b: b, owner: p, parent: a, grant: grants.Items[0]}
	f.execution = f.running(t, a, "parent-run")
	return f
}
func (f collaborationFixture) running(t *testing.T, a assistant.Assistant, key string) core.Execution {
	t.Helper()
	e := core.Execution{Principal: core.Principal{OwnerID: f.owner.OwnerID, SubjectID: a.ID, Kind: core.ActorAssistant, ExecutionID: core.ID(key)}, AssistantID: a.ID, OperationID: core.ID("op-" + key)}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.jobs", persistence.Key(f.owner.OwnerID, core.ID(key)), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: core.ID(key), OwnerID: f.owner.OwnerID, AssistantID: a.ID}})
	}))
	return e
}
func (f collaborationFixture) hire(t *testing.T, key string) assistant.Assistant {
	t.Helper()
	a, err := f.b.Services.Assistants.HireTemporary(t.Context(), f.execution, assistant.TemporaryRequest{Name: key, Instructions: "Summarize the supplied material and report to your employer.", Mutation: core.Mutation{IdempotencyKey: key}})
	if err != nil {
		t.Fatalf("hire temporary: %v", err)
	}
	return a
}
func TestTemporaryPreauthorizationLifecycleAndIsolation(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t, true)
	ctx := t.Context()
	a := f.hire(t, "worker")
	if f.parent.Hiring.ExpiresAt != nil || a.ExpiresAt != nil {
		t.Fatal("permanent authorization acquired a deadline")
	}
	need(t, f.b.Services.Assistants.ExpireTemporary(ctx, time.Now().AddDate(10, 0, 0)))
	live, err := f.b.Services.Assistants.Get(ctx, f.owner, a.ID)
	need(t, err)
	if live.State != assistant.Active {
		t.Fatal("permanent authorization expired worker")
	}
	e := f.running(t, a, "worker-run")
	repeated := f.hire(t, "worker")
	if repeated.ID != a.ID {
		t.Fatal("duplicate worker")
	}
	list, err := f.b.Services.Assistants.Directory(ctx, e.Principal)
	need(t, err)
	if len(list) != 1 || list[0].AssistantID != f.parent.ID {
		t.Fatalf("directory escaped employer: %+v", list)
	}
	grant, err := policy.ActiveGrants(ctx, f.b.store, f.owner.OwnerID, policy.Query{AssistantID: a.ID}, time.Now())
	need(t, err)
	if len(grant) != 1 || grant[0].ID != f.grant.ID || grant[0].ExpiresAt != nil {
		t.Fatalf("source grant identity not shared: %+v", grant)
	}
	auth := f.b.Services.Grants.(policy.Authorizer)
	docs, err := (&permissionRequests{store: f.b.store, authorizer: auth}).context(ctx, e)
	need(t, err)
	if len(docs) != 1 || docs[0].Kind != "permissions" {
		t.Fatalf("temporary context must expose preauthorization without request tools: %+v", docs)
	}
	for _, tool := range []string{"assistant_hire", "assistant_dismiss", "permission_request", "permission_list", "plan_manage"} {
		if err := auth.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: core.ID(tool)}, Action: policy.Execute}); core.CodeOf(err) != core.Forbidden {
			t.Fatalf("allowed %s: %v", tool, err)
		}
	}
	if _, err := f.b.Services.Assistants.HireTemporary(ctx, e, assistant.TemporaryRequest{Name: "recursive", Instructions: "no", Mutation: core.Mutation{IdempotencyKey: "recursive"}}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("recursive hire: %v", err)
	}
	chat := f.b.Services.Conversations.(conversation.Service)
	for _, target := range []conversation.Endpoint{{Type: conversation.User, ID: f.owner.OwnerID}, {Type: conversation.Assistant, ID: "unrelated"}} {
		if _, err := chat.OpenDirect(ctx, e, target); err == nil {
			t.Fatal("escaped employer")
		}
	}
	c, err := chat.OpenDirect(ctx, e, conversation.Endpoint{Type: conversation.Assistant, ID: f.parent.ID})
	need(t, err)
	if c.ID != a.ConversationID {
		t.Fatal("duplicate employer conversation")
	}
	m, err := chat.Publish(ctx, e, conversation.PublishRequest{ConversationID: c.ID, Content: conversation.Content{Kind: conversation.Text, Text: "Completed summary"}, Mutation: core.Mutation{IdempotencyKey: "reply"}})
	need(t, err)
	if m.Receiver.ID != f.parent.ID || m.SenderID != a.ID {
		t.Fatal("incorrect addressed identity")
	}
	_, err = chat.SendUser(ctx, f.owner, conversation.SendUserRequest{ConversationID: c.ID, Content: conversation.Content{Kind: conversation.Text, Text: "impersonated"}, Mutation: core.Mutation{IdempotencyKey: "inject"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("owner injected direct chat: %v", err)
	}
	history, err := chat.Collaborations(ctx, f.owner, f.parent.ID, core.PageRequest{})
	need(t, err)
	if len(history.Items) != 1 {
		t.Fatalf("missing history: %+v", history)
	}
	need(t, chat.MarkRead(ctx, f.owner, conversation.ReadRequest{ConversationID: c.ID, ThroughSequence: m.Sequence}))
	loaded, err := conversation.LoadMessage(ctx, f.b.store, f.owner.OwnerID, m.ID)
	need(t, err)
	if loaded.ReadAt != nil {
		t.Fatal("owner read changed assistant receipt")
	}
	need(t, chat.ReadUserMessages(ctx, f.execution, []core.ID{m.ID}))
	loaded, err = conversation.LoadMessage(ctx, f.b.store, f.owner.OwnerID, m.ID)
	need(t, err)
	if loaded.ReadAt == nil {
		t.Fatal("model read receipt missing")
	}
	worker2 := f.hire(t, "second")
	_ = worker2
	if _, err := f.b.Services.Assistants.HireTemporary(ctx, f.execution, assistant.TemporaryRequest{Name: "third", Instructions: "no", Mutation: core.Mutation{IdempotencyKey: "third"}}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("slot limit: %v", err)
	}
	ended, err := f.b.Services.Assistants.EndTemporary(ctx, f.execution.Principal, assistant.LifecycleRequest{ID: a.ID, Mutation: core.Mutation{IdempotencyKey: "end", ExpectedVersion: a.Version}})
	need(t, err)
	if ended.State != assistant.Dismissed {
		t.Fatal("not dismissed")
	}
	// Completing employment must not invalidate already-delivered model inputs.
	need(t, chat.ReadUserMessages(ctx, f.execution, []core.ID{m.ID}))
	_, err = chat.Messages(ctx, f.execution.Principal, conversation.MessageQuery{ConversationID: c.ID})
	need(t, err)
	if _, err := chat.Publish(ctx, e, conversation.PublishRequest{ConversationID: c.ID, Content: conversation.Content{Kind: conversation.Text, Text: "late"}, Mutation: core.Mutation{IdempotencyKey: "late"}}); err == nil {
		t.Fatal("late publish reactivated ended worker")
	}
	f.hire(t, "replacement")
	h := *f.parent.Hiring
	h.Enabled = false
	_, err = f.b.Services.Assistants.SaveHiring(ctx, f.owner, assistant.HiringRequest{AssistantID: f.parent.ID, Authorization: h, Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: f.parent.Version}})
	need(t, err)
	need(t, f.b.Services.Assistants.ExpireTemporary(ctx, time.Now()))
	rows, err := f.b.Services.Assistants.List(ctx, f.owner, assistant.Query{EmployerID: f.parent.ID, IncludeTemporary: true})
	need(t, err)
	for _, v := range rows.Items {
		if v.State != assistant.Dismissed {
			t.Fatal("revocation left worker active")
		}
	}
	page, err := chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: c.ID})
	need(t, err)
	if len(page.Items) != 2 {
		t.Fatal("history lost on dismissal")
	}
}
func TestTemporaryConcurrentSlotsAndBudget(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t, true)
	ctx := t.Context()
	var wg sync.WaitGroup
	results := make(chan assistant.Assistant, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := f.b.Services.Assistants.HireTemporary(ctx, f.execution, assistant.TemporaryRequest{Name: fmt.Sprint(i), Instructions: "work", Mutation: core.Mutation{IdempotencyKey: fmt.Sprint(i)}})
			if err == nil {
				results <- a
			} else if core.CodeOf(err) != core.FailedPrecondition {
				t.Errorf("hire: %v", err)
			}
		}()
	}
	wg.Wait()
	close(results)
	var children []assistant.Assistant
	for a := range results {
		children = append(children, a)
	}
	if len(children) != 2 {
		t.Fatalf("concurrent slots: %d", len(children))
	}
	budget := f.b.Services.Budget.(policy.Budget)
	errors := make(chan error, 2)
	for i, a := range children {
		e := f.running(t, a, fmt.Sprintf("budget-%d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := budget.Reserve(ctx, policy.ReserveRequest{Execution: e, ModelID: a.ModelID, Estimate: core.Money{Currency: "USD", Micros: 600000}})
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	success := 0
	for err := range errors {
		if err == nil {
			success++
		} else if core.CodeOf(err) != core.BudgetExceeded {
			t.Fatalf("budget error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("shared budget admitted %d", success)
	}
	for _, a := range children {
		_, err := f.b.Services.Assistants.EndTemporary(ctx, f.execution.Principal, assistant.LifecycleRequest{ID: a.ID, Mutation: core.Mutation{IdempotencyKey: string(a.ID), ExpectedVersion: a.Version}})
		need(t, err)
	}
	replacement := f.hire(t, "new-worker")
	e := f.running(t, replacement, "replacement-budget")
	_, err := budget.Reserve(ctx, policy.ReserveRequest{Execution: e, ModelID: replacement.ModelID, Estimate: core.Money{Currency: "USD", Micros: 600000}})
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("rehire reset budget: %v", err)
	}
	records, err := quota.ModelRecords(ctx, f.b.store, f.owner.OwnerID, "", "", time.Time{}, time.Time{})
	need(t, err)
	if len(records) != 1 {
		t.Fatalf("rejected reservation persisted: %d", len(records))
	}
}
func TestTemporaryCreateRollbackAndExpiry(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t)
	ctx := t.Context()
	if err := f.b.Services.Agent.(*agent.Engine).TemporaryLifecycle(ctx, f.owner.OwnerID, f.parent.ID, "stop"); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("temporary lifecycle accepted permanent identity: %v", err)
	}
	f.b.store.(*sqlitestore.Store).FailNextCommit(errors.New("injected failure"))
	_, err := f.b.Services.Assistants.HireTemporary(ctx, f.execution, assistant.TemporaryRequest{Name: "failed", Instructions: "work", Mutation: core.Mutation{IdempotencyKey: "failed"}})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	rows, err := f.b.Services.Assistants.List(ctx, f.owner, assistant.Query{EmployerID: f.parent.ID})
	need(t, err)
	if len(rows.Items) != 0 {
		t.Fatal("rollback left worker")
	}
	a := f.hire(t, "expiry")
	need(t, f.b.Services.Assistants.ExpireTemporary(ctx, a.ExpiresAt.Add(time.Second)))
	got, err := f.b.Services.Assistants.Get(ctx, f.owner, a.ID)
	need(t, err)
	if got.State != assistant.Dismissed {
		t.Fatal("expired worker survived")
	}
	status, err := f.b.Services.Agent.Status(ctx, f.owner, a.ID)
	need(t, err)
	if status.State != agent.Stopped {
		t.Fatal("expired loop still running")
	}
	notices, err := persistence.Select[event.AssistantEvent](ctx, f.b.store, "agent.events", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", event.AssistantNotice)}})
	need(t, err)
	if len(notices) != 1 || notices[0].AssistantID != f.parent.ID {
		t.Fatal("expiry did not notify employer")
	}

}

func TestAssistantDirectMessagesWakeOnlyReceiverAndPreserveHistory(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t)
	ctx := t.Context()
	f.parent = f.communication(t, f.parent, true, "enable-parent")
	chat := f.b.Services.Conversations.(conversation.Service)
	other, err := f.b.Services.Assistants.Hire(ctx, f.owner, assistant.HireRequest{Name: "Specialist", ModelID: f.parent.ModelID, Budget: f.parent.Budget, Mutation: core.Mutation{IdempotencyKey: "specialist"}})
	need(t, err)
	other = f.communication(t, other, true, "enable-peer")
	e := f.running(t, other, "specialist-run")
	c, err := chat.OpenDirect(ctx, f.execution, conversation.Endpoint{Type: conversation.Assistant, ID: other.ID})
	need(t, err)
	reverse, err := chat.OpenDirect(ctx, e, conversation.Endpoint{Type: conversation.Assistant, ID: f.parent.ID})
	need(t, err)
	if reverse.ID != c.ID {
		t.Fatal("direct conversation not symmetric")
	}
	consumer := scheduler.NewMessageConsumer(f.b.store, scheduler.Options{})
	for i := range 2 {
		m, err := chat.Publish(ctx, f.execution, conversation.PublishRequest{Receiver: &conversation.Endpoint{Type: conversation.Assistant, ID: other.ID}, Content: conversation.Content{Kind: conversation.Text, Text: fmt.Sprintf("request %d", i)}, Mutation: core.Mutation{IdempotencyKey: fmt.Sprintf("send-%d", i)}})
		need(t, err)
		if m.Receiver.ID != other.ID {
			t.Fatal("receiver identity lost")
		}
		pending, err := conversation.NewOutbox(f.b.store).Pending(ctx, 100)
		need(t, err)
		for _, out := range pending {
			need(t, consumer.AcceptMessage(ctx, out))
			need(t, consumer.AcceptMessage(ctx, out))
		}
	}
	jobs, err := persistence.Select[event.AssistantEvent](ctx, f.b.store, "agent.events", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", event.AssistantMessage)}})
	need(t, err)
	if len(jobs) != 2 {
		t.Fatalf("messages collapsed or duplicated: %d", len(jobs))
	}
	for _, j := range jobs {
		if j.AssistantID != other.ID || j.ConversationID != c.ID || j.MessageSequence != 0 {
			t.Fatalf("wrong event routing: %+v", j)
		}
	}
	if _, err := chat.Messages(ctx, e.Principal, conversation.MessageQuery{ConversationID: f.parent.ConversationID}); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("private user history leaked: %v", err)
	}
	f.b.store.(*sqlitestore.Store).FailNextCommit(errors.New("message rollback"))
	_, err = chat.Publish(ctx, e, conversation.PublishRequest{ConversationID: c.ID, Content: conversation.Content{Kind: conversation.Text, Text: "must rollback"}, Mutation: core.Mutation{IdempotencyKey: "rollback"}})
	if err == nil {
		t.Fatal("expected rollback")
	}
	history, err := chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: c.ID})
	need(t, err)
	if len(history.Items) != 2 {
		t.Fatal("rollback left message")
	}
	// A stopped parent's old incoming assistant events stay canceled even when
	// the durable outbox is consumed after Stop.
	late, err := chat.Publish(ctx, e, conversation.PublishRequest{ConversationID: c.ID, Content: conversation.Content{Kind: conversation.Text, Text: "old reply"}, Mutation: core.Mutation{IdempotencyKey: "old-reply"}})
	need(t, err)
	need(t, f.b.Services.Agent.Stop(ctx, f.owner, f.parent.ID))
	pending, err := conversation.NewOutbox(f.b.store).Pending(ctx, 100)
	need(t, err)
	for _, out := range pending {
		need(t, consumer.AcceptMessage(ctx, out))
	}
	q, err := scheduler.NewQueue(f.b.store).List(ctx, f.owner.OwnerID, scheduler.Query{AssistantID: f.parent.ID})
	need(t, err)
	found := false
	for _, job := range q.Items {
		if job.Event.MessageID == late.ID {
			found = true
			if job.State != scheduler.Canceled {
				t.Fatalf("late outbox crossed stop boundary: %s", job.State)
			}
		}
	}
	if !found {
		t.Fatal("missing canceled source")
	}
}

func TestDismissEmployerFencesPendingMessagesAndEndsChildren(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t)
	ctx := t.Context()
	f.parent = f.communication(t, f.parent, true, "enable-parent")
	child := f.hire(t, "worker")
	other, err := f.b.Services.Assistants.Hire(ctx, f.owner, assistant.HireRequest{Name: "Peer", ModelID: f.parent.ModelID, Budget: f.parent.Budget, Mutation: core.Mutation{IdempotencyKey: "peer"}})
	need(t, err)
	other = f.communication(t, other, true, "enable-peer")
	peer := f.running(t, other, "peer-run")
	chat := f.b.Services.Conversations.(conversation.Service)
	m, err := chat.Publish(ctx, peer, conversation.PublishRequest{Receiver: &conversation.Endpoint{Type: conversation.Assistant, ID: f.parent.ID}, Content: conversation.Content{Kind: conversation.Text, Text: "old request"}, Mutation: core.Mutation{IdempotencyKey: "old"}})
	need(t, err)
	ended, err := f.b.Services.Assistants.Dismiss(ctx, f.owner, assistant.LifecycleRequest{ID: f.parent.ID, Mutation: core.Mutation{IdempotencyKey: "dismiss-parent", ExpectedVersion: f.parent.Version}})
	need(t, err)
	got, err := f.b.Services.Assistants.Get(ctx, f.owner, child.ID)
	need(t, err)
	if got.State != assistant.Dismissed {
		t.Fatal("dismissal did not end worker atomically")
	}
	_, err = f.b.Services.Assistants.Rehire(ctx, f.owner, assistant.RehireRequest{LifecycleRequest: assistant.LifecycleRequest{ID: ended.ID, Mutation: core.Mutation{IdempotencyKey: "rehire", ExpectedVersion: ended.Version}}})
	need(t, err)
	pending, err := conversation.NewOutbox(f.b.store).Pending(ctx, 100)
	need(t, err)
	consumer := scheduler.NewMessageConsumer(f.b.store, scheduler.Options{})
	for _, item := range pending {
		need(t, consumer.AcceptMessage(ctx, item))
	}
	jobs, err := scheduler.NewQueue(f.b.store).List(ctx, f.owner.OwnerID, scheduler.Query{AssistantID: f.parent.ID})
	need(t, err)
	found := false
	for _, j := range jobs.Items {
		if j.Event.MessageID == m.ID {
			found = true
			if j.State != scheduler.Canceled {
				t.Fatal("rehire reactivated old direct message")
			}
		}
	}
	if !found {
		t.Fatal("old source not fenced")
	}
	got, err = f.b.Services.Assistants.Get(ctx, f.owner, child.ID)
	need(t, err)
	if got.State != assistant.Dismissed {
		t.Fatal("rehire reactivated old worker")
	}
}

func TestTemporaryAttachmentsStayScopedAndSurviveDismissal(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t)
	ctx := t.Context()
	a := f.hire(t, "author")
	e := f.running(t, a, "author-run")
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.tasks", persistence.Key(f.owner.OwnerID, "worker-task"), task.Task{ID: "worker-task", OwnerID: f.owner.OwnerID, AssistantID: a.ID, State: task.Running})
	}))
	file, err := f.b.Services.Files.Upload(ctx, e.Principal, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "artifact"}, Name: "result.txt", MediaType: "text/plain", Size: 6, TaskID: "worker-task", Body: strings.NewReader("result")})
	need(t, err)
	if _, err := f.b.Services.Files.Get(ctx, f.execution.Principal, file.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("unshared worker file leaked: %v", err)
	}
	chat := f.b.Services.Conversations.(conversation.Service)
	_, err = chat.Publish(ctx, e, conversation.PublishRequest{ConversationID: a.ConversationID, Mutation: core.Mutation{IdempotencyKey: "deliver"}, Content: conversation.Content{Kind: conversation.Text, Text: "Delivered", AttachmentIDs: []core.ID{file.ID}}})
	need(t, err)
	_, err = f.b.Services.Files.Get(ctx, f.execution.Principal, file.ID)
	need(t, err)
	sibling := f.hire(t, "sibling")
	siblingExecution := f.running(t, sibling, "sibling-run")
	if _, err := f.b.Services.Files.Get(ctx, siblingExecution.Principal, file.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("sibling read attachment: %v", err)
	}
	_, err = f.b.Services.Assistants.EndTemporary(ctx, f.execution.Principal, assistant.LifecycleRequest{ID: a.ID, Mutation: core.Mutation{IdempotencyKey: "end", ExpectedVersion: a.Version}})
	need(t, err)
	download, err := f.b.Services.Files.Open(ctx, f.execution.Principal, file.ID)
	need(t, err)
	defer download.Body.Close()
	body, err := io.ReadAll(download.Body)
	need(t, err)
	if string(body) != "result" {
		t.Fatal("delivered attachment lost")
	}
}

func TestPermanentHiringRetainsSourceExpiryAndCanBecomeFinite(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t, true)
	ctx := t.Context()
	now := time.Now()
	sourceExpiry := now.Add(time.Hour)
	f.grant.ExpiresAt = &sourceExpiry
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.grants", persistence.Key(f.owner.OwnerID, f.grant.ID), f.grant)
	}))
	a := f.hire(t, "permanent-worker")
	grants, err := policy.ActiveGrants(ctx, f.b.store, f.owner.OwnerID, policy.Query{AssistantID: a.ID}, now)
	need(t, err)
	if len(grants) != 1 || grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(sourceExpiry) {
		t.Fatalf("permanent hiring extended source permission: %+v", grants)
	}
	grants, err = policy.ActiveGrants(ctx, f.b.store, f.owner.OwnerID, policy.Query{AssistantID: a.ID}, sourceExpiry.Add(time.Second))
	need(t, err)
	if len(grants) != 0 {
		t.Fatal("expired source permission remained available")
	}
	h := *f.parent.Hiring
	past := now.Add(-time.Hour)
	h.ExpiresAt = &past
	_, err = f.b.Services.Assistants.SaveHiring(ctx, f.owner, assistant.HiringRequest{AssistantID: f.parent.ID, Authorization: h, Mutation: core.Mutation{IdempotencyKey: "past", ExpectedVersion: f.parent.Version}})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("accepted expired authorization: %v", err)
	}
	deadline := now.Add(time.Minute)
	h.ExpiresAt = &deadline
	_, err = f.b.Services.Assistants.SaveHiring(ctx, f.owner, assistant.HiringRequest{AssistantID: f.parent.ID, Authorization: h, Mutation: core.Mutation{IdempotencyKey: "finite", ExpectedVersion: f.parent.Version}})
	need(t, err)
	grants, err = policy.ActiveGrants(ctx, f.b.store, f.owner.OwnerID, policy.Query{AssistantID: a.ID}, now)
	need(t, err)
	if len(grants) != 1 || grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(deadline) {
		t.Fatalf("new authorization deadline not applied: %+v", grants)
	}
	need(t, f.b.Services.Assistants.ExpireTemporary(ctx, deadline.Add(time.Second)))
	ended, err := f.b.Services.Assistants.Get(ctx, f.owner, a.ID)
	need(t, err)
	if ended.State != assistant.Dismissed {
		t.Fatal("new finite authorization failed to stop permanent worker")
	}
}

func (f collaborationFixture) communication(t *testing.T, a assistant.Assistant, enabled bool, key string) assistant.Assistant {
	t.Helper()
	updated, err := f.b.Services.Assistants.SetPeerCommunication(t.Context(), f.owner, assistant.PeerCommunicationRequest{AssistantID: a.ID, Authorization: assistant.PeerCommunicationAuthorization{Enabled: enabled, AllAssistants: true}, Mutation: core.Mutation{IdempotencyKey: key, ExpectedVersion: a.Version}})
	need(t, err)
	return updated
}

func TestPeerCommunicationGatesDirectoryAndBothMessageDirections(t *testing.T) {
	t.Parallel()
	f := collaborationTestFixture(t, true)
	ctx := t.Context()
	chat := f.b.Services.Conversations.(conversation.Service)
	worker := f.hire(t, "worker")
	workerExecution := f.running(t, worker, "worker-run")
	peer, err := f.b.Services.Assistants.Hire(ctx, f.owner, assistant.HireRequest{Name: "Peer", ModelID: f.parent.ModelID, Budget: f.parent.Budget, Mutation: core.Mutation{IdempotencyKey: "peer"}})
	need(t, err)
	peerExecution := f.running(t, peer, "peer-run")
	directory := func(expected ...core.ID) {
		t.Helper()
		rows, err := f.b.Services.Assistants.Directory(ctx, f.execution.Principal)
		need(t, err)
		if len(rows) != len(expected) {
			t.Fatalf("directory: %+v, want %v", rows, expected)
		}
		for _, id := range expected {
			found := false
			for _, row := range rows {
				if row.AssistantID == id {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing visible assistant %s", id)
			}
		}
	}
	publish := func(e core.Execution, target core.ID, key string) (conversation.Message, error) {
		return chat.Publish(ctx, e, conversation.PublishRequest{Receiver: &conversation.Endpoint{Type: conversation.Assistant, ID: target}, Content: conversation.Content{Kind: conversation.Text, Text: key}, Mutation: core.Mutation{IdempotencyKey: key}})
	}
	deniedMessage := func(e core.Execution, target core.ID, key string) {
		t.Helper()
		_, err := publish(e, target, key)
		if core.CodeOf(err) != core.Forbidden {
			t.Fatalf("peer message allowed: %v", err)
		}
	}
	directory(worker.ID)
	deniedMessage(f.execution, peer.ID, "both-disabled")
	f.parent = f.communication(t, f.parent, true, "enable-parent")
	directory(worker.ID)
	deniedMessage(f.execution, peer.ID, "receiver-disabled")
	deniedMessage(peerExecution, f.parent.ID, "sender-disabled")
	peer = f.communication(t, peer, true, "enable-peer")
	directory(worker.ID, peer.ID)
	other, err := f.b.Services.Assistants.Hire(ctx, f.owner, assistant.HireRequest{Name: "Outside scope", ModelID: f.parent.ModelID, Budget: f.parent.Budget, Mutation: core.Mutation{IdempotencyKey: "outside-peer"}})
	need(t, err)
	other = f.communication(t, other, true, "enable-outside-peer")
	f.parent, err = f.b.Services.Assistants.SetPeerCommunication(ctx, f.owner, assistant.PeerCommunicationRequest{AssistantID: f.parent.ID, Authorization: assistant.PeerCommunicationAuthorization{Enabled: true, AssistantIDs: []core.ID{peer.ID}}, Mutation: core.Mutation{IdempotencyKey: "selected-peer", ExpectedVersion: f.parent.Version}})
	need(t, err)
	directory(worker.ID, peer.ID)
	deniedMessage(f.execution, other.ID, "outside-scope")
	peer, err = f.b.Services.Assistants.SetPeerCommunication(ctx, f.owner, assistant.PeerCommunicationRequest{AssistantID: peer.ID, Authorization: assistant.PeerCommunicationAuthorization{Enabled: true, AssistantIDs: []core.ID{other.ID}}, Mutation: core.Mutation{IdempotencyKey: "peer-excludes-parent", ExpectedVersion: peer.Version}})
	need(t, err)
	directory(worker.ID)
	deniedMessage(f.execution, peer.ID, "receiver-excludes-parent")
	peer = f.communication(t, peer, true, "restore-peer-scope")
	past := time.Now().Add(-time.Hour)
	for i, authorization := range []assistant.PeerCommunicationAuthorization{
		{Enabled: true},
		{Enabled: true, AllAssistants: true, ExpiresAt: &past},
		{Enabled: true, AssistantIDs: []core.ID{worker.ID}},
		{Enabled: true, AssistantIDs: []core.ID{f.parent.ID}},
	} {
		_, err := f.b.Services.Assistants.SetPeerCommunication(ctx, f.owner, assistant.PeerCommunicationRequest{AssistantID: f.parent.ID, Authorization: authorization, Mutation: core.Mutation{IdempotencyKey: fmt.Sprintf("invalid-scope-%d", i), ExpectedVersion: f.parent.Version}})
		if core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("invalid authorization accepted: %v", err)
		}
	}

	pending, err := publish(f.execution, peer.ID, "pending")
	need(t, err)
	peer = f.communication(t, peer, false, "disable-peer")
	directory(worker.ID)
	deniedMessage(f.execution, peer.ID, "disabled-receiver-again")
	deniedMessage(peerExecution, f.parent.ID, "disabled-sender-again")
	_, err = chat.Publish(ctx, f.execution, conversation.PublishRequest{ConversationID: pending.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "old chat"}, Mutation: core.Mutation{IdempotencyKey: "old-chat"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("existing chat bypassed permission: %v", err)
	}
	if err = chat.AcceptUserMessage(ctx, peerExecution, pending.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("disabled peer accepted input: %v", err)
	}
	history, err := chat.Messages(ctx, f.owner, conversation.MessageQuery{ConversationID: pending.ConversationID})
	need(t, err)
	if len(history.Items) != 1 {
		t.Fatal("owner lost history")
	}
	history, err = chat.Messages(ctx, peerExecution.Principal, conversation.MessageQuery{ConversationID: pending.ConversationID})
	need(t, err)
	if len(history.Items) != 1 {
		t.Fatal("disabling communication erased shared history")
	}
	peer = f.communication(t, peer, true, "reenable-peer")
	if err = chat.AcceptUserMessage(ctx, peerExecution, pending.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("old input revived after reenable: %v", err)
	}
	fresh, err := publish(f.execution, peer.ID, "fresh")
	need(t, err)
	need(t, chat.AcceptUserMessage(ctx, peerExecution, fresh.ID))
	f.parent = f.communication(t, f.parent, false, "disable-parent")
	directory(worker.ID)
	_, err = publish(f.execution, worker.ID, "employer-request")
	need(t, err)
	_, err = publish(workerExecution, f.parent.ID, "worker-reply")
	need(t, err)
	rows, err := f.b.Services.Assistants.Directory(ctx, workerExecution.Principal)
	need(t, err)
	if len(rows) != 1 || rows[0].AssistantID != f.parent.ID {
		t.Fatalf("worker directory: %+v", rows)
	}
	_, err = f.b.Services.Assistants.SetPeerCommunication(ctx, f.execution.Principal, assistant.PeerCommunicationRequest{AssistantID: f.parent.ID, Authorization: assistant.PeerCommunicationAuthorization{Enabled: true, AllAssistants: true}, Mutation: core.Mutation{IdempotencyKey: "self-authorize", ExpectedVersion: f.parent.Version}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("assistant changed own authorization: %v", err)
	}
	_, err = f.b.Services.Assistants.SetPeerCommunication(ctx, f.owner, assistant.PeerCommunicationRequest{AssistantID: worker.ID, Authorization: assistant.PeerCommunicationAuthorization{Enabled: true, AllAssistants: true}, Mutation: core.Mutation{IdempotencyKey: "worker-authorize", ExpectedVersion: worker.Version}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("temporary assistant enabled peer communication: %v", err)
	}
	_, err = chat.SendUser(ctx, f.owner, conversation.SendUserRequest{ConversationID: f.parent.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "user message"}, Mutation: core.Mutation{IdempotencyKey: "user-unaffected"}})
	need(t, err)
}
