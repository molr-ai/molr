package api

import (
	"context"
	"molr.ai/runtime/internal/auth"

	"molr.ai/runtime/internal/core"
	"net/http"
	"time"
)

func (s *HTTPServer) session(w http.ResponseWriter, issued auth.SessionIssued, target ...string) {
	var expires time.Time
	if issued.RememberMe {
		expires = issued.Session.ExpiresAt
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(sessionCookie), Value: issued.Token.Reveal(), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires})
	// This is a separate proof, never the session credential. Browser clients
	// can restore it after a reload or in another tab; CheckCSRF still compares
	// its header value against the authenticated session's persisted digest.
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(csrfCookie), Value: issued.CSRF.Reveal(), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), SameSite: http.SameSiteStrictMode, Expires: expires})
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(loginCookie), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	response := map[string]any{"session": issued.Session, "csrf_token": issued.CSRF.Reveal()}
	if len(target) == 1 {
		response["target_path"] = target[0]
	}
	sendJSON(w, http.StatusOK, response)
}

type verificationDTO struct {
	Verification string `json:"verification"`
}
type tokenRequestDTO struct {
	Name         string    `json:"name"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	Verification string    `json:"verification"`
}
type reauthenticationDTO struct {
	Password string `json:"password"`
	MFACode  string `json:"mfa_code"`
	Purpose  string `json:"purpose"`
}

func (s *HTTPServer) authRoutes() {
	s.accountRoutes()
	s.loginLinkRoutes()
	s.mux.HandleFunc("GET /api/auth/csrf", func(w http.ResponseWriter, r *http.Request) {
		challenge, token, err := s.options.PreLoginCSRF(r.Context())
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(loginCookie), Value: challenge.ID.Reveal(), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: challenge.ExpiresAt})
		sendJSON(w, http.StatusOK, map[string]any{"csrf_token": token.Reveal(), "expires_at": challenge.ExpiresAt})
	})
	s.mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[struct {
			Username   string `json:"username"`
			RememberMe bool   `json:"remember_me"`
			Password   string `json:"password"`
			DeviceName string `json:"device_name"`
			DeviceID   string `json:"device_id"`
			ClientType string `json:"client_type"`
		}](r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		result, err := s.services.Auth.Login(r.Context(), auth.LoginRequest{RememberMe: input.RememberMe, Username: input.Username, Password: core.NewSecret(input.Password), Client: auth.Client{Address: info(r).ClientAddress, UserAgent: r.UserAgent(), DeviceName: input.DeviceName, DeviceID: input.DeviceID, ClientType: input.ClientType}})
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		if result.Session != nil && result.Challenge == nil {
			s.session(w, *result.Session)
			return
		}
		if result.Challenge == nil || result.Session != nil {
			fail(w, core.NewError(core.Internal, "invalid login result"), info(r).ID)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"challenge": result.Challenge.ID.Reveal(), "expires_at": result.Challenge.ExpiresAt})
	})
	s.mux.HandleFunc("POST /api/auth/mfa/complete", func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[struct {
			Challenge  string `json:"challenge"`
			Code       string `json:"code"`
			DeviceName string `json:"device_name"`
			DeviceID   string `json:"device_id"`
			ClientType string `json:"client_type"`
		}](r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		issued, err := s.services.Auth.CompleteMFA(r.Context(), auth.MFAChallengeResponse{Challenge: core.NewSecret(input.Challenge), Code: core.NewSecret(input.Code), Client: auth.Client{Address: info(r).ClientAddress, UserAgent: r.UserAgent(), DeviceName: input.DeviceName, DeviceID: input.DeviceID, ClientType: input.ClientType}})
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		s.session(w, issued)
	})
	s.handle("GET /api/auth/me", func(r *http.Request) (any, error) {
		p := principal(r)
		if s.options.Accounts == nil {
			return p, nil
		}
		u, err := s.options.Accounts.CurrentUser(r.Context(), p)
		return struct {
			core.Principal
			User auth.UserInfo
		}{p, u}, err
	})
	s.handle("GET /api/auth/security", func(r *http.Request) (any, error) {
		return s.services.Auth.SecurityStatus(r.Context(), principal(r))
	})
	route(s, "POST /api/auth/security", func(ctx context.Context, p core.Principal, input struct {
		Enabled         bool         `json:"enabled"`
		ExpectedVersion core.Version `json:"expected_version"`
		Verification    string       `json:"verification"`
	}) (any, error) {
		return s.services.Auth.SetReauthentication(ctx, p, auth.ReauthenticationSettings{Enabled: input.Enabled, ExpectedVersion: input.ExpectedVersion, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}})
	})
	s.mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := s.services.Auth.Logout(r.Context(), principal(r)); err != nil {
			fail(w, err, info(r).ID)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(sessionCookie), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(csrfCookie), Path: "/", Secure: !s.config.HTTP.PlainHTTP(), SameSite: http.SameSiteStrictMode, MaxAge: -1})
		sendJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	s.handle("GET /api/auth/sessions", func(r *http.Request) (any, error) {
		q, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Auth.Sessions(r.Context(), principal(r), q)
	})
	s.handle("POST /api/auth/sessions/revoke-others", func(r *http.Request) (any, error) {
		return nil, s.services.Auth.RevokeOtherSessions(r.Context(), principal(r))
	})
	s.handle("DELETE /api/auth/sessions/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return nil, s.services.Auth.RevokeSession(r.Context(), principal(r), id)
	})
	route(s, "POST /api/auth/reauthenticate", func(ctx context.Context, p core.Principal, input reauthenticationDTO) (any, error) {
		v, err := s.services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Password: core.NewSecret(input.Password), MFACode: core.NewSecret(input.MFACode), Purpose: input.Purpose})
		if err != nil {
			return nil, err
		}
		return map[string]string{"verification": v.Token.Reveal()}, nil
	})
	route(s, "POST /api/auth/password", func(ctx context.Context, p core.Principal, input struct {
		Verification string `json:"verification"`
		NewPassword  string `json:"new_password"`
	}) (any, error) {
		return nil, s.services.Auth.ChangePassword(ctx, p, auth.ChangePasswordRequest{Verification: auth.Verification{Token: core.NewSecret(input.Verification)}, NewPassword: core.NewSecret(input.NewPassword)})
	})
	route(s, "POST /api/auth/mfa/enroll", func(ctx context.Context, p core.Principal, input verificationDTO) (any, error) {
		enrollment, err := s.services.Auth.BeginMFAEnrollment(ctx, p, auth.Verification{Token: core.NewSecret(input.Verification)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": enrollment.ID, "provisioning_uri": enrollment.ProvisioningURI.Reveal(), "expires_at": enrollment.ExpiresAt}, nil
	})
	route(s, "POST /api/auth/mfa/confirm", func(ctx context.Context, p core.Principal, input struct {
		EnrollmentID core.ID `json:"enrollment_id"`
		Code         string  `json:"code"`
	}) (any, error) {
		codes, err := s.services.Auth.ConfirmMFAEnrollment(ctx, p, auth.ConfirmMFARequest{EnrollmentID: input.EnrollmentID, Code: core.NewSecret(input.Code)})
		if err != nil {
			return nil, err
		}
		plain := make([]string, len(codes.Codes))
		for i, c := range codes.Codes {
			plain[i] = c.Reveal()
		}
		return map[string]any{"recovery_codes": plain}, nil
	})
	route(s, "POST /api/auth/mfa/disable", func(ctx context.Context, p core.Principal, input verificationDTO) (any, error) {
		return nil, s.services.Auth.DisableMFA(ctx, p, auth.Verification{Token: core.NewSecret(input.Verification)})
	})
	s.handle("GET /api/auth/tokens", func(r *http.Request) (any, error) {
		q, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Auth.Tokens(r.Context(), principal(r), q)
	})
	route(s, "POST /api/auth/tokens", func(ctx context.Context, p core.Principal, input tokenRequestDTO) (any, error) {
		token, err := s.services.Auth.CreateToken(ctx, p, auth.CreateTokenRequest{Name: input.Name, Scopes: input.Scopes, ExpiresAt: input.ExpiresAt, Verification: auth.Verification{Token: core.NewSecret(input.Verification)}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": token.Token, "value": token.Value.Reveal()}, nil
	})
	s.handle("DELETE /api/auth/tokens/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return nil, s.services.Auth.RevokeToken(r.Context(), principal(r), id)
	})
}
