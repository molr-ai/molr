// Package testkit provides shared assertions and explicit implementation seams
// for Runtime's test-first behavioral contracts. It contains no fake services.
package testkit

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
)

// RequireFactory fails the test rather than skipping it when an implementation
// has not been wired. Tests below this seam must still exercise the public API
// and assert actual behavior, so binding a real fixture turns them executable.
// Factories create isolated state per test and register cleanup with t.Cleanup.
func RequireFactory[T any](t *testing.T, module string, factory func(*testing.T) T) T {
	t.Helper()
	if factory == nil {
		t.Fatalf("TDD implementation missing: %s; bind newContractFixture to the real module implementation", module)
	}
	return factory(t)
}

// Context bounds blocking contract tests even when the SUT has no work to read.
func Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func RequireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func RequireCode(t *testing.T, err error, want core.Code) {
	t.Helper()
	if got := core.CodeOf(err); got != want {
		t.Fatalf("error code = %q (%v), want %q", got, err, want)
	}
}
