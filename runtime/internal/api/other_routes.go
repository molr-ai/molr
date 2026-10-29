package api

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/internal/webassets"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func timeQuery(r *http.Request, key string) (time.Time, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return time.Time{}, nil
	}
	v, e := time.Parse(time.RFC3339, raw)
	if e != nil {
		return time.Time{}, core.Invalid(key, "RFC3339 timestamp required")
	}
	return v, nil
}
func (s *HTTPServer) routes() {
	s.handle("GET /api/updates", func(r *http.Request) (any, error) {
		if s.options.Updates == nil {
			return nil, core.NewError(core.Unavailable, "updates unavailable")
		}
		return s.options.Updates.Snapshot(), nil
	})
	s.handle("POST /api/updates/check", func(r *http.Request) (any, error) {
		if s.options.Updates == nil {
			return nil, core.NewError(core.Unavailable, "updates unavailable")
		}
		q, err := decode[struct {
			Automatic bool `json:"automatic"`
		}](r)
		if err != nil {
			return nil, err
		}
		return s.options.Updates.Check(r.Context(), q.Automatic), nil
	})
	s.handle("GET /api/updates/installation", func(r *http.Request) (any, error) {
		if s.options.Accounts == nil {
			return nil, core.NewError(core.Unavailable, "accounts unavailable")
		}
		if err := s.options.Accounts.RequireAdministrator(r.Context(), principal(r)); err != nil {
			return nil, err
		}
		return s.options.UpgradeInfo, nil
	})

	s.handle("GET /api/permission-requests/{id}", func(r *http.Request) (any, error) {
		if s.options.PermissionRequests == nil {
			return nil, core.NewError(core.Unavailable, "permission requests unavailable")
		}
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.PermissionRequests.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/permission-requests", func(r *http.Request) (any, error) {
		if s.options.PermissionRequests == nil {
			return nil, core.NewError(core.Unavailable, "permission requests unavailable")
		}
		return s.options.PermissionRequests.List(r.Context(), principal(r), queryID(r, "assistant_id"))
	})
	route(s, "POST /api/permission-requests/resolve", func(ctx context.Context, p core.Principal, q policy.PermissionResolution) (any, error) {
		if s.options.PermissionRequests == nil {
			return nil, core.NewError(core.Unavailable, "permission requests unavailable")
		}
		return s.options.PermissionRequests.Resolve(ctx, p, q)
	})
	s.maintenanceRoutes()
	s.authRoutes()
	s.oauthRoutes()
	s.notificationRoutes()
	s.businessRoutes()
	s.otherRoutes()
	s.debugRoutes()
	s.sandboxRoutes()
	s.browserRoutes()
	s.workspaceRoutes()
	s.pluginRoutes()
	s.mcpRoutes()
}
func (s *HTTPServer) otherRoutes() {
	s.handle("GET /api/skills", func(r *http.Request) (any, error) {
		if s.options.SkillsCatalog == nil || s.options.Skill == nil {
			return nil, core.NewError(core.Unavailable, "skills unavailable")
		}
		if err := principal(r).RequireOwner(); err != nil {
			return nil, err
		}
		id, path := queryID(r, "assistant_id"), r.URL.Query().Get("path")
		if path != "" {
			return s.options.Skill(r.Context(), principal(r), id, path)
		}
		if id != "" {
			return nil, core.Invalid("assistant_id", "assistant_id requires a skill path")
		}
		return s.options.SkillsCatalog(r.Context(), principal(r))
	})
	s.handle("GET /api/setup", func(r *http.Request) (any, error) {
		if s.options.Setup == nil {
			return map[string]any{"Required": false}, nil
		}
		return s.options.Setup(r.Context(), principal(r), nil)
	})
	route(s, "POST /api/setup", func(ctx context.Context, p core.Principal, q json.RawMessage) (any, error) {
		if s.options.Setup == nil {
			return nil, core.NewError(core.Unavailable, "setup unavailable")
		}
		return s.options.Setup(ctx, p, q)
	})
	s.handle("GET /api/resources/discover-local", func(r *http.Request) (any, error) {
		if s.options.DiscoverProviders == nil {
			return nil, core.NewError(core.Unavailable, "discovery unavailable")
		}
		return s.options.DiscoverProviders(r.Context(), principal(r))
	})

	s.handle("GET /api/sandbox", func(r *http.Request) (any, error) {
		if s.options.SandboxInfo == nil {
			return map[string]any{"Enabled": false, "Networks": []any{}}, nil
		}
		return s.options.SandboxInfo(r.Context(), principal(r))
	})
	s.handle("GET /api/settings/currency", func(r *http.Request) (any, error) {
		settings, err := s.services.Settings.Get(r.Context(), principal(r))
		if err != nil {
			return nil, err
		}
		if s.services.Exchange == nil {
			return nil, core.NewError(core.Unavailable, "exchange rates unavailable")
		}
		return map[string]any{"Currency": settings.Currency, "Timezone": settings.Timezone, "Rates": s.services.Exchange.Snapshot()}, nil
	})
	s.handle("GET /api/status", func(r *http.Request) (any, error) {
		info := buildinfo.Current()
		info.EmbeddedWebUI = webassets.Embedded
		return map[string]any{"build": info, "workbench_available": s.config.Debug.Enabled, "name": "Molr Runtime", "status": "running", "public_url": s.config.HTTP.PublicURL}, nil
	})
	s.handle("GET /api/models", func(r *http.Request) (any, error) { return s.services.Models.List(r.Context(), principal(r)) })
	s.handle("POST /api/models/refresh-pricing", func(r *http.Request) (any, error) {
		if s.options.Accounts == nil || s.options.Pricing == nil {
			return nil, core.NewError(core.Unavailable, "pricing refresh unavailable")
		}
		if err := s.options.Accounts.RequireAdministrator(r.Context(), principal(r)); err != nil {
			return nil, err
		}
		if _, err := decode[struct{}](r); err != nil {
			return nil, err
		}
		return s.options.Pricing.Refresh(r.Context())
	})
	s.handle("GET /api/assistants/{id}/capabilities", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Agent.Capabilities(r.Context(), principal(r), id)
	})
	s.handle("GET /api/assistants/{id}/status", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Agent.Status(r.Context(), principal(r), id)
	})
	s.handle("POST /api/assistants/{id}/expedite", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		request, err := decode[agent.ExpediteRequest](r)
		if err != nil {
			return nil, err
		}
		return s.services.Agent.Expedite(r.Context(), principal(r), id, request)
	})
	s.handle("POST /api/assistants/{id}/compress", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		request, err := decode[agent.CompressionRequest](r)
		if err != nil {
			return nil, err
		}
		return s.services.Agent.Compress(r.Context(), principal(r), id, request)
	})
	s.handle("POST /api/assistants/{id}/stop", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return nil, s.services.Agent.Stop(r.Context(), principal(r), id)
	})
	s.handle("GET /api/events", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Realtime.List(r.Context(), principal(r), realtime.Query{After: r.URL.Query().Get("after"), Topics: r.URL.Query()["topic"], Page: q})
	})
	s.mux.HandleFunc("GET /api/events/stream", s.sse)
	s.handle("GET /api/audit", func(r *http.Request) (any, error) {
		page, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		from, e := timeQuery(r, "from")
		if e != nil {
			return nil, e
		}
		until, e := timeQuery(r, "until")
		if e != nil {
			return nil, e
		}
		return s.services.Audit.List(r.Context(), principal(r), audit.Query{Action: r.URL.Query().Get("action"), Outcome: audit.Outcome(r.URL.Query().Get("outcome")), From: from, Until: until, Page: page})
	})
	s.handle("GET /api/credentials", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Credentials.List(r.Context(), principal(r), q)
	})
	s.handle("GET /api/credentials/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Credentials.Get(r.Context(), principal(r), id)
	})
	route(s, "POST /api/credentials", func(ctx context.Context, p core.Principal, input struct {
		Mutation     core.Mutation `json:"mutation"`
		ID           core.ID       `json:"id"`
		Provider     string        `json:"provider"`
		Label        string        `json:"label"`
		Value        string        `json:"value"`
		Verification string        `json:"verification"`
	}) (any, error) {
		return s.services.Credentials.Put(ctx, p, credential.PutRequest{Mutation: input.Mutation, ID: input.ID, Provider: input.Provider, Label: input.Label, Value: core.NewSecret(input.Value), Verification: auth.Verification{Token: core.NewSecret(input.Verification)}})
	})
	route(s, "POST /api/credentials/delete", func(ctx context.Context, p core.Principal, input struct {
		Mutation     core.Mutation `json:"mutation"`
		ID           core.ID       `json:"id"`
		Verification string        `json:"verification"`
	}) (any, error) {
		return nil, s.services.Credentials.Delete(ctx, p, credential.DeleteRequest{Mutation: input.Mutation, ID: input.ID, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}})
	})
	s.handle("GET /api/grants", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		return s.services.Grants.List(r.Context(), principal(r), policy.Query{AssistantID: queryID(r, "assistant_id"), ResourceID: queryID(r, "resource_id"), Page: q})
	})
	route(s, "POST /api/grants", func(ctx context.Context, p core.Principal, q policy.GrantRequest) (any, error) {
		return s.services.Grants.Grant(ctx, p, q)
	})
	route(s, "POST /api/grants/update", func(ctx context.Context, p core.Principal, q policy.UpdateGrantRequest) (any, error) {
		return s.services.Grants.UpdateGrant(ctx, p, q)
	})
	route(s, "POST /api/grants/revoke", func(ctx context.Context, p core.Principal, q policy.RevokeRequest) (any, error) {
		return nil, s.services.Grants.Revoke(ctx, p, q)
	})
	s.handle("GET /api/usage", func(r *http.Request) (any, error) {
		from, e := timeQuery(r, "from")
		if e != nil {
			return nil, e
		}
		until, e := timeQuery(r, "until")
		if e != nil {
			return nil, e
		}
		return s.services.Budget.Usage(r.Context(), principal(r), policy.UsageQuery{CurrentPeriod: r.URL.Query().Get("current_period") == "true", AssistantID: queryID(r, "assistant_id"), TaskID: queryID(r, "task_id"), From: from, Until: until})
	})
	route(s, "POST /api/settings/export", func(ctx context.Context, p core.Principal, input struct {
		Mutation         core.Mutation `json:"mutation"`
		Verification     string        `json:"verification"`
		IncludeMessages  bool          `json:"include_messages"`
		IncludeArtifacts bool          `json:"include_artifacts"`
	}) (any, error) {
		return s.services.Settings.Export(ctx, p, settings.ExportRequest{Mutation: input.Mutation, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}, IncludeMessages: input.IncludeMessages, IncludeArtifacts: input.IncludeArtifacts})
	})
	route(s, "POST /api/settings/cleanup", func(ctx context.Context, p core.Principal, input struct {
		Mutation     core.Mutation `json:"mutation"`
		Verification string        `json:"verification"`
		ObjectIDs    []core.Object `json:"object_ids"`
		MemoryFiles  []memory.Ref  `json:"memory_files"`
	}) (any, error) {
		return nil, s.services.Settings.Cleanup(ctx, p, settings.CleanupRequest{Mutation: input.Mutation, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}, ObjectIDs: input.ObjectIDs, MemoryFiles: input.MemoryFiles})
	})
	s.handle("GET /api/search", func(r *http.Request) (any, error) {
		page, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		query := search.Query{Text: r.URL.Query().Get("q"), AssistantID: queryID(r, "assistant_id"), Page: page}
		for _, source := range r.URL.Query()["source"] {
			query.Sources = append(query.Sources, search.Source(source))
		}
		if raw := r.URL.Query().Get("include_archived"); raw != "" {
			query.IncludeArchived, e = strconv.ParseBool(raw)
			if e != nil {
				return nil, core.Invalid("include_archived", "boolean required")
			}
		}
		return s.services.Search.Search(r.Context(), principal(r), query)
	})
	route(s, "POST /api/search", func(ctx context.Context, p core.Principal, q search.Query) (any, error) {
		return s.services.Search.Search(ctx, p, q)
	})
	s.handle("POST /api/search/rebuild", func(r *http.Request) (any, error) {
		id, e := s.services.Index.Rebuild(r.Context(), principal(r))
		return map[string]any{"id": id}, e
	})
	s.handle("GET /api/search/rebuild/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Index.Status(r.Context(), principal(r), id)
	})
	s.handle("GET /api/jobs", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		query := scheduler.Query{AssistantID: queryID(r, "assistant_id"), Page: q}
		for _, state := range r.URL.Query()["state"] {
			query.States = append(query.States, scheduler.State(state))
		}
		return s.services.Jobs.Jobs(r.Context(), principal(r), query)
	})
	s.handle("POST /api/jobs/{id}/cancel", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return nil, s.services.Jobs.Cancel(r.Context(), principal(r), id)
	})
	s.handle("GET /api/operations", func(r *http.Request) (any, error) {
		q, e := queryPage(r)
		if e != nil {
			return nil, e
		}
		query := operation.Query{AssistantID: queryID(r, "assistant_id"), TaskID: queryID(r, "task_id"), Page: q}
		for _, state := range r.URL.Query()["state"] {
			query.States = append(query.States, operation.State(state))
		}
		return s.services.Operations.List(r.Context(), principal(r), query)
	})
	s.handle("GET /api/operations/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Operations.Get(r.Context(), principal(r), id)
	})
	s.handle("POST /api/operations/{id}/cancel", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Operations.Cancel(r.Context(), principal(r), id)
	})
	route(s, "POST /api/operations/reconcile", func(ctx context.Context, p core.Principal, q operation.ReconcileRequest) (any, error) {
		return s.services.Operations.Reconcile(ctx, p, q)
	})
	s.handle("GET /api/files/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return s.services.Files.Get(r.Context(), principal(r), id)
	})
	s.handle("DELETE /api/files/{id}", func(r *http.Request) (any, error) {
		id, e := pathID(r)
		if e != nil {
			return nil, e
		}
		return nil, s.services.Files.Delete(r.Context(), principal(r), id)
	})
	s.handle("POST /api/files", func(r *http.Request) (any, error) {
		if r.ContentLength < 0 || r.ContentLength > s.config.Execution.MaxUploadBytes {
			return nil, core.Invalid("content_length", "bounded content length required")
		}
		name := r.Header.Get("X-File-Name")
		if encoding := r.Header.Get("X-File-Name-Encoding"); encoding != "" {
			if encoding != "percent" {
				return nil, core.Invalid("file_name_encoding", "unsupported encoding")
			}
			var err error
			name, err = url.PathUnescape(name)
			if err != nil {
				return nil, core.Invalid("file_name", "invalid encoded name")
			}
		}
		return s.services.Files.Upload(r.Context(), principal(r), files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: r.Header.Get("Idempotency-Key")}, Name: name, MediaType: r.Header.Get("Content-Type"), Size: r.ContentLength, TaskID: core.ID(r.Header.Get("X-Task-ID")), Body: r.Body})
	})
	s.mux.HandleFunc("GET /api/files/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		if e != nil {
			fail(w, e, info(r).ID)
			return
		}
		download, e := s.services.Files.Open(r.Context(), principal(r), id)
		if e != nil {
			fail(w, e, info(r).ID)
			return
		}
		defer download.Body.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.File.Name}))
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Content-Length", strconv.FormatInt(download.File.Size, 10))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(w, download.Body)
	})
}
