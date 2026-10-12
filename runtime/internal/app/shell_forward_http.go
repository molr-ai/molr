package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
)

// Match every Molr cookie variant, including other development ports. No Molr
// credential, challenge or proof is ever delegated to the forwarded application.
func molrCookie(name string) bool {
	return strings.HasPrefix(name, "__Host-molr_") || strings.HasPrefix(name, "molr_dev_") || strings.HasPrefix(name, "molr_lan_") || strings.HasPrefix(name, "molr_forward_")
}
func sessionCookie(name string) bool {
	return molrCookie(name) && (strings.HasSuffix(name, "_session") || strings.Contains(name, "_session_p"))
}
func stripForwardCredentials(h http.Header) {
	req := http.Request{Header: h}
	cookies := req.Cookies()
	h.Del("Cookie")
	for _, c := range cookies {
		if !molrCookie(c.Name) {
			req.AddCookie(c)
		}
	}
	for _, k := range []string{"Authorization", "Proxy-Authorization", "X-CSRF-Token", "X-Task-ID", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Molr-Forward-Session-Cookie"} {
		h.Del(k)
	}
}
func (s *shellTasks) forwardScheme() string {
	if !s.forwardHTTP.PlainHTTP() || s.forwardConfig.CertFile != "" {
		return "https"
	}
	return "http"
}
func (s *shellTasks) forwardCertificate() (tls.Certificate, error) {
	cert, key := s.forwardConfig.CertFile, s.forwardConfig.KeyFile
	if cert == "" && s.forwardHTTP.TLSMode == config.DirectTLS {
		cert, key = s.forwardHTTP.CertFile, s.forwardHTTP.KeyFile
	}
	if cert == "" || key == "" {
		return tls.Certificate{}, core.NewError(core.FailedPrecondition, "HTTPS forwarding requires a certificate and key for the forward host")
	}
	return tls.LoadX509KeyPair(cert, key)
}
func (s *shellTasks) forwardPrincipal(ctx context.Context, req *http.Request, task shellTask) (core.Principal, error) {
	if s.identity == nil {
		return core.Principal{}, core.NewError(core.Unauthenticated, "forward authentication unavailable")
	}
	// Cookie names are selected by the login entry, never by this random port.
	// Reject ambiguous sessions instead of choosing a possibly different account.
	var principal core.Principal
	req.Header.Del("X-Molr-Forward-Session-Cookie")
	selected := ""
	if c, err := req.Cookie(forwardSelectorCookie(req)); err == nil {
		selected = c.Value
		if !sessionCookie(selected) {
			return principal, denied()
		}
	}
	loginPort := req.URL.Query().Get("__molr_login_port")
	if loginPort != "" {
		n, err := strconv.Atoi(loginPort)
		if err != nil || n < 1 || n > 65535 {
			return principal, denied()
		}
		selected = ""
	}
	count := 0
	for _, c := range req.Cookies() {
		if !sessionCookie(c.Name) {
			continue
		}
		if selected != "" && c.Name != selected {
			continue
		}
		if loginPort != "" && !strings.HasSuffix(c.Name, "_p"+loginPort) && !((loginPort == "443" || loginPort == "80") && strings.HasSuffix(c.Name, "_session")) {
			continue
		}
		count++
		if count > 16 {
			return principal, denied()
		}
		p, err := s.identity.Authenticate(ctx, auth.Authentication{Session: core.NewSecret(c.Value)})
		if err != nil {
			continue
		}
		if principal.SessionID != "" && principal.SessionID != p.SessionID {
			return principal, core.NewError(core.Forbidden, "multiple login sessions; use the matching Molr login host")
		}
		principal = p
		req.Header.Set("X-Molr-Forward-Session-Cookie", c.Name)
	}
	if principal.SessionID == "" {
		return principal, core.NewError(core.Unauthenticated, "sign in to Molr on this host before opening the forwarded service")
	}
	if principal.OwnerID != task.OwnerID {
		return principal, denied()
	}
	return principal, (access{s.store}).assistant(ctx, principal, task.AssistantID)
}
func (s *shellTasks) serveForward(ctx context.Context, e core.Execution, r *runningShell, index int, f *taskForward) {
	defer s.wg.Done()
	defer f.close()
	select {
	case <-r.ready:
	case <-f.ctx.Done():
		return
	}
	s.mu.Lock()
	task := r.task
	entry := task.Forwards[index]
	s.mu.Unlock()
	listener := net.Listener(&forwardLimitedListener{Listener: f.listener, local: f.slots, global: s.forwardSlots})
	if entry.Protocol != "http" {
		return
	} // TCP public mode is disabled.
	if s.forwardScheme() == "https" {
		cert, err := s.forwardCertificate()
		if err != nil {
			s.forwardState(r, index, "closed")
			return
		}
		listener = tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(call context.Context, _, _ string) (net.Conn, error) {
		return asForwardConn(s.router.dialForward(context.WithValue(call, managedShellKey{}, r.scope), e, task.SandboxID, entry.Port))
	}}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Rewrite: func(p *httputil.ProxyRequest) {
		p.Out.URL.Scheme = "http"
		p.Out.URL.Host = "sandbox.internal"
		p.Out.Host = p.In.Host
		stripForwardCredentials(p.Out.Header)
		query := p.Out.URL.Query()
		query.Del("__molr_login_port")
		p.Out.URL.RawQuery = query.Encode()
	}, ModifyResponse: func(resp *http.Response) error {
		values := resp.Header.Values("Set-Cookie")
		resp.Header.Del("Set-Cookie")
		for _, v := range values {
			c, err := http.ParseSetCookie(v)
			if err == nil && !molrCookie(c.Name) {
				resp.Header.Add("Set-Cookie", v)
			}
		}
		resp.Header.Del("Clear-Site-Data")
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "forwarded service unavailable", http.StatusBadGateway)
	}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		if req.Host != entry.Address {
			http.Error(w, "invalid forward host", http.StatusForbidden)
			return
		}
		if req.Method == http.MethodConnect {
			http.Error(w, "CONNECT is not supported", http.StatusMethodNotAllowed)
			return
		}
		origin := req.Header.Get("Origin")
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != req.Host || u.Scheme != s.forwardScheme() || u.Path != "" {
				http.Error(w, "origin denied", http.StatusForbidden)
				return
			}
		}
		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") && origin == "" {
			http.Error(w, "WebSocket origin required", http.StatusForbidden)
			return
		}
		p, err := s.forwardPrincipal(req.Context(), req, task)
		if err != nil {
			status := http.StatusForbidden
			if core.CodeOf(err) == core.Unauthenticated {
				status = http.StatusUnauthorized
			}
			http.Error(w, err.Error(), status)
			return
		}
		if name := req.Header.Get("X-Molr-Forward-Session-Cookie"); name != "" {
			http.SetCookie(w, &http.Cookie{Name: forwardSelectorCookie(req), Value: name, Path: "/", HttpOnly: true, Secure: s.forwardScheme() == "https", SameSite: http.SameSiteStrictMode})
		}
		watch, err := s.identity.WatchPrincipal(p)
		if err != nil {
			http.Error(w, "session unavailable", 401)
			return
		}
		defer watch.Close()
		call, cancel := context.WithCancel(req.Context())
		defer cancel()
		stop := context.AfterFunc(f.ctx, cancel)
		defer stop()
		if _, err = watch.Check(call); err != nil {
			http.Error(w, "session expired", 401)
			return
		}
		done := make(chan struct{})
		defer func() { cancel(); <-done }()
		go func() {
			defer close(done)
			for {
				left, err := watch.Check(call)
				if err != nil {
					cancel()
					return
				}
				if left <= 0 {
					left = time.Millisecond
				}
				timer := time.NewTimer(left)
				select {
				case <-call.Done():
					timer.Stop()
					return
				case <-watch.Changes():
					timer.Stop()
				case <-timer.C:
				}
			}
		}()
		proxy.ServeHTTP(w, req.WithContext(call))
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	stop := context.AfterFunc(f.ctx, func() { server.Close() })
	defer stop()
	// Readiness is a TCP reachability hint, never an authentication bypass.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			probe, cancel := context.WithTimeout(f.ctx, 4*time.Second)
			conn, err := s.router.dialForward(probe, e, task.SandboxID, entry.Port)
			cancel()
			if err == nil {
				conn.Close()
				s.forwardState(r, index, "ready")
			} else {
				s.forwardState(r, index, "waiting")
			}
			select {
			case <-f.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && f.ctx.Err() == nil {
		s.forwardState(r, index, "closed")
	}
}

// Remembers only which login entry to use, never a credential. Binding avoids
// switching to a different logged-in session after the selected session logs out.
func forwardSelectorCookie(r *http.Request) string {
	_, port, _ := net.SplitHostPort(r.Host)
	return "molr_forward_login_p" + port
}
