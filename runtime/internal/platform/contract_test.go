package platform_test

import (
	"context"
	"io"
	"net/netip"
	"runtime"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/tests/testkit"
)

// The factory wires real platform implementations. Command prepares a harmless
// test helper executable in a temporary directory; it does not run the SUT.
// Helper modes: echo (fixed stdout/stderr), literal (echo argv verbatim), env
// (print ALLOWED and inherited-secret values), long (spawn a child and wait),
// output (emit more than MaxOutputBytes on both streams). The long helper signals
// DescendantStarted only after the OS child exists; DescendantsAlive observes
// actual OS processes. Neither hook synthesizes a Runner/Process return value.
// BindDestination modifies the real configured resource/permission fixture.
type contractFixture struct {
	Runner            platform.Runner
	Locker            platform.Locker
	Network           platform.NetworkGuard
	Execution         core.Execution
	DataDir           string
	Command           func(mode string) platform.Command
	BindDestination   func(core.ID, string)
	DescendantsAlive  func() bool
	DescendantStarted <-chan struct{}
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "platform", newContractFixture)
}

// Bound the test independently of whether the implementation honors contexts.
// Cleanup still returns if a broken Cancel ignores its deadline. The factory
// additionally owns an OS-level last-resort cleanup for its helper process IDs.
func cleanupProcess(t *testing.T, p platform.Process) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done := make(chan struct{}, 1)
		go func() { _ = p.Cancel(ctx); done <- struct{}{} }()
		select {
		case <-done:
		case <-ctx.Done():
			t.Error("process cleanup exceeded its deadline")
		}
	})
}

type streamResult struct {
	data []byte
	err  error
}
type processStreams struct{ stdout, stderr <-chan streamResult }

func drainProcess(p platform.Process) processStreams {
	read := func(reader io.Reader) <-chan streamResult {
		ch := make(chan streamResult, 1)
		go func() { data, err := io.ReadAll(reader); ch <- streamResult{data: data, err: err} }()
		return ch
	}
	return processStreams{stdout: read(p.Stdout()), stderr: read(p.Stderr())}
}
func collectStream(t *testing.T, watchdog context.Context, ch <-chan streamResult) []byte {
	t.Helper()
	select {
	case result := <-ch:
		testkit.RequireNoError(t, result.err)
		return result.data
	case <-watchdog.Done():
		t.Fatal("process stream did not close before watchdog")
		return nil
	}
}
func waitProcess(t *testing.T, ctx, watchdog context.Context, p platform.Process) (platform.Exit, error) {
	t.Helper()
	type result struct {
		exit platform.Exit
		err  error
	}
	done := make(chan result, 1)
	go func() { exit, err := p.Wait(ctx); done <- result{exit: exit, err: err} }()
	select {
	case result := <-done:
		return result.exit, result.err
	case <-watchdog.Done():
		t.Fatal("process Wait did not return before watchdog")
		return platform.Exit{}, watchdog.Err()
	}
}
func cancelProcess(t *testing.T, ctx, watchdog context.Context, p platform.Process) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Cancel(ctx) }()
	select {
	case err := <-done:
		testkit.RequireNoError(t, err)
	case <-watchdog.Done():
		t.Fatal("process Cancel did not return before watchdog")
	}
}

func TestTDDPlatformCapabilitiesAndProcessStreams(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	caps, err := f.Runner.Capabilities(ctx)
	testkit.RequireNoError(t, err)
	if caps.OS != runtime.GOOS {
		t.Fatalf("reported OS %q, want %q", caps.OS, runtime.GOOS)
	}
	process, err := f.Runner.Start(ctx, f.Execution, f.Command("echo"))
	testkit.RequireNoError(t, err)
	cleanupProcess(t, process)
	streams := drainProcess(process)
	stdout := collectStream(t, ctx, streams.stdout)
	stderr := collectStream(t, ctx, streams.stderr)
	exit, err := waitProcess(t, ctx, ctx, process)
	testkit.RequireNoError(t, err)
	if exit.Code != 0 || exit.TimedOut || exit.OutputTruncated {
		t.Fatalf("unexpected exit %+v", exit)
	}
	if string(stdout) != "stdout-data" || string(stderr) != "stderr-data" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestTDDPlatformDoesNotInterpretShellOrInheritSecrets(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	t.Setenv("MOLR_PLATFORM_PARENT_SECRET", "never-inherit-this")
	for _, mode := range []string{"literal", "env"} {
		t.Run(mode, func(t *testing.T) {
			command := f.Command(mode)
			command.Args = append(command.Args, "literal; echo injected")
			command.Environment = map[string]string{"MOLR_PLATFORM_ALLOWED": "allowed"}
			process, err := f.Runner.Start(ctx, f.Execution, command)
			testkit.RequireNoError(t, err)
			cleanupProcess(t, process)
			streams := drainProcess(process)
			bytes := collectStream(t, ctx, streams.stdout)
			_ = collectStream(t, ctx, streams.stderr)
			_, err = waitProcess(t, ctx, ctx, process)
			testkit.RequireNoError(t, err)
			output := string(bytes)
			if strings.Contains(output, "never-inherit-this") {
				t.Fatal("parent secret environment inherited")
			}
			if mode == "literal" && output != "literal; echo injected" {
				t.Fatalf("argv interpreted or altered: %q", output)
			}
			if mode == "env" && !strings.Contains(output, "allowed") {
				t.Fatal("explicit environment missing")
			}
		})
	}
}

func TestTDDPlatformRequiresDeclaredIsolation(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	caps, err := f.Runner.Capabilities(ctx)
	testkit.RequireNoError(t, err)
	command := f.Command("echo")
	command.RequireIsolation = true
	p, err := f.Runner.Start(ctx, f.Execution, command)
	if !caps.FileIsolation || !caps.NetworkIsolation || !caps.ProcessIsolation {
		testkit.RequireCode(t, err, core.FailedPrecondition)
		if p != nil {
			t.Fatal("created unrestricted process after isolation failure")
		}
		return
	}
	testkit.RequireNoError(t, err)
	cleanupProcess(t, p)
	streams := drainProcess(p)
	_ = collectStream(t, ctx, streams.stdout)
	_ = collectStream(t, ctx, streams.stderr)
	_, err = waitProcess(t, ctx, ctx, p)
	testkit.RequireNoError(t, err)
}

func TestTDDPlatformCancellationAndTimeoutReapProcessTree(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t)
			ctx := testkit.Context(t)
			// The watchdog must expire before the parent context. It cannot masquerade
			// as the command timeout under test.
			watchdog, stopWatchdog := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopWatchdog()
			command := f.Command("long")
			if mode == "timeout" {
				command.Timeout = 2 * time.Second
			}
			p, err := f.Runner.Start(ctx, f.Execution, command)
			testkit.RequireNoError(t, err)
			cleanupProcess(t, p)
			streams := drainProcess(p)
			select {
			case <-f.DescendantStarted:
			case <-watchdog.Done():
				t.Fatal("helper did not confirm child creation")
			}
			if !f.DescendantsAlive() {
				t.Fatal("helper handshake had no live descendant; cancellation would be untested")
			}
			if mode == "cancel" {
				cancelProcess(t, ctx, watchdog, p)
			}
			_ = collectStream(t, watchdog, streams.stdout)
			_ = collectStream(t, watchdog, streams.stderr)
			exit, err := waitProcess(t, ctx, watchdog, p)
			if ctx.Err() != nil {
				t.Fatal("parent context expired; command timeout was not independently demonstrated")
			}
			if err != nil && core.CodeOf(err) != core.Canceled && core.CodeOf(err) != core.DeadlineExceeded {
				t.Fatal(err)
			}
			if mode == "timeout" && !exit.TimedOut && core.CodeOf(err) != core.DeadlineExceeded {
				t.Fatal("timeout not observable", exit, err)
			}
			if mode == "cancel" && exit.Code == 0 && err == nil {
				t.Fatal("cancellation reported successful completion")
			}
			if f.DescendantsAlive() {
				t.Fatal("child process survived termination")
			}
		})
	}
}

func TestTDDPlatformOutputIsBounded(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	command := f.Command("output")
	command.MaxOutputBytes = 4096
	command.Timeout = time.Second
	p, err := f.Runner.Start(ctx, f.Execution, command)
	testkit.RequireNoError(t, err)
	cleanupProcess(t, p)
	streams := drainProcess(p)
	data := collectStream(t, ctx, streams.stdout)
	stderr := collectStream(t, ctx, streams.stderr)
	exit, err := waitProcess(t, ctx, ctx, p)
	testkit.RequireNoError(t, err)
	if len(data) > 4096 || len(stderr) > 4096 || !exit.OutputTruncated {
		t.Fatalf("unbounded or silently truncated output: %d %+v", len(data), exit)
	}
}

func TestTDDPlatformLockExcludesConcurrentInstanceAndReleases(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	lock, err := f.Locker.Acquire(ctx, f.DataDir)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })
	duplicate, err := f.Locker.Acquire(ctx, f.DataDir)
	testkit.RequireCode(t, err, core.Conflict)
	if duplicate != nil {
		t.Fatal("second instance acquired the lock")
	}
	testkit.RequireNoError(t, lock.Close())
	next, err := f.Locker.Acquire(ctx, f.DataDir)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, next.Close())
}

func TestTDDPlatformNetworkRejectsSSRFAndRebinding(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	resourceID := core.ID("provider")
	public := netip.MustParseAddr("93.184.216.34")
	origin := "https://provider.example.test"
	f.BindDestination(resourceID, origin)
	permitted := platform.Destination{ResourceID: resourceID, URL: origin + "/v1/model", ResolvedAddresses: []netip.Addr{public}}
	testkit.RequireNoError(t, f.Network.Check(ctx, f.Execution, permitted))
	for _, address := range []string{"127.0.0.1", "::1", "169.254.169.254", "169.254.170.2", "10.0.0.1", "fc00::1", "::ffff:127.0.0.1"} {
		t.Run(address, func(t *testing.T) {
			denied := permitted
			denied.ResolvedAddresses = []netip.Addr{public, netip.MustParseAddr(address)}
			testkit.RequireCode(t, f.Network.Check(ctx, f.Execution, denied), core.Forbidden)
		})
	}
	for _, target := range []string{"http://provider.example.test/v1", "https://other.example.test", "file:///etc/passwd", "https://user:pass@provider.example.test"} {
		denied := permitted
		denied.URL = target
		if err := f.Network.Check(ctx, f.Execution, denied); err == nil {
			t.Fatalf("unsafe target permitted: %s", target)
		}
	}
	wrongResource := permitted
	wrongResource.ResourceID = "unbound"
	testkit.RequireCode(t, f.Network.Check(ctx, f.Execution, wrongResource), core.Forbidden)
}

func TestTDDPlatformExplicitLANBindingDoesNotOpenEntireNetwork(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	ip := netip.MustParseAddr("192.168.10.20")
	f.BindDestination("lan-model", "https://lan-model.example.test")
	destination := platform.Destination{ResourceID: "lan-model", URL: "https://lan-model.example.test/v1", ResolvedAddresses: []netip.Addr{ip}}
	testkit.RequireNoError(t, f.Network.Check(ctx, f.Execution, destination))
	destination.ResolvedAddresses = []netip.Addr{netip.MustParseAddr("192.168.10.21")}
	testkit.RequireCode(t, f.Network.Check(ctx, f.Execution, destination), core.Forbidden)
}

func TestTDDPlatformCanceledContextDoesNotStartWork(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Runner.Start(ctx, f.Execution, f.Command("echo"))
	testkit.RequireCode(t, err, core.Canceled)
	_, err = f.Locker.Acquire(ctx, f.DataDir)
	testkit.RequireCode(t, err, core.Canceled)
}
