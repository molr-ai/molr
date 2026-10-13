package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"molr.ai/runtime/internal/installation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/updates"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/webassets"
)

const sessionCookie = "__Host-molr_session"
const loginCookie = "__Host-molr_login"
const csrfCookie = "__Host-molr_csrf"

type permissionRequestService interface {
	Get(context.Context, core.Principal, core.ID) (policy.PermissionRequest, error)
	List(context.Context, core.Principal, core.ID) ([]policy.PermissionRequest, error)
	Resolve(context.Context, core.Principal, policy.PermissionResolution) (policy.PermissionRequest, error)
}

type Options struct {
	UsageAnalytics     func(context.Context, core.Principal, UsageQuery, bool) (any, error)
	Maintenance        *installation.Maintenance
	Browser            func(context.Context, core.Principal, core.ID, json.RawMessage) (json.RawMessage, error)
	BrowserSettings    func(context.Context, core.Principal, json.RawMessage) (any, error)
	BrowserStream      func(context.Context, core.Principal, core.ID, core.ID, func(json.RawMessage) error) error
	Updates            *updates.Service
	UpgradeInfo        *updates.Installation
	Notifications      *notification.Service
	Weixin             *weixin.Service
	Setup              func(context.Context, core.Principal, json.RawMessage) (any, error)
	DiscoverProviders  func(context.Context, core.Principal) (any, error)
	Accounts           *auth.Manager
	AdminCatalog       func(context.Context, core.Principal) (any, error)
	AdminUsage         func(context.Context, core.Principal) (any, error)
	AccountQuotas      func(context.Context, core.Principal) (any, error)
	Plugins            *plugins.Service
	PermissionRequests permissionRequestService
	SandboxAdmin       func(context.Context, core.Principal, string, json.RawMessage) (any, error)
	ShellTasks         func(context.Context, core.Principal, core.ID, json.RawMessage) (json.RawMessage, error)
	SandboxInfo        func(context.Context, core.Principal) (any, error)
	Limiter            RateLimiter
	PreLoginCSRF       func(context.Context) (auth.Challenge, core.Secret, error)
	Listen             func(string, string) (net.Listener, error)
	OnListen           func(net.Addr)
}
type HTTPServer struct {
	config           config.Config
	services         Services
	options          Options
	origin           *url.URL
	mux              *http.ServeMux
	http             *http.Server
	mu               sync.Mutex
	serving, closing bool
	streams          int
	streamContext    context.Context
	cancelStreams    context.CancelFunc
}

func NewServer(cfg config.Config, services Services, o Options) (*HTTPServer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := services.Validate(); err != nil {
		return nil, err
	}
	if o.Limiter == nil || o.PreLoginCSRF == nil {
		return nil, core.Invalid("api", "rate limiter and pre-login CSRF issuer required")
	}
	if o.Listen == nil {
		o.Listen = net.Listen
	}
	origin, _ := url.Parse(cfg.HTTP.PublicURL)
	ctx, cancel := context.WithCancel(context.Background())
	s := &HTTPServer{config: cfg, services: services, options: o, origin: origin, mux: http.NewServeMux(), streamContext: ctx, cancelStreams: cancel}
	s.routes()
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout.Value(), IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	return s, nil
}
func (s *HTTPServer) Handler() http.Handler { return http.HandlerFunc(s.dispatch) }
func (s *HTTPServer) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.serving || s.closing {
		s.mu.Unlock()
		return core.NewError(core.FailedPrecondition, "server already started or stopped")
	}
	s.serving = true
	s.mu.Unlock()
	if s.config.HTTP.TLSMode == config.DirectTLS {
		if _, err := tls.LoadX509KeyPair(s.config.HTTP.CertFile, s.config.HTTP.KeyFile); err != nil {
			return core.Wrap(core.FailedPrecondition, "TLS certificate unavailable", err)
		}
	}
	listener, err := s.options.Listen("tcp", s.config.HTTP.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	if s.config.HTTP.TLSMode == config.LocalHTTP {
		host, _, err := net.SplitHostPort(listener.Addr().String())
		ip, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !ip.IsLoopback() {
			return core.NewError(core.Forbidden, "local listener must bind loopback")
		}
	}
	if s.options.OnListen != nil {
		s.options.OnListen(listener.Addr())
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.Execution.ShutdownTimeout.Value())
			defer cancel()
			_ = s.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	if s.config.HTTP.TLSMode == config.DirectTLS {
		err = s.http.ServeTLS(listener, s.config.HTTP.CertFile, s.config.HTTP.KeyFile)
	} else {
		err = s.http.Serve(listener)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	s.cancelStreams()
	s.mu.Unlock()
	return s.http.Shutdown(ctx)
}
func requestID() core.ID {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "request"
	}
	return core.ID(base64.RawURLEncoding.EncodeToString(b[:]))
}
func sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, err error, id core.ID) {
	status, body := PublicError(err, id)
	sendJSON(w, status, body)
}

type principalKey struct{}
type infoKey struct{}

func principal(r *http.Request) core.Principal {
	p, _ := r.Context().Value(principalKey{}).(core.Principal)
	return p
}
func info(r *http.Request) RequestInfo { v, _ := r.Context().Value(infoKey{}).(RequestInfo); return v }
func (s *HTTPServer) transport(r *http.Request) (string, error) {
	if !strings.EqualFold(r.Host, s.origin.Host) && !(s.config.HTTP.TLSMode == config.LANHTTP && s.allowedOrigin("http://"+r.Host)) {
		return "", core.NewError(core.Forbidden, "invalid host")
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "", core.NewError(core.Forbidden, "invalid peer")
	}
	address := peer.Unmap().String()
	if s.config.HTTP.PlainHTTP() {
		if !peer.IsLoopback() && !(s.config.HTTP.TLSMode == config.LANHTTP && peer.Unmap().IsPrivate()) {
			return "", core.NewError(core.Forbidden, "local peer required")
		}
		for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"} {
			if r.Header.Get(header) != "" {
				return "", core.NewError(core.Forbidden, "forwarded requests disabled in local mode")
			}
		}
		return address, nil
	}
	if s.config.HTTP.TLSMode == config.DirectTLS {
		if r.TLS == nil {
			return "", core.NewError(core.Forbidden, "HTTPS required")
		}
		return address, nil
	}
	trusted := false
	for _, network := range s.config.HTTP.TrustedProxies {
		prefix, err := netip.ParsePrefix(network)
		if err == nil && prefix.Contains(peer.Unmap()) {
			trusted = true
		}
	}
	if !trusted || r.Header.Get("X-Forwarded-Proto") != "https" {
		return "", core.NewError(core.Forbidden, "trusted HTTPS proxy required")
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				return "", core.NewError(core.Forbidden, "invalid proxy address")
			}
			candidate = candidate.Unmap()
			known := false
			for _, network := range s.config.HTTP.TrustedProxies {
				prefix, _ := netip.ParsePrefix(network)
				if prefix.Contains(candidate) {
					known = true
				}
			}
			address = candidate.String()
			if !known {
				break
			}
		}
	}
	return address, nil
}
func (s *HTTPServer) allowedOrigin(origin string) bool {
	if origin == s.origin.Scheme+"://"+s.origin.Host {
		return true
	}
	for _, allowed := range s.config.HTTP.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func (s *HTTPServer) authentication(r *http.Request) (auth.Authentication, error) {
	var out auth.Authentication
	if cookie, err := r.Cookie(s.cookieName(sessionCookie)); err == nil {
		out.Session = core.NewSecret(cookie.Value)
	}
	header := r.Header.Get("Authorization")
	if header != "" {
		scheme, value, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t\r\n") {
			return out, core.NewError(core.Unauthenticated, "invalid bearer")
		}
		out.Bearer = core.NewSecret(value)
	}
	return out, out.Validate()
}
func (s *HTTPServer) dispatch(w http.ResponseWriter, r *http.Request) {
	id := requestID()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.config.HTTP.PlainHTTP() {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	address, err := s.transport(r)
	if err != nil {
		fail(w, err, id)
		return
	}
	for _, key := range []string{"token", "access_token", "authorization", "session"} {
		if r.URL.Query().Has(key) {
			fail(w, core.Invalid("query", "credentials forbidden in query"), id)
			return
		}
	}
	origin := r.Header.Get("Origin")
	if origin != "" && !s.allowedOrigin(origin) {
		fail(w, core.NewError(core.Forbidden, "origin denied"), id)
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Vary", "Origin")
	}
	if r.Method == http.MethodOptions {
		if origin == "" {
			fail(w, core.NewError(core.Forbidden, "origin required"), id)
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PATCH, DELETE, PUT")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, Idempotency-Key, Last-Event-ID, X-File-Name, X-File-Name-Encoding, X-Task-ID")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	category := "request"
	if r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/login-links/redeem" || r.URL.Path == "/api/auth/login-links/preview" || r.URL.Path == "/api/auth/mfa/complete" {
		category = "login"
	}
	allowance, err := s.options.Limiter.Allow(r.Context(), RateKey{Category: category, Address: address})
	if err != nil {
		fail(w, err, id)
		return
	}
	if !allowance.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(allowance.RetryAfter.Seconds())+1))
		fail(w, core.NewError(core.RateLimited, "request rate limited"), id)
		return
	}
	if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
		webassets.Handler().ServeHTTP(w, r)
		return
	}
	public := r.URL.Path == "/api/auth/csrf" || r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/login-links/redeem" || r.URL.Path == "/api/auth/login-links/preview" || r.URL.Path == "/api/auth/mfa/complete"
	ctx := context.WithValue(r.Context(), infoKey{}, RequestInfo{ID: id, ClientAddress: address, Origin: origin, Secure: !s.config.HTTP.PlainHTTP()})
	if !public {
		a, err := s.authentication(r)
		if err != nil {
			fail(w, err, id)
			return
		}
		p, err := s.services.Auth.Authenticate(ctx, a)
		if err != nil {
			fail(w, err, id)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !a.Session.Empty() {
			if err = s.services.Browser.CheckCSRF(ctx, a, core.NewSecret(r.Header.Get("X-CSRF-Token")), origin); err != nil {
				fail(w, core.NewError(core.Forbidden, "CSRF check failed"), id)
				return
			}
		}
		ctx = context.WithValue(ctx, principalKey{}, p)
	} else if r.Method != http.MethodGet {
		cookie, err := r.Cookie(s.cookieName(loginCookie))
		if err != nil {
			fail(w, core.NewError(core.Forbidden, "pre-login CSRF required"), id)
			return
		}
		if err = s.services.Browser.CheckCSRF(ctx, auth.Authentication{Session: core.NewSecret(cookie.Value)}, core.NewSecret(r.Header.Get("X-CSRF-Token")), origin); err != nil {
			fail(w, core.NewError(core.Forbidden, "CSRF check failed"), id)
			return
		}
	}
	if r.URL.Path != "/api/events/stream" && r.URL.Path != "/api/debug/stream" && !strings.HasSuffix(r.URL.Path, "/browser/stream") {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.HTTP.RequestTimeout.Value())
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.config.HTTP.RequestTimeout.Value()))
	}
	limit := s.config.HTTP.MaxBodyBytes
	if r.URL.Path == "/api/plugins/install" && r.Method == http.MethodPost {
		limit = 8 << 20
	}
	if r.URL.Path == "/api/files" && r.Method == http.MethodPost {
		limit = s.config.Execution.MaxUploadBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	s.mux.ServeHTTP(w, r.WithContext(ctx))
}
func decode[T any](r *http.Request) (T, error) {
	var value T
	if content := r.Header.Get("Content-Type"); content != "" {
		media, _, err := mime.ParseMediaType(content)
		if err != nil || media != "application/json" {
			return value, core.Invalid("content_type", "JSON required")
		}
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, core.Wrap(core.InvalidArgument, "invalid JSON", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return value, core.Invalid("body", "single JSON object required")
	}
	return value, nil
}
func route[T any](s *HTTPServer, pattern string, call func(context.Context, core.Principal, T) (any, error)) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		value, err := decode[T](r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		result, err := call(r.Context(), principal(r), value)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		sendJSON(w, http.StatusOK, result)
	})
}
func queryPage(r *http.Request) (core.PageRequest, error) {
	q := core.PageRequest{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil {
			return q, core.Invalid("limit", "integer required")
		}
		q.Limit = n
	}
	return q.Normalize()
}
func pathID(r *http.Request) (core.ID, error) {
	id := core.ID(r.PathValue("id"))
	return id, id.Validate()
}
func queryID(r *http.Request, key string) core.ID { return core.ID(r.URL.Query().Get(key)) }
func (s *HTTPServer) handle(pattern string, call func(*http.Request) (any, error)) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		out, err := call(r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		sendJSON(w, http.StatusOK, out)
	})
}
func (s *HTTPServer) sse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	if s.closing || s.streams >= s.config.HTTP.MaxSSEConnections {
		s.mu.Unlock()
		fail(w, core.NewError(core.RateLimited, "stream limit reached"), info(r).ID)
		return
	}
	s.streams++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.streams--; s.mu.Unlock() }()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.streamContext.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	q := realtime.Query{After: r.Header.Get("Last-Event-ID"), Topics: r.URL.Query()["topic"]}
	if q.After == "" {
		q.After = r.URL.Query().Get("after")
	}
	subscription, err := s.services.Realtime.Subscribe(ctx, principal(r), q)
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	defer subscription.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	control := http.NewResponseController(w)
	_ = control.SetWriteDeadline(time.Now().Add(s.config.HTTP.StreamWriteTimeout.Value()))
	if _, err = io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	if err = control.Flush(); err != nil {
		return
	}
	type result struct {
		v   realtime.Visible
		err error
	}
	events := make(chan result, 1)
	go func() {
		for {
			v, err := subscription.Next(ctx)
			select {
			case events <- result{v, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	a, _ := s.authentication(r)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// The subscription observes revocation and expiry. Heartbeats only keep
			// the transport alive; event delivery still revalidates the identity.
			_ = control.SetWriteDeadline(time.Now().Add(s.config.HTTP.StreamWriteTimeout.Value()))
			if _, err = io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err = control.Flush(); err != nil {
				return
			}
		case next := <-events:
			if next.err != nil {
				return
			}
			if err = s.checkStreamPrincipal(ctx, principal(r), a); err != nil {
				return
			}
			data, err := json.Marshal(next.v)
			if err != nil {
				return
			}
			_ = control.SetWriteDeadline(time.Now().Add(s.config.HTTP.StreamWriteTimeout.Value()))
			cursor, err := realtime.Cursor(principal(r).OwnerID, q.Topics, next.v.Sequence)
			if err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "id: %s\nevent: update\ndata: %s\n\n", cursor, data); err != nil {
				return
			}
			if err = control.Flush(); err != nil {
				return
			}
		}
	}
}

var _ Server = (*HTTPServer)(nil)

func (s *HTTPServer) checkStreamPrincipal(ctx context.Context, p core.Principal, a auth.Authentication) error {
	if validator, ok := s.services.Auth.(auth.PrincipalValidator); ok {
		return validator.ValidatePrincipal(ctx, p)
	}
	_, err := s.services.Auth.Authenticate(ctx, a)
	return err
}

func (s *HTTPServer) cookieName(name string) string {
	if s.config.HTTP.TLSMode == config.LANHTTP {
		port := s.origin.Port()
		if port == "" {
			port = "80"
		}
		return "molr_lan_" + strings.TrimPrefix(name, "__Host-molr_") + "_p" + port
	}
	if s.config.HTTP.TLSMode == config.LocalHTTP {
		name = "molr_dev_" + strings.TrimPrefix(name, "__Host-molr_")
	}
	port := s.origin.Port()
	standard := "443"
	if s.config.HTTP.PlainHTTP() {
		standard = "80"
	}
	if port != "" && port != standard {
		return name + "_p" + port
	}
	return name
}
