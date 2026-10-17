package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

type observedDebugRevision struct {
	debugtrace.Control
	reads      atomic.Int64
	principals chan core.Principal
	completed  chan struct{}
}

func (d *observedDebugRevision) Revision(ctx context.Context, p core.Principal) (uint64, error) {
	d.reads.Add(1)
	if d.principals != nil {
		select {
		case d.principals <- p:
		default:
		}
	}
	revision, err := d.Control.Revision(ctx, p)
	if d.completed != nil {
		select {
		case d.completed <- struct{}{}:
		default:
		}
	}
	return revision, err
}

func TestDebugStreamIdleDoesNotPoll(t *testing.T) {
	f := fixture(t)
	implementation := f.Server.(*HTTPServer)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "debug.db"))
	testkit.RequireNoError(t, err)
	defer store.Close()
	trace := &observedDebugRevision{principals: make(chan core.Principal, 10), Control: debugtrace.New(store, debugtrace.Options{
		Enabled:   true,
		Authorize: func(context.Context, core.Principal) error { return nil },
	})}
	implementation.services.Debug = trace
	identity := &streamIdentity{Service: f.Services.Auth, validator: f.Services.Auth.(auth.PrincipalValidator)}
	implementation.services.Auth = identity
	server := httptest.NewTLSServer(f.Server.Handler())
	defer server.Close()
	r := f.SSERequest()
	r.URL.Scheme = "https"
	r.URL.Host = strings.TrimPrefix(server.URL, "https://")
	r.URL.Path = "/api/debug/stream"
	r.RequestURI = ""
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	response, err := server.Client().Do(r.WithContext(ctx))
	testkit.RequireNoError(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); done <- err }()
	beforeReads, beforeChecks := trace.reads.Load(), identity.checks.Load()
	time.Sleep(10500 * time.Millisecond)
	if reads, checks := trace.reads.Load()-beforeReads, identity.checks.Load()-beforeChecks; reads != 0 || checks != 0 {
		t.Fatalf("idle debug stream polled revision %d times and identity %d times", reads, checks)
	}
	p := <-trace.principals
	testkit.RequireNoError(t, identity.RevokeSession(t.Context(), p, p.SessionID))
	select {
	case <-done:
		if ctx.Err() != nil {
			t.Fatal("only request timeout closed stream")
		}
	case <-ctx.Done():
		t.Fatal("revocation did not close debug stream")
	}
}

func TestDebugStreamCommitReplayAndShutdown(t *testing.T) {
	f := fixture(t)
	implementation := f.Server.(*HTTPServer)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "debug.db"))
	testkit.RequireNoError(t, err)
	defer store.Close()
	trace := debugtrace.New(store, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
	observed := &observedDebugRevision{Control: trace, principals: make(chan core.Principal, 10)}
	implementation.services.Debug = observed
	server := httptest.NewTLSServer(f.Server.Handler())
	defer server.Close()
	open := func(after string) (*http.Response, <-chan string) {
		t.Helper()
		r := f.SSERequest()
		r.URL.Scheme, r.URL.Host, r.URL.Path, r.RequestURI = "https", strings.TrimPrefix(server.URL, "https://"), "/api/debug/stream", ""
		r.URL.RawQuery = "after=999"
		r.Header.Set("Last-Event-ID", after)
		response, err := server.Client().Do(r.WithContext(t.Context()))
		testkit.RequireNoError(t, err)
		if response.StatusCode != 200 {
			response.Body.Close()
			t.Fatal(response.Status)
		}
		lines := make(chan string, 32)
		go func() {
			defer close(lines)
			scan := bufio.NewScanner(response.Body)
			for scan.Scan() {
				lines <- scan.Text()
			}
		}()
		return response, lines
	}
	nextID := func(lines <-chan string, duration time.Duration) string {
		t.Helper()
		timer := time.NewTimer(duration)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatal("stream closed before revision")
				}
				if strings.HasPrefix(line, "id: ") {
					return strings.TrimPrefix(line, "id: ")
				}
			case <-timer.C:
				t.Fatal("committed revision did not arrive promptly")
			}
		}
	}
	response, lines := open("0")
	defer response.Body.Close()
	p := <-observed.principals
	started := time.Now()
	ctx := trace.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	id := nextID(lines, 500*time.Millisecond)
	t.Logf("commit-to-event=%s", time.Since(started))
	if revision, err := strconv.ParseUint(id, 10, 64); err != nil || revision == 0 {
		t.Fatal(id, err)
	}
	testkit.RequireNoError(t, response.Body.Close())
	trace.Finish(ctx, "succeeded", "")
	revision, err := trace.Revision(t.Context(), p)
	testkit.RequireNoError(t, err)
	resumed, resumedLines := open(id)
	defer resumed.Body.Close()
	if got := nextID(resumedLines, time.Second); got != fmt.Sprint(revision) {
		t.Fatalf("replay revision = %s, want %d", got, revision)
	}
	shutdownCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	testkit.RequireNoError(t, implementation.Shutdown(shutdownCtx))
	for {
		select {
		case _, ok := <-resumedLines:
			if !ok {
				return
			}
		case <-shutdownCtx.Done():
			t.Fatal("shutdown left stream open")
		}
	}
}

func TestDebugStreamClosesAtCredentialExpiry(t *testing.T) {
	f := fixture(t)
	implementation := f.Server.(*HTTPServer)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "expiry.db"))
	testkit.RequireNoError(t, err)
	defer store.Close()
	// Keep authentication time fixed through hashing, TLS and stream setup.
	// Advance it only after the stream has checked a still-valid credential.
	issuedAt := time.Now()
	var clockOffset atomic.Int64
	identity, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", SessionIdle: 700 * time.Millisecond, Now: func() time.Time {
		return issuedAt.Add(time.Duration(clockOffset.Load()))
	}})
	testkit.RequireNoError(t, err)
	password := core.NewSecret("synthetic-expiry-password-123!")
	_, err = identity.Initialize(t.Context(), auth.InitializeRequest{Username: "owner", Password: password})
	testkit.RequireNoError(t, err)
	login, err := identity.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: password})
	testkit.RequireNoError(t, err)
	implementation.services.Auth = identity
	trace := &observedDebugRevision{completed: make(chan struct{}, 4), Control: debugtrace.New(store, debugtrace.Options{Enabled: true, Authorize: func(ctx context.Context, p core.Principal) error { return identity.CheckScope(ctx, p, "debug.read") }})}
	implementation.services.Debug = trace
	server := httptest.NewTLSServer(f.Server.Handler())
	defer server.Close()
	r, err := http.NewRequest("GET", server.URL+"/api/debug/stream", nil)
	testkit.RequireNoError(t, err)
	r.Host = "molr.example"
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: login.Session.Token.Reveal()})
	r.Header.Set("Origin", "https://molr.example")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	response, err := server.Client().Do(r.WithContext(ctx))
	testkit.RequireNoError(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); done <- err }()
	<-trace.completed // Initial revision, before the connected response.
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	select {
	case <-trace.completed: // The expiry timer rechecked a still-valid credential.
	case err := <-done:
		t.Fatalf("stream closed before credential expiry: %v", err)
	case <-deadline.C:
		t.Fatal("stream did not recheck credential at its deadline")
	}
	// No revocation notification is sent; the stream must wake on its expiry timer.
	clockOffset.Store(int64(time.Second))
	select {
	case err := <-done:
		testkit.RequireNoError(t, err)
		if ctx.Err() != nil {
			t.Fatal("only request cancellation closed stream")
		}
	case <-deadline.C:
		t.Fatal("credential expiry did not close stream")
	}
}
