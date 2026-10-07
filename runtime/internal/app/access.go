package app

import (
	"context"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
	"strings"
)

type access struct{ store persistence.Store }

func (a access) grantScope(ctx context.Context, p core.Principal, id core.ID, scope policy.Scope) error {
	if err := groupAccess(ctx, a.store, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, ModelID: scope.ModelID, Action: policy.Read}); err != nil {
		return err
	}
	repo := resource.NewRepository(a.store)
	r, err := repo.Get(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	if scope.FullAccess && r.Kind != resource.Compute {
		return core.Invalid("full_access", "Full Access is available only for compute resources")
	}
	if r.Kind != resource.Model {
		if scope.ModelID != "" {
			return core.Invalid("model_id", "resource is not a model provider")
		}
		return nil
	}
	if scope.ModelID.Validate() != nil || len(scope.PathPrefixes) != 0 {
		return core.Invalid("model_id", "select a model instead of provider-wide access")
	}
	b, err := repo.Binding(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	for _, entry := range modelEntries(r, b) {
		if entry.id == scope.ModelID {
			return nil
		}
	}
	return core.Invalid("model_id", "model does not belong to provider")
}

func (a access) resourceActions(ctx context.Context, p core.Principal, id core.ID) ([]string, error) {
	r, err := resource.NewRepository(a.store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return nil, err
	}
	if r.State != resource.Available {
		return nil, nil
	}
	return r.Actions, nil
}

func denied() error { return core.NewError(core.Forbidden, "object access denied") }
func (a access) assistant(ctx context.Context, p core.Principal, id core.ID) error {
	if id.Validate() != nil {
		return core.Invalid("assistant_id", "required")
	}
	if p.Kind == core.ActorAssistant && p.SubjectID != id {
		return denied()
	}
	v, err := persistence.Load[assistant.Assistant](ctx, a.store, "business.assistants", persistence.Key(p.OwnerID, id))
	if err != nil {
		return err
	}
	if v.OwnerID != p.OwnerID || v.State != assistant.Active {
		return denied()
	}
	return nil
}
func (a access) liveAssistant(ctx context.Context, p core.Principal) error {
	if p.Kind != core.ActorAssistant {
		return denied()
	}
	if err := a.assistant(ctx, p, p.SubjectID); err != nil {
		return err
	}
	if managedPrincipal(ctx, p) {
		return nil
	}
	jobs, err := persistence.Scan[scheduler.Job](ctx, a.store, "agent.jobs")
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.State == scheduler.Running && job.Event.OwnerID == p.OwnerID && job.Event.AssistantID == p.SubjectID && job.ExecutionID == p.ExecutionID {
			return nil
		}
	}
	return denied()
}
func (a access) execution(ctx context.Context, e core.Execution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := a.liveAssistant(ctx, e.Principal); err != nil {
		return err
	}
	if managedShellFor(ctx, e) != nil {
		return nil
	}
	if e.TaskID != "" {
		v, err := persistence.Load[task.Task](ctx, a.store, "business.tasks", persistence.Key(e.Principal.OwnerID, e.TaskID))
		if err != nil {
			return err
		}
		if v.AssistantID != e.AssistantID || v.State == task.Paused || v.State == task.Canceled || v.State == task.Completed || v.State == task.Failed {
			return denied()
		}
	}
	return nil
}
func (a access) owns(ctx context.Context, p core.Principal, o core.Object) error {
	if o.Kind == "resource" && o.ID != "" {
		_, err := resource.NewRepository(a.store).Get(ctx, p.OwnerID, o.ID)
		return err
	}
	owner := p.Kind == core.ActorOwner
	// Collection authorization is followed by each service's query binding and
	// per-object checks. Collections without assistant filtering remain owner-only.
	if o.ID == "" {
		switch o.Kind {
		case "assistant", "settings":
			if owner {
				return nil
			}
		case "task", "memory", "search", "resource", "todo", "schedule":
			return nil
		}
		if o.Kind == "file" {
			return nil
		}
		return denied()
	}
	if o.Kind == "tool" {
		if strings.HasPrefix(string(o.ID), "plg_") && len(o.ID) == 52 {
			return nil
		}
		switch o.ID {
		case "profile_update", "permission_list", "permission_request", "shell_execute", "shell_task", "catalog", "conversation_read", "conversation_publish", "file_search", "file_read", "file_list", "file_write", "file_edit", "file_transfer", "conversation_attachment", "plan_list", "plan_read", "plan_manage":
			return nil
		}
		return denied()
	}
	if o.Kind == "model" && o.ID == "catalog" {
		return nil
	}
	if o.ID == p.OwnerID {
		switch o.Kind {
		case "settings", "realtime", "scheduler", "conversation", "operation":
			if owner {
				return nil
			}
		case "model":
			return nil
		}
	}
	if o.Kind == "message" {
		m, err := persistence.Load[conversation.Message](ctx, a.store, "im.messages", persistence.Key(p.OwnerID, o.ID))
		if err != nil {
			return err
		}
		return a.owns(ctx, p, core.Object{Kind: "conversation", ID: m.ConversationID})
	}
	buckets := map[string]string{"permission_request": "security.permission_requests", "assistant": "business.assistants", "conversation": "im.conversations", "task": "business.tasks", "todo": "business.todos", "schedule": "business.schedules", "resource": "business.resources", "file": files.Bucket, "operation": "agent.operations", "settings": "business.exports", "search": "search.jobs"}
	bucket, ok := buckets[o.Kind]
	if !ok {
		return denied()
	}
	type record struct {
		OwnerID, AssistantID, PeerID, TaskID core.ID
		Scope                                string
	}
	row, err := persistence.Load[record](ctx, a.store, bucket, persistence.Key(p.OwnerID, o.ID))
	if persistence.Missing(err) && owner && o.Kind == "assistant" {
		row, err = persistence.Load[record](ctx, a.store, "business.groups", persistence.Key(p.OwnerID, o.ID))
	}
	if err != nil {
		return err
	}
	// Export/rebuild records are owner-keyed and do not duplicate OwnerID.
	if row.OwnerID != p.OwnerID && !(owner && (o.Kind == "settings" || o.Kind == "search")) {
		return denied()
	}
	if owner {
		return nil
	}
	switch o.Kind {
	case "assistant":
		if o.ID == p.SubjectID {
			return nil
		}
	case "conversation":
		if row.PeerID == p.SubjectID {
			return nil
		}
	case "task", "todo", "schedule", "operation", "permission_request":
		if row.AssistantID == p.SubjectID {
			return nil
		}
	case "resource":
		return nil // policy additionally requires a live resource grant.
	case "file":
		if row.AssistantID == p.SubjectID {
			return nil
		}
		if row.TaskID != "" {
			return a.owns(ctx, p, core.Object{Kind: "task", ID: row.TaskID})
		}
		// A file is visible to an assistant only through an attachment in its own
		// conversation; mere owner equality is insufficient.
		messages, err := persistence.Scan[conversation.Message](ctx, a.store, "im.messages")
		if err != nil {
			return err
		}
		for _, m := range messages {
			for _, id := range m.Content.AttachmentIDs {
				if id == o.ID && a.owns(ctx, p, core.Object{Kind: "conversation", ID: m.ConversationID}) == nil {
					return nil
				}
			}
		}
	}
	return denied()
}
func (a access) verifyReceipt(ctx context.Context, e core.Execution, id core.ID, amount core.Money) error {
	r, err := model.NewReceipts(a.store).Get(ctx, e.Principal.OwnerID, id)
	if err != nil {
		return err
	}
	if r.OperationID != e.OperationID || !r.OutcomeKnown || r.Charged == nil || *r.Charged != amount {
		return denied()
	}
	return nil
}
func (a access) noCharge(ctx context.Context, e core.Execution) (bool, error) {
	r, err := model.NewReceipts(a.store).ByOperation(ctx, e.Principal.OwnerID, e.OperationID)
	if err == nil {
		return r.OutcomeKnown && r.Charged != nil && r.Charged.Micros == 0, nil
	}
	if !persistence.Missing(err) {
		return false, err
	}
	op, err := operation.NewRepository(a.store).Get(ctx, e.Principal.OwnerID, e.OperationID)
	if persistence.Missing(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return op.State == operation.Prepared, nil
}

// historicalExecution is solely a trusted settlement capability: it cannot
// authorize a new tool/model call. Reservations and receipts are subsequently
// checked against this persisted execution and operation by policy.
func (a access) historicalExecution(ctx context.Context, e core.Execution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	jobs, err := persistence.Scan[scheduler.Job](ctx, a.store, "agent.jobs")
	if err != nil {
		return err
	}
	matched := false
	for _, job := range jobs {
		if job.Event.OwnerID == e.Principal.OwnerID && job.Event.AssistantID == e.AssistantID && job.ExecutionID == e.Principal.ExecutionID && job.Event.TaskID == e.TaskID {
			matched = true
			break
		}
	}
	if !matched {
		return denied()
	}
	op, err := operation.NewRepository(a.store).Get(ctx, e.Principal.OwnerID, e.OperationID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if op.OwnerID != e.Principal.OwnerID || op.AssistantID != e.AssistantID || op.ExecutionID != e.Principal.ExecutionID || op.TaskID != e.TaskID {
		return denied()
	}
	return nil
}
