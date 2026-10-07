package api

import (
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/plugins"
	"net/http"
)

func (s *HTTPServer) pluginRoutes() {
	if s.options.Plugins == nil {
		return
	}
	s.handle("GET /api/plugins", func(r *http.Request) (any, error) { return s.options.Plugins.List(r.Context(), principal(r)) })
	s.handle("POST /api/plugins/preference", func(r *http.Request) (any, error) {
		q, err := decode[plugins.PreferenceRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.SavePreference(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/install", func(r *http.Request) (any, error) {
		q, err := decode[plugins.Package](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.Install(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/configure", func(r *http.Request) (any, error) {
		q, err := decode[plugins.Configure](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.Configure(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/remove", func(r *http.Request) (any, error) {
		q, err := decode[struct {
			ID              string
			ExpectedVersion core.Version
		}](r)
		if err != nil {
			return nil, err
		}
		return nil, s.options.Plugins.Remove(r.Context(), principal(r), q.ID, q.ExpectedVersion)
	})
	s.handle("POST /api/plugins/settings/read", func(r *http.Request) (any, error) {
		q, err := decode[plugins.SettingsRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.Settings(r.Context(), principal(r), q.ID, q.Digest, q.Scope)
	})
	s.handle("POST /api/plugins/settings/save", func(r *http.Request) (any, error) {
		q, err := decode[plugins.SettingsRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.SaveSettings(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/connections/list", func(r *http.Request) (any, error) {
		q, err := decode[plugins.ConnectionRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.Connections(r.Context(), principal(r), q.PluginID, q.Digest)
	})
	s.handle("POST /api/plugins/connections/save", func(r *http.Request) (any, error) {
		q, err := decode[struct {
			PluginID, Digest, Name, ProviderID, APIKey, Verification string
			ID                                                       core.ID
			Enabled                                                  bool
			Mutation                                                 core.Mutation
			CallLimit                                                int64
			Concurrent, TimeoutSeconds                               int
		}](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.SaveConnection(r.Context(), principal(r), plugins.SaveConnection{PluginID: q.PluginID, Digest: q.Digest, ID: q.ID, Name: q.Name, ProviderID: q.ProviderID, Enabled: q.Enabled, APIKey: core.NewSecret(q.APIKey), Verification: auth.Verification{Token: core.NewSecret(q.Verification)}, Mutation: q.Mutation, CallLimit: q.CallLimit, Concurrent: q.Concurrent, TimeoutSeconds: q.TimeoutSeconds})
	})
	s.handle("POST /api/plugins/connections/remove", func(r *http.Request) (any, error) {
		q, err := decode[struct {
			PluginID, Digest, Verification string
			ID                             core.ID
			Mutation                       core.Mutation
		}](r)
		if err != nil {
			return nil, err
		}
		return nil, s.options.Plugins.RemoveConnection(r.Context(), principal(r), plugins.ConnectionRequest{PluginID: q.PluginID, Digest: q.Digest, ID: q.ID, Mutation: q.Mutation, Verification: auth.Verification{Token: core.NewSecret(q.Verification)}})
	})
	s.handle("POST /api/plugins/connections/test", func(r *http.Request) (any, error) {
		q, err := decode[plugins.TestConnectionRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.TestConnection(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/connections/history", func(r *http.Request) (any, error) {
		q, err := decode[plugins.ConnectionRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.HTTPHistory(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/connections/acknowledge", func(r *http.Request) (any, error) {
		q, err := decode[plugins.AcknowledgeCall](r)
		if err != nil {
			return nil, err
		}
		return nil, s.options.Plugins.AcknowledgeHTTP(r.Context(), principal(r), q)
	})
	s.handle("POST /api/plugins/ui", func(r *http.Request) (any, error) {
		q, err := decode[plugins.UIRequest](r)
		if err != nil {
			return nil, err
		}
		return s.options.Plugins.UI(r.Context(), principal(r), q)
	})
}
