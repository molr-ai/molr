package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Application is real. Recording/fault-injectable dependency adapters observe
// acquired resources and calls; they must not implement the application itself.
// Events names: lock.acquire, database.migrate, bootstrap.initialized,
// scheduler.recover, scheduler.run, http.serve, http.shutdown,
// scheduler.shutdown, database.close, lock.close. Events is concurrency-safe.
type contractFixture struct {
	Application   Application
	Dependencies  Dependencies
	Events        func() []string
	Ready         <-chan struct{}
	FailAt        func(string, error)
	Uninitialized func()
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "app", newContractFixture)
}
func ordered(t *testing.T, events []string, names ...string) {
	t.Helper()
	last := -1
	for _, name := range names {
		found := -1
		for i, event := range events {
			if event == name {
				found = i
				break
			}
		}
		if found <= last {
			t.Fatalf("missing/out-of-order %q in %v", name, events)
		}
		last = found
	}
}
func absent(t *testing.T, events []string, names ...string) {
	t.Helper()
	for _, event := range events {
		for _, name := range names {
			if event == name {
				t.Fatalf("unexpected %q in %v", name, events)
			}
		}
	}
}
func TestTDDApplicationStartupAndShutdownOrder(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(testkit.Context(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Application.Run(ctx) }()
	select {
	case <-f.Ready:
	case err := <-done:
		t.Fatalf("Run exited before readiness: %v", err)
	case <-ctx.Done():
		t.Fatal("application did not become ready")
	}
	events := f.Events()
	ordered(t, events, "lock.acquire", "database.migrate", "bootstrap.initialized", "calls.recover", "scheduler.recover")
	ordered(t, events, "scheduler.recover", "scheduler.run")
	ordered(t, events, "scheduler.recover", "http.serve")
	testkit.RequireNoError(t, f.Application.Shutdown(ctx))
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown did not complete")
	}
	events = f.Events()
	ordered(t, events, "http.shutdown", "database.close", "lock.close")
	ordered(t, events, "scheduler.shutdown", "database.close")
	testkit.RequireNoError(t, f.Application.Shutdown(ctx))
	for _, name := range []string{"database.close", "lock.close"} {
		count := 0
		for _, e := range f.Events() {
			if e == name {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s called %d times", name, count)
		}
	}
}
func TestTDDApplicationStartupFailureUnwinds(t *testing.T) {
	for _, stage := range []string{"lock.acquire", "database.migrate", "bootstrap.initialized", "calls.recover", "scheduler.recover", "http.serve"} {
		t.Run(stage, func(t *testing.T) {
			f := fixture(t)
			sentinel := errors.New("injected startup failure")
			f.FailAt(stage, sentinel)
			err := f.Application.Run(testkit.Context(t))
			if !errors.Is(err, sentinel) {
				t.Fatalf("startup failure not propagated: %v", err)
			}
			events := f.Events()
			switch stage {
			case "lock.acquire":
				absent(t, events, "database.migrate", "http.serve", "scheduler.run", "lock.close")
			case "database.migrate", "bootstrap.initialized", "calls.recover", "scheduler.recover":
				absent(t, events, "http.serve", "scheduler.run")
				ordered(t, events, "database.close", "lock.close")
			case "http.serve":
				ordered(t, events, "scheduler.shutdown", "database.close", "lock.close")
			}
		})
	}
}
func TestTDDApplicationRefusesUninitializedPublicServer(t *testing.T) {
	f := fixture(t)
	f.Uninitialized()
	err := f.Application.Run(testkit.Context(t))
	if err == nil {
		t.Fatal("uninitialized server started")
	}
	absent(t, f.Events(), "scheduler.run", "http.serve")
	ordered(t, f.Events(), "database.close", "lock.close")
}
func TestTDDApplicationCanceledContextReleasesResources(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(testkit.Context(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Application.Run(ctx) }()
	select {
	case <-f.Ready:
	case err := <-done:
		t.Fatalf("Run returned before ready: %v", err)
	case <-ctx.Done():
		t.Fatal("startup timeout")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-testkit.Context(t).Done():
		t.Fatal("Run ignored cancellation")
	}
	ordered(t, f.Events(), "database.close", "lock.close")
}
func TestTDDDependenciesValidateEveryRequiredPort(t *testing.T) {
	f := fixture(t)
	testkit.RequireNoError(t, f.Dependencies.Validate())
	v := reflect.ValueOf(f.Dependencies)
	for i := 1; i < v.NumField(); i++ {
		t.Run(v.Type().Field(i).Name, func(t *testing.T) {
			copy := f.Dependencies
			reflect.ValueOf(&copy).Elem().Field(i).SetZero()
			testkit.RequireCode(t, copy.Validate(), core.InvalidArgument)
		})
	}
}

type nilServer struct{ api.Server }

func TestTDDDependenciesRejectInvalidConfigAndTypedNil(t *testing.T) {
	testkit.RequireCode(t, (Dependencies{}).Validate(), core.InvalidArgument)
	cfg := config.Defaults(t.TempDir())
	cfg.HTTP.PublicURL = "https://molr.example"
	cfg.HTTP.CertFile = "cert.pem"
	cfg.HTTP.KeyFile = "key.pem"
	var server *nilServer
	err := (Dependencies{Config: cfg, HTTP: server}).Validate()
	var detail *core.Error
	if !errors.As(err, &detail) || detail.Field != "dependencies.HTTP" {
		t.Fatalf("typed nil HTTP dependency accepted: %v", err)
	}
}
