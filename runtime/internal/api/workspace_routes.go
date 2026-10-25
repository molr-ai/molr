package api

import (
	"mime"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/workspace"
	"net/http"
	"strconv"
)

func (s *HTTPServer) workspaceRoutes() {
	s.handle("GET /api/assistants/{id}/workspace", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.Workspace == nil {
			return nil, core.NewError(core.Unavailable, "workspace unavailable")
		}
		q := workspace.Request{Action: r.URL.Query().Get("action"), Path: r.URL.Query().Get("path"), ExpectedVersion: r.URL.Query().Get("version")}
		if q.Action == "" {
			q.Action = "list"
		}
		if q.Action != "list" && q.Action != "stat" && q.Action != "read" {
			return nil, core.Invalid("action", "read action required")
		}
		for key, target := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
			if raw := r.URL.Query().Get(key); raw != "" {
				v, e := strconv.Atoi(raw)
				if e != nil {
					return nil, core.Invalid(key, "integer required")
				}
				*target = v
			}
		}
		return s.options.Workspace.Query(r.Context(), principal(r), id, q)
	})
	s.handle("POST /api/assistants/{id}/workspace", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.Workspace == nil {
			return nil, core.NewError(core.Unavailable, "workspace unavailable")
		}
		q, err := decode[workspace.Request](r)
		if err != nil {
			return nil, err
		}
		return s.options.Workspace.Query(r.Context(), principal(r), id, q)
	})
	s.handle("PUT /api/assistants/{id}/workspace/content", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.Workspace == nil {
			return nil, core.NewError(core.Unavailable, "workspace unavailable")
		}
		return s.options.Workspace.Upload(r.Context(), principal(r), id, workspace.Request{Path: r.URL.Query().Get("path"), ExpectedVersion: r.URL.Query().Get("version")}, r.Body)
	})
	s.mux.HandleFunc("GET /api/assistants/{id}/workspace/content", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		if s.options.Workspace == nil {
			fail(w, core.NewError(core.Unavailable, "workspace unavailable"), info(r).ID)
			return
		}
		f, err := s.options.Workspace.Download(r.Context(), principal(r), id, workspace.Request{Path: r.URL.Query().Get("path"), ExpectedVersion: r.URL.Query().Get("version")})
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Entry.Name}))
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		http.ServeContent(w, r, f.Entry.Name, f.Entry.ModifiedAt, f)
	})
}
