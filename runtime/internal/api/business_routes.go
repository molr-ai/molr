package api

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/trigger"
	"net/http"
	"strconv"
	"time"
)

// Routes are registered explicitly against the restricted Services ports. No
// reflection or arbitrary method name can invoke internal execution methods.
func (s *HTTPServer) businessRoutes() {
	s.weixinRoutes()
	s.telegramRoutes()
	s.slackRoutes()
	s.dingtalkRoutes()
	s.handle("GET /api/assistants/activity", s.assistantActivity)
	s.handle("GET /api/plans/events", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		from, err := time.Parse(time.RFC3339Nano, q.Get("from"))
		if err != nil {
			return nil, core.Invalid("from", "invalid date")
		}
		limit := 50
		if q.Get("limit") != "" {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || limit < 1 || limit > 200 {
				return nil, core.Invalid("limit", "must be 1 to 200")
			}
		}
		return s.services.Plans.Events(r.Context(), principal(r), plan.EventQuery{From: from, Scope: q.Get("scope"), Kind: q.Get("kind"), State: q.Get("state"), Page: core.PageRequest{Limit: limit, Cursor: q.Get("cursor")}})
	})
	s.handle("GET /api/plans", func(r *http.Request) (any, error) {
		var from, to time.Time
		if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
			var err error
			from, err = time.Parse(time.RFC3339, r.URL.Query().Get("from"))
			if err != nil {
				return nil, core.Invalid("from", "invalid date")
			}
			to, err = time.Parse(time.RFC3339, r.URL.Query().Get("to"))
			if err != nil {
				return nil, core.Invalid("to", "invalid date")
			}
		}
		return s.services.Plans.List(r.Context(), principal(r), from, to)
	})
	route(s, "POST /api/plans/configure", func(ctx context.Context, p core.Principal, r plan.ConfigureRequest) (any, error) {
		return s.services.Plans.Configure(ctx, p, r)
	})
	route(s, "POST /api/plans/execute", func(ctx context.Context, p core.Principal, r plan.ExecuteRequest) (any, error) {
		return s.services.Plans.Execute(ctx, p, r)
	})
	route(s, "POST /api/assistants", func(ctx context.Context, p core.Principal, r assistant.HireRequest) (any, error) {
		return s.services.Assistants.Hire(ctx, p, r)
	})
	route(s, "POST /api/assistants/update", func(ctx context.Context, p core.Principal, r assistant.UpdateRequest) (any, error) {
		return s.services.Assistants.Update(ctx, p, r)
	})
	route(s, "POST /api/assistants/place", func(ctx context.Context, p core.Principal, r assistant.PlacementRequest) (any, error) {
		return s.services.Assistants.Place(ctx, p, r)
	})
	route(s, "POST /api/assistants/dismiss", func(ctx context.Context, p core.Principal, r assistant.LifecycleRequest) (any, error) {
		return s.services.Assistants.Dismiss(ctx, p, r)
	})
	route(s, "POST /api/assistants/rehire", func(ctx context.Context, p core.Principal, r assistant.RehireRequest) (any, error) {
		return s.services.Assistants.Rehire(ctx, p, r)
	})
	route(s, "POST /api/assistants/clone", func(ctx context.Context, p core.Principal, r assistant.CloneRequest) (any, error) {
		return s.services.Assistants.Clone(ctx, p, r)
	})
	route(s, "POST /api/assistants/groups", func(ctx context.Context, p core.Principal, r assistant.GroupRequest) (any, error) {
		return s.services.Assistants.CreateGroup(ctx, p, r)
	})
	route(s, "POST /api/assistants/groups/update", func(ctx context.Context, p core.Principal, r assistant.GroupRequest) (any, error) {
		return s.services.Assistants.UpdateGroup(ctx, p, r)
	})
	route(s, "POST /api/assistants/groups/delete", func(ctx context.Context, p core.Principal, r assistant.LifecycleRequest) (any, error) {
		return nil, s.services.Assistants.DeleteGroup(ctx, p, r)
	})
	route(s, "POST /api/tasks", func(ctx context.Context, p core.Principal, r task.CreateRequest) (any, error) {
		return s.services.Tasks.Create(ctx, p, r)
	})
	route(s, "POST /api/tasks/revise", func(ctx context.Context, p core.Principal, r task.ReviseRequest) (any, error) {
		return s.services.Tasks.Revise(ctx, p, r)
	})
	route(s, "POST /api/tasks/pause", func(ctx context.Context, p core.Principal, r task.ControlRequest) (any, error) {
		return s.services.Tasks.Pause(ctx, p, r)
	})
	route(s, "POST /api/tasks/resume", func(ctx context.Context, p core.Principal, r task.ControlRequest) (any, error) {
		return s.services.Tasks.Resume(ctx, p, r)
	})
	route(s, "POST /api/tasks/cancel", func(ctx context.Context, p core.Principal, r task.ControlRequest) (any, error) {
		return s.services.Tasks.Cancel(ctx, p, r)
	})
	route(s, "POST /api/todos", func(ctx context.Context, p core.Principal, r todo.CreateRequest) (any, error) {
		return s.services.Todos.Create(ctx, p, r)
	})
	route(s, "POST /api/todos/update", func(ctx context.Context, p core.Principal, r todo.UpdateRequest) (any, error) {
		return s.services.Todos.Update(ctx, p, r)
	})
	route(s, "POST /api/todos/delete", func(ctx context.Context, p core.Principal, r todo.DeleteRequest) (any, error) {
		return nil, s.services.Todos.Delete(ctx, p, r)
	})
	route(s, "POST /api/schedules", func(ctx context.Context, p core.Principal, r schedule.CreateRequest) (any, error) {
		return s.services.Schedules.Create(ctx, p, r)
	})
	route(s, "POST /api/schedules/update", func(ctx context.Context, p core.Principal, r schedule.UpdateRequest) (any, error) {
		return s.services.Schedules.Update(ctx, p, r)
	})
	route(s, "POST /api/schedules/delete", func(ctx context.Context, p core.Principal, r schedule.DeleteRequest) (any, error) {
		return nil, s.services.Schedules.Delete(ctx, p, r)
	})
	route(s, "POST /api/resources", func(ctx context.Context, p core.Principal, r resource.ConnectRequest) (any, error) {
		return s.services.Resources.Connect(ctx, p, r)
	})
	route(s, "POST /api/resources/provider", func(ctx context.Context, p core.Principal, r struct {
		Mutation        core.Mutation
		ID              core.ID
		Name            string
		Endpoint        string
		UseAPIKey       bool
		PricingProvider string
		APIKey          string
		Verification    string `json:"verification"`
	}) (any, error) {
		return s.services.Resources.SaveProvider(ctx, p, resource.SaveProviderRequest{PricingProvider: r.PricingProvider, Mutation: r.Mutation, ID: r.ID, Name: r.Name, Endpoint: r.Endpoint, UseAPIKey: r.UseAPIKey, APIKey: core.NewSecret(r.APIKey), Verification: auth.Verification{Token: core.NewSecret(r.Verification)}})
	})
	s.handle("GET /api/resources/{id}/connection", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Resources.ProviderConnection(r.Context(), principal(r), id)
	})
	route(s, "POST /api/resources/test-connection", func(ctx context.Context, p core.Principal, r struct {
		Provider string
		Binding  resource.Binding
		APIKey   string
	}) (any, error) {
		return s.services.Resources.TestConnection(ctx, p, resource.TestConnectionRequest{Provider: r.Provider, Binding: r.Binding, APIKey: core.NewSecret(r.APIKey)})
	})
	route(s, "POST /api/resources/update", func(ctx context.Context, p core.Principal, r resource.UpdateRequest) (any, error) {
		return s.services.Resources.Update(ctx, p, r)
	})
	route(s, "POST /api/resources/refresh-models", func(ctx context.Context, p core.Principal, r resource.RefreshModelsRequest) (any, error) {
		return s.services.Resources.RefreshModels(ctx, p, r)
	})
	route(s, "POST /api/resources/configure-model", func(ctx context.Context, p core.Principal, r resource.ConfigureModelRequest) (any, error) {
		return s.services.Resources.ConfigureModel(ctx, p, r)
	})
	route(s, "POST /api/resources/disconnect", func(ctx context.Context, p core.Principal, r resource.DisconnectRequest) (any, error) {
		return nil, s.services.Resources.Disconnect(ctx, p, r)
	})
	route(s, "POST /api/resources/delete-provider", func(ctx context.Context, p core.Principal, r struct {
		Mutation     core.Mutation
		ID           core.ID
		Verification string `json:"verification"`
	}) (any, error) {
		return nil, s.services.Resources.DeleteProvider(ctx, p, resource.DeleteProviderRequest{Mutation: r.Mutation, ID: r.ID, Verification: auth.Verification{Token: core.NewSecret(r.Verification)}})
	})
	s.handle("GET /api/dreams/runs/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Dreams.GetRun(r.Context(), principal(r), id)
	})
	s.handle("GET /api/dreams/conflicts", func(r *http.Request) (any, error) {
		start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start_at"))
		if err != nil {
			return nil, core.Invalid("start_at", "invalid time")
		}
		rule := trigger.Rule{StartAt: start, Timezone: r.URL.Query().Get("timezone"), Recurrence: r.URL.Query().Get("recurrence"), MissedPolicy: trigger.Coalesce}
		from := start
		// Editing an old series must check its upcoming occurrences, not its
		// first 90 days. Keep DTSTART unchanged so COUNT and UNTIL still apply.
		if now := time.Now(); rule.Recurrence != "" && from.Before(now) {
			from = now
		}
		to := from.Add(90 * 24 * time.Hour)
		return s.services.Dreams.Conflicts(r.Context(), principal(r), queryID(r, "assistant_id"), rule, from, to)
	})
	s.handle("GET /api/dreams/windows", func(r *http.Request) (any, error) {
		from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		if err != nil {
			return nil, core.Invalid("from", "invalid time")
		}
		to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if err != nil {
			return nil, core.Invalid("to", "invalid time")
		}
		return s.services.Dreams.Windows(r.Context(), principal(r), queryID(r, "assistant_id"), from, to)
	})
	route(s, "POST /api/dreams/config", func(ctx context.Context, p core.Principal, r dream.ConfigureRequest) (any, error) {
		return s.services.Dreams.Configure(ctx, p, r)
	})
	route(s, "POST /api/dreams/undo", func(ctx context.Context, p core.Principal, r dream.UndoRequest) (any, error) {
		return s.services.Dreams.Undo(ctx, p, r)
	})
	route(s, "POST /api/dreams/run", func(ctx context.Context, p core.Principal, r dream.Request) (any, error) {
		return s.services.Dreams.Request(ctx, p, r)
	})
	s.handle("GET /api/dreams/config", func(r *http.Request) (any, error) {
		return s.services.Dreams.Get(r.Context(), principal(r), queryID(r, "assistant_id"))
	})
	s.handle("GET /api/dreams", func(r *http.Request) (any, error) {
		return s.services.Dreams.Runs(r.Context(), principal(r), queryID(r, "assistant_id"))
	})
	route(s, "POST /api/memories", func(ctx context.Context, p core.Principal, r memory.CreateRequest) (any, error) {
		return s.services.Memories.Create(ctx, p, r)
	})
	route(s, "POST /api/memories/update", func(ctx context.Context, p core.Principal, r memory.UpdateRequest) (any, error) {
		return s.services.Memories.Update(ctx, p, r)
	})
	route(s, "POST /api/memories/delete", func(ctx context.Context, p core.Principal, r memory.DeleteRequest) (any, error) {
		return nil, s.services.Memories.Delete(ctx, p, r)
	})
	route(s, "POST /api/conversations/messages", func(ctx context.Context, p core.Principal, r conversation.SendUserRequest) (any, error) {
		return s.services.Conversations.SendUser(ctx, p, r)
	})
	route(s, "POST /api/conversations/read", func(ctx context.Context, p core.Principal, r conversation.ReadRequest) (any, error) {
		return nil, s.services.Conversations.MarkRead(ctx, p, r)
	})
	route(s, "POST /api/conversations/feedback", func(ctx context.Context, p core.Principal, r conversation.FeedbackRequest) (any, error) {
		return nil, s.services.Conversations.SetFeedback(ctx, p, r)
	})
	route(s, "POST /api/settings", func(ctx context.Context, p core.Principal, r settings.UpdateRequest) (any, error) {
		return s.services.Settings.Update(ctx, p, r)
	})
	s.handle("GET /api/assistants/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Assistants.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/tasks/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Tasks.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/todos/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Todos.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/schedules/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Schedules.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/resources/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Resources.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/memories/file", func(r *http.Request) (any, error) {
		return s.services.Memories.Get(r.Context(), principal(r), memoryRef(r))
	})
	s.handle("GET /api/memories/directory", func(r *http.Request) (any, error) {
		return s.services.Memories.Browse(r.Context(), principal(r), memoryRef(r))
	})
	route(s, "POST /api/memories/mkdir", func(ctx context.Context, p core.Principal, req memory.DirectoryRequest) (any, error) {
		return nil, s.services.Memories.Mkdir(ctx, p, req)
	})
	route(s, "POST /api/memories/move", func(ctx context.Context, p core.Principal, req memory.MoveRequest) (any, error) {
		return nil, s.services.Memories.Move(ctx, p, req)
	})
	route(s, "POST /api/memories/rmdir", func(ctx context.Context, p core.Principal, req memory.DirectoryRequest) (any, error) {
		return nil, s.services.Memories.RemoveDirectory(ctx, p, req)
	})

	s.handle("GET /api/conversations/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Conversations.Get(r.Context(), principal(r), id)
	})

	s.handle("GET /api/assistants", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Assistants.List(r.Context(), principal(r), assistant.Query{State: assistant.State(r.URL.Query().Get("state")), GroupID: queryID(r, "group_id"), Name: r.URL.Query().Get("name"), Page: q})
	})
	s.handle("GET /api/assistants/groups", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Assistants.Groups(r.Context(), principal(r), q)
	})
	s.handle("GET /api/tasks", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		query := task.Query{AssistantID: queryID(r, "assistant_id"), Page: q}
		for _, state := range r.URL.Query()["state"] {
			query.States = append(query.States, task.State(state))
		}
		return s.services.Tasks.List(r.Context(), principal(r), query)
	})
	s.handle("GET /api/tasks/{id}/revisions", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Tasks.Revisions(r.Context(), principal(r), id, q)
	})
	s.handle("GET /api/todos", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		query := todo.Query{AssistantID: queryID(r, "assistant_id"), Page: q}
		if raw := r.URL.Query().Get("completed"); raw != "" {
			completed, e := strconv.ParseBool(raw)
			if e != nil {
				return nil, core.Invalid("completed", "boolean required")
			}
			query.Completed = &completed
		}
		return s.services.Todos.List(r.Context(), principal(r), query)
	})
	s.handle("GET /api/schedules", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		result, err := s.services.Schedules.List(r.Context(), principal(r), schedule.Query{AssistantID: queryID(r, "assistant_id"), Kind: schedule.Kind(r.URL.Query().Get("kind")), Page: q})
		if err != nil {
			return nil, err
		}
		if r.URL.Query().Get("from") == "" && r.URL.Query().Get("to") == "" {
			return result, nil
		}
		from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		if err != nil {
			return nil, core.Invalid("from", "invalid date")
		}
		to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if err != nil || !to.After(from) || to.Sub(from) > 62*24*time.Hour {
			return nil, core.Invalid("to", "range must be within 62 days")
		}
		type calendarSchedule struct {
			schedule.Schedule
			Occurrences []time.Time
		}
		calendar := core.Page[calendarSchedule]{NextCursor: result.NextCursor, Items: []calendarSchedule{}}
		for _, plan := range result.Items {
			times, err := schedule.CalendarTimes(r.Context(), plan, from, to)
			if err != nil {
				return nil, err
			}
			calendar.Items = append(calendar.Items, calendarSchedule{Schedule: plan, Occurrences: times})
		}
		return calendar, nil
	})
	s.handle("GET /api/resources", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Resources.List(r.Context(), principal(r), resource.Query{Kind: resource.Kind(r.URL.Query().Get("kind")), Name: r.URL.Query().Get("name"), Page: q})
	})
	s.handle("POST /api/resources/{id}/check", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Resources.Check(r.Context(), principal(r), id)
	})
	s.handle("GET /api/memories", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Memories.List(r.Context(), principal(r), memory.Query{State: memory.State(r.URL.Query().Get("state")), Scope: memory.Scope(r.URL.Query().Get("scope")), AssistantID: queryID(r, "assistant_id"), Text: r.URL.Query().Get("q"), Page: q})
	})
	s.handle("GET /api/conversations", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Conversations.List(r.Context(), principal(r), q)
	})
	s.handle("GET /api/conversations/{id}/messages/{message}/metrics", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		message := core.ID(r.PathValue("message"))
		if err = message.Validate(); err != nil {
			return nil, err
		}
		return s.services.Agent.MessageMetrics(r.Context(), principal(r), id, message)
	})
	s.handle("GET /api/conversations/{id}/messages", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		order := r.URL.Query().Get("order")
		if order != "" && order != "asc" && order != "desc" {
			return nil, core.Invalid("order", "asc or desc required")
		}
		var after uint64
		if value, present := r.URL.Query()["after_sequence"]; present {
			if len(value) != 1 || value[0] == "" {
				return nil, core.Invalid("after_sequence", "unsigned message sequence required")
			}
			after, e = strconv.ParseUint(value[0], 10, 64)
			if e != nil {
				return nil, core.Invalid("after_sequence", "unsigned message sequence required")
			}
		}
		messageID := queryID(r, "message_id")
		if messageID != "" {
			if e := messageID.Validate(); e != nil {
				return nil, e
			}
		}
		return s.services.Conversations.Messages(r.Context(), principal(r), conversation.MessageQuery{ConversationID: id, MessageID: messageID, AfterSequence: after, Before: r.URL.Query().Get("before"), Descending: order == "desc", Page: q})
	})
	s.handle("GET /api/settings", func(r *http.Request) (any, error) { return s.services.Settings.Get(r.Context(), principal(r)) })
	s.handle("GET /api/settings/exports/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Settings.ExportStatus(r.Context(), principal(r), id)
	})
}

func memoryRef(r *http.Request) memory.Ref {
	return memory.Ref{Scope: memory.Scope(r.URL.Query().Get("scope")), AssistantID: queryID(r, "assistant_id"), Path: r.URL.Query().Get("path")}
}
