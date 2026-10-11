package api

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"net/http"
	"time"
)

func (s *HTTPServer) loginLinkRoutes() {
	route(s, "POST /api/auth/login-links", func(ctx context.Context, p core.Principal, input struct {
		Name         string `json:"name"`
		TargetPath   string `json:"target_path"`
		ExpiresIn    int64  `json:"expires_in"`
		Verification string `json:"verification"`
	}) (any, error) {
		if input.ExpiresIn < 60 || input.ExpiresIn > 86400 {
			return nil, core.Invalid("expires_in", "between 60 and 86400 seconds required")
		}
		issued, err := s.services.Auth.CreateLoginLink(ctx, p, auth.CreateLoginLinkRequest{Name: input.Name, TargetPath: input.TargetPath, ExpiresIn: time.Duration(input.ExpiresIn) * time.Second, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"link": issued.Link, "value": issued.Value.Reveal()}, nil
	})
	s.handle("GET /api/auth/login-links", func(r *http.Request) (any, error) {
		q, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Auth.LoginLinks(r.Context(), principal(r), q)
	})
	s.handle("DELETE /api/auth/login-links/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return nil, s.services.Auth.RevokeLoginLink(r.Context(), principal(r), id)
	})
	s.handle("POST /api/auth/login-links/preview", func(r *http.Request) (any, error) {
		input, err := decode[struct {
			Token string `json:"token"`
		}](r)
		if err != nil {
			return nil, err
		}
		return s.services.Auth.PreviewLoginLink(r.Context(), auth.PreviewLoginLinkRequest{Token: core.NewSecret(input.Token), Client: auth.Client{Address: info(r).ClientAddress}})
	})
	s.mux.HandleFunc("POST /api/auth/login-links/redeem", func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[struct {
			Token      string `json:"token"`
			DeviceName string `json:"device_name"`
			DeviceID   string `json:"device_id"`
			ClientType string `json:"client_type"`
		}](r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		if len(input.DeviceName) > 128 {
			fail(w, core.Invalid("device_name", "too long"), info(r).ID)
			return
		}
		result, err := s.services.Auth.RedeemLoginLink(r.Context(), auth.RedeemLoginLinkRequest{Token: core.NewSecret(input.Token), Client: auth.Client{Address: info(r).ClientAddress, UserAgent: r.UserAgent(), DeviceName: input.DeviceName, DeviceID: input.DeviceID, ClientType: input.ClientType}})
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		if result.Session != nil {
			s.session(w, *result.Session, result.TargetPath)
			return
		}
		if result.Challenge == nil {
			fail(w, core.NewError(core.Internal, "invalid login result"), info(r).ID)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"challenge": result.Challenge.ID.Reveal(), "expires_at": result.Challenge.ExpiresAt, "target_path": result.TargetPath})
	})
}
