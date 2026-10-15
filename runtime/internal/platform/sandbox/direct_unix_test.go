//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestDirectTerminalAndCancel(t *testing.T) {
	r := New(Options{QuotaStore: quotaTestStore(t), AllowFullAccess: true, PrivateRoot: t.TempDir(), Bwrap: "/does-not-exist", Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	caps, err := r.Capabilities(t.Context())
	if err != nil || !caps.FullAccess || caps.Shell {
		t.Fatal(caps, err)
	}
	e := testExecution()
	p, err := r.Start(t.Context(), e, platform.Command{FullAccess: true, Script: "read value; printf 'reply:%s\\n' \"$value\"", TTY: true, MaxOutputBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Terminal(t.Context(), e, "hello\n", 30, 90); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	exit, err := p.Wait(ctx)
	if err != nil || exit.Code != 0 {
		t.Fatal(exit, err)
	}
	data, err := io.ReadAll(p.Stdout())
	if err != nil || !strings.Contains(string(data), "reply:hello") {
		t.Fatal(string(data), err)
	}
	e.OperationID = "cancel"
	p, err = r.Start(t.Context(), e, platform.Command{FullAccess: true, Script: "sleep 3600 & wait", MaxOutputBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	exit, err = p.Wait(ctx)
	if err != nil || exit.Reason != "canceled" {
		t.Fatal(exit, err)
	}
}

func TestIsolatedNeverFallsBackToDirect(t *testing.T) {
	r := New(Options{QuotaStore: quotaTestStore(t), PrivateRoot: t.TempDir(), Bwrap: "/does-not-exist", Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if _, err := r.Start(t.Context(), testExecution(), testCommand()); err == nil {
		t.Fatal("unavailable isolation executed on host")
	}
}

func TestDirectRevocationStopsRunningCommand(t *testing.T) {
	var revoked atomic.Bool
	r := New(Options{QuotaStore: quotaTestStore(t), AllowFullAccess: true, PrivateRoot: t.TempDir(), Authorize: func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	}})
	defer r.Close()
	p, err := r.Start(t.Context(), testExecution(), platform.Command{FullAccess: true, Script: "sleep 3600", MaxOutputBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result, err := p.Wait(ctx)
	if err != nil || result.Reason != "authorization_changed" {
		t.Fatal(result, err)
	}
}
