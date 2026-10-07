package app

import (
	"context"
	"io"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/auth"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestComposedApplicationServesAndReleasesInstance(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	_, token := ownerSession(t, b)
	limiter, err := api.NewRateLimiter(b.store, api.RateLimitOptions{})
	need(t, err)
	ready := make(chan net.Addr, 1)
	server, err := api.NewServer(cfg, b.Services, api.Options{Limiter: limiter, PreLoginCSRF: b.Services.Auth.(*auth.Manager).PreLoginCSRF, Listen: func(_, _ string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, OnListen: func(addr net.Addr) { ready <- addr }})
	need(t, err)
	b.Application.(*Runtime).dependencies.HTTP = server
	b.HTTP = server
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Application.Run(ctx) }()
	var address net.Addr
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("startup failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not start")
	}
	request, err := http.NewRequest("GET", "http://"+address.String()+"/api/assistants", nil)
	need(t, err)
	request.Host = "runtime.example"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: token.Reveal()})
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	need(t, err)
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("composed server: %d %s", response.StatusCode, body)
	}
	cancel()
	select {
	case err = <-done:
		need(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server shutdown did not finish")
	}
	need(t, b.Close())
	reopened, err := Build(t.Context(), cfg)
	need(t, err)
	need(t, reopened.Close())
}
