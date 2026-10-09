package api

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

type streamIdentity struct {
	auth.Service
	validator auth.PrincipalValidator
	checks    atomic.Int64
}

func (a *streamIdentity) ValidatePrincipal(ctx context.Context, p core.Principal) error {
	a.checks.Add(1)
	return a.validator.ValidatePrincipal(ctx, p)
}

func TestStreamIdleDoesNotPollHTTPIdentity(t *testing.T) {
	f := fixture(t)
	implementation := f.Server.(*HTTPServer)
	identity := &streamIdentity{Service: f.Services.Auth, validator: f.Services.Auth.(auth.PrincipalValidator)}
	implementation.services.Auth = identity
	server := httptest.NewTLSServer(f.Server.Handler())
	defer server.Close()
	r := f.SSERequest()
	r.URL.Scheme = "https"
	r.URL.Host = strings.TrimPrefix(server.URL, "https://")
	r.RequestURI = ""
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	response, err := server.Client().Do(r.WithContext(ctx))
	testkit.RequireNoError(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); done <- err }()
	before := identity.checks.Load()
	time.Sleep(600 * time.Millisecond)
	if delta := identity.checks.Load() - before; delta != 0 {
		t.Fatalf("idle HTTP layer rechecked identity %d times", delta)
	}
	f.RevokeSession()
	select {
	case <-done:
		if ctx.Err() != nil {
			t.Fatal("only request timeout closed stream")
		}
	case <-ctx.Done():
		t.Fatal("revocation did not close stream")
	}
}
