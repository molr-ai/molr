package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Requests use implementation-specific routes, never a test-only handler.
// Protected reads, owner mutations and SSE must target real API endpoints.
// ServiceCalls records calls at injected service boundaries; RevokeSession
// persists revocation. RateLimit is configured for two attempts per minute.
type contractFixture struct {
	SeedNotification   func() core.ID
	SeedDreamAssistant func() core.ID
	Server             Server
	Services           Services
	Limiter            RateLimiter
	ReadRequest        func() *http.Request
	MutationRequest    func() *http.Request
	SSERequest         func() *http.Request
	StripCredentials   func(*http.Request)
	StripCSRF          func(*http.Request)
	RevokeSession      func()
	ServiceCalls       func() int
	Advance            func(time.Duration)
	FailLimiter        func()
	Started            <-chan struct{}
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "api", newContractFixture)
}
func TestTDDServerHandlerAuthAndCSRFBeforeServices(t *testing.T) {
	f := fixture(t)
	h := f.Server.Handler()
	if h == nil {
		t.Fatal("nil handler")
	}
	for _, tc := range []struct {
		name    string
		request func() *http.Request
		mutate  func(*http.Request)
		status  int
	}{
		{"anonymous read", f.ReadRequest, f.StripCredentials, http.StatusUnauthorized},
		{"missing CSRF", f.MutationRequest, f.StripCSRF, http.StatusForbidden},
		{"foreign origin", f.MutationRequest, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.request()
			tc.mutate(r)
			before := f.ServiceCalls()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			if f.ServiceCalls() != before {
				t.Fatal("rejected request reached protected service")
			}
		})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, f.ReadRequest())
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("authorized read status=%d", w.Code)
	}
}
func TestTDDServerSSEChecksLiveSession(t *testing.T) {
	f := fixture(t)
	server := httptest.NewTLSServer(f.Server.Handler())
	defer server.Close()
	r := f.SSERequest()
	r.URL.Scheme = "https"
	r.URL.Host = strings.TrimPrefix(server.URL, "https://")
	r.RequestURI = ""
	ctx, cancel := context.WithTimeout(testkit.Context(t), 3*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	response, err := server.Client().Do(r)
	testkit.RequireNoError(t, err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("authenticated SSE did not open")
	}
	finished := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); finished <- err }()
	f.RevokeSession()
	select {
	case err := <-finished:
		if ctx.Err() != nil {
			t.Fatalf("stream only stopped on test cancellation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("SSE continued after session revocation")
	}
	w := httptest.NewRecorder()
	f.Server.Handler().ServeHTTP(w, f.SSERequest())
	if w.Code != http.StatusUnauthorized {
		t.Fatal("revoked session reconnected to SSE")
	}
}
func TestTDDServerServeAndShutdown(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(testkit.Context(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Server.Serve(ctx) }()
	select {
	case <-f.Started:
	case err := <-done:
		t.Fatalf("Serve exited before readiness: %v", err)
	case <-ctx.Done():
		t.Fatal("server did not start")
	}
	testkit.RequireNoError(t, f.Server.Shutdown(ctx))
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not drain")
	}
	testkit.RequireNoError(t, f.Server.Shutdown(ctx))
}
func TestTDDRateLimiterBoundedIndependentAndFailureClosed(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	key := RateKey{Category: "login", Account: "owner", Address: "192.0.2.1"}
	for i := 0; i < 2; i++ {
		a, err := f.Limiter.Allow(ctx, key)
		testkit.RequireNoError(t, err)
		if !a.Allowed {
			t.Fatal("rate limit rejected initial allowance")
		}
	}
	denied, err := f.Limiter.Allow(ctx, key)
	testkit.RequireNoError(t, err)
	if denied.Allowed || denied.RetryAfter <= 0 {
		t.Fatal("exhausted limit accepted")
	}
	other := key
	other.Address = "192.0.2.2"
	other.Account = "other"
	a, err := f.Limiter.Allow(ctx, other)
	testkit.RequireNoError(t, err)
	if !a.Allowed {
		t.Fatal("unrelated identity shared exhausted quota")
	}
	f.Advance(time.Minute + time.Second)
	a, err = f.Limiter.Allow(ctx, key)
	testkit.RequireNoError(t, err)
	if !a.Allowed {
		t.Fatal("window did not recover")
	}
	f.FailLimiter()
	a, err = f.Limiter.Allow(ctx, key)
	if err == nil || a.Allowed {
		t.Fatal("limiter outage failed open")
	}
	before := f.ServiceCalls()
	w := httptest.NewRecorder()
	f.Server.Handler().ServeHTTP(w, f.ReadRequest())
	if w.Code < 400 || f.ServiceCalls() != before {
		t.Fatal("HTTP bypassed failed limiter")
	}
}
func TestTDDServicesValidationAllFields(t *testing.T) {
	f := fixture(t)
	testkit.RequireNoError(t, f.Services.Validate())
	v := reflect.ValueOf(f.Services)
	for i := 0; i < v.NumField(); i++ {
		t.Run(v.Type().Field(i).Name, func(t *testing.T) {
			copy := f.Services
			reflect.ValueOf(&copy).Elem().Field(i).SetZero()
			testkit.RequireCode(t, copy.Validate(), core.InvalidArgument)
		})
	}
}
func TestTDDPublicErrorStatusMapping(t *testing.T) {
	cases := map[core.Code]int{core.InvalidArgument: 400, core.Unauthenticated: 401, core.Forbidden: 403, core.NotFound: 404, core.Conflict: 409, core.FailedPrecondition: 409, core.RateLimited: 429, core.BudgetExceeded: 403, core.Unavailable: 503, core.Canceled: 408, core.DeadlineExceeded: 504, core.Internal: 500}
	for code, want := range cases {
		status, response := PublicError(core.NewError(code, "sensitive diagnostic"), "request-id")
		if status != want || response.Code != code || response.RequestID != "request-id" || strings.Contains(response.Message, "sensitive") {
			t.Fatalf("mapping failed for %s", code)
		}
	}
}
