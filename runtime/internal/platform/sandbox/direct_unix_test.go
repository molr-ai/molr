//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"bufio"
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
	revoked.Store(false)
	testTerminalRevocation(t, r, platform.Command{FullAccess: true, MaxOutputBytes: 8192}, &revoked)
}

// Exercise the same readiness handshake on native Seatbelt and direct Unix PTYs.
func testTerminalRevocation(t *testing.T, r *Runner, c platform.Command, revoked *atomic.Bool) {
	t.Helper()
	// Deliberately exceed the old fixed 100ms waits before readiness and reply.
	c.Script = "sleep 0.3; echo READY; read line; sleep 0.3; echo RECEIVED:$line; sleep 30"
	c.TTY = true
	c.Managed = true
	c.Timeout = 0
	e := testExecution()
	e.OperationID = "terminal-revocation"
	p, err := r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	readCtx, stopReading := context.WithCancel(t.Context())
	defer stopReading()
	lines := make(chan string)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(p.Stdout())
		defer close(lines)
		defer close(readErr)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-readCtx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()
	var output strings.Builder
	waitLine := func(want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("terminal closed before %q: %v; output: %q", want, <-readErr, output.String())
				}
				output.WriteString(line + "\n")
				if line == want {
					return
				}
			case <-ctx.Done():
				t.Fatalf("waiting for terminal %q: %v; output: %q", want, ctx.Err(), output.String())
			}
		}
	}
	waitLine("READY")
	if err := r.Terminal(t.Context(), e, "hello\n", 40, 120); err != nil {
		t.Fatal(err)
	}
	waitLine("RECEIVED:hello")
	revoked.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	exit, err := p.Wait(ctx)
	if err != nil || exit.Reason != "authorization_changed" || exit.Code != 125 {
		t.Fatalf("revocation: %+v %v; output: %q", exit, err, output.String())
	}
}
