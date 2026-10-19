// Package objectaccess checks domain ownership through module-owned readers.
// It does not grant resource capabilities or authenticate principals.
package objectaccess

import (
	"context"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
	"strings"
)

type Service struct{ Store persistence.Store }

func denied() error { return core.NewError(core.Forbidden, "object access denied") }
func (s Service) Require(ctx context.Context, p core.Principal, o core.Object) error {
	if o.Kind == "resource" && o.ID != "" {
		_, err := resource.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
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
		if (strings.HasPrefix(string(o.ID), "plg_") || strings.HasPrefix(string(o.ID), "mcp_")) && len(o.ID) == 52 {
			return nil
		}
		switch o.ID {
		case "browser_session", "browser_action", "profile_update", "permission_list", "permission_request", "shell_execute", "shell_task", "catalog", "conversation_read", "conversation_publish", "file_search", "file_read", "file_list", "file_write", "file_edit", "file_move", "file_delete", "file_transfer", "conversation_attachment", "plan_list", "plan_read", "plan_manage":
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
		m, err := conversation.LoadMessage(ctx, s.Store, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return s.Require(ctx, p, core.Object{Kind: "conversation", ID: m.ConversationID})
	}

	switch o.Kind {
	case "assistant":
		v, err := assistant.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if persistence.Missing(err) && owner {
			_, err = assistant.GetGroup(ctx, s.Store, p.OwnerID, o.ID)
			return err
		}
		if err != nil {
			return err
		}
		if v.State == assistant.Cloning || v.OwnerID != p.OwnerID {
			return denied()
		}
		if owner || o.ID == p.SubjectID {
			return nil
		}
	case "conversation":
		v, err := conversation.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		peer, err := assistant.NewRepository(s.Store).Get(ctx, p.OwnerID, v.PeerID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if v.OwnerID != p.OwnerID || peer.State == assistant.Cloning {
			return denied()
		}
		if owner || v.PeerID == p.SubjectID {
			return nil
		}
	case "task":
		v, err := task.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return assigned(p, v.OwnerID, v.AssistantID)
	case "todo":
		v, err := todo.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return assigned(p, v.OwnerID, v.AssistantID)
	case "schedule":
		v, err := schedule.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return assigned(p, v.OwnerID, v.AssistantID)
	case "operation":
		v, err := operation.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return assigned(p, v.OwnerID, v.AssistantID)
	case "permission_request":
		v, err := policy.LoadPermissionRequest(ctx, s.Store, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		return assigned(p, v.OwnerID, v.AssistantID)
	case "settings":
		if !owner {
			return denied()
		}
		_, err := settings.NewRepository(s.Store).Export(ctx, p.OwnerID, o.ID)
		return err
	case "search":
		if !owner {
			return denied()
		}
		_, err := search.LoadStatus(ctx, s.Store, p.OwnerID, o.ID)
		return err
	case "file":
		v, err := files.NewRepository(s.Store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		if v.OwnerID != p.OwnerID {
			return denied()
		}
		if owner || v.AssistantID == p.SubjectID {
			return nil
		}
		if v.TaskID != "" {
			return s.Require(ctx, p, core.Object{Kind: "task", ID: v.TaskID})
		}
		return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			chats, err := conversation.AttachmentConversations(ctx, tx.Store(), p.OwnerID, o.ID)
			if err != nil {
				return err
			}
			for _, id := range chats {
				if s.Require(ctx, p, core.Object{Kind: "conversation", ID: id}) == nil {
					return nil
				}
			}
			return denied()
		})
	}
	return denied()
}
func assigned(p core.Principal, owner, assistant core.ID) error {
	if owner == p.OwnerID && (p.Kind == core.ActorOwner || assistant == p.SubjectID) {
		return nil
	}
	return denied()
}
