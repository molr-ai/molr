//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"context"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (r *Runner) checkDirect() error {
	if _, err := os.Stat("/bin/sh"); err != nil {
		return unavailable("shell_unavailable")
	}
	return nil
}
func (r *Runner) startDirect(parent context.Context, e core.Execution, c platform.Command) (*process, error) {
	if c.BrowserAssets {
		return r.startBrowser(parent, e, c)
	}
	return r.startHost(parent, e, c, nil)
}

// configure runs before exec, after allocating stdio. It must not add inherited FDs.
func (r *Runner) startHost(parent context.Context, e core.Execution, c platform.Command, configure func(*exec.Cmd, string, string) (func(), error)) (*process, error) {
	ctx, cancel := context.WithCancel(parent)
	deadline := c.AuthorizationDeadline
	if c.Timeout > 0 {
		d := time.Now().Add(c.Timeout)
		if deadline.IsZero() || d.Before(deadline) {
			deadline = d
		}
	}
	if !deadline.IsZero() {
		cancel()
		ctx, cancel = context.WithDeadline(parent, deadline)
	}
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	work := ""
	directory := c.Directory
	if r.options.NoWorkspace {
		// A private per-execution temporary directory is never a persistent home.
		var err error
		work, err = os.MkdirTemp(r.options.PrivateRoot, "remote-exec-")
		if err != nil {
			return nil, err
		}
		defer func() {
			if !ok {
				os.RemoveAll(work)
			}
		}()
		if directory == "" {
			directory = work
		}
		if !filepath.IsAbs(directory) {
			return nil, core.Invalid("directory", "remote execution requires an absolute directory")
		}
	} else {
		work = platform.AssistantWorkspace(r.options.PrivateRoot, r.options.WorkRoot, e.Principal.OwnerID, e.AssistantID)
		if err := os.MkdirAll(work, 0700); err != nil {
			return nil, err
		}
		if directory == "" || directory == "/work" {
			directory = work
		} else if strings.HasPrefix(directory, "/work/") {
			directory = filepath.Join(work, strings.TrimPrefix(directory, "/work/"))
		} else if !filepath.IsAbs(directory) {
			directory = filepath.Join(work, directory)
		}
	}
	budget := &outputBudget{left: c.MaxOutputBytes}
	p := &process{stdout: newStream(budget), stderr: newStream(budget), done: make(chan struct{}), cancel: cancel, operation: string(e.Principal.ExecutionID) + "/" + string(e.OperationID)}
	cmd := exec.Command("/bin/sh", "-c", c.Script)
	if c.TTY && !r.options.NoWorkspace {
		cmd = exec.Command("/bin/sh", "-c", `if [ -f "$MOLR_WORKSPACE/.profile" ]; then . "$MOLR_WORKSPACE/.profile"; fi; `+c.Script)
	}
	cmd.Dir = directory
	cmd.Env = os.Environ()
	if !r.options.NoWorkspace {
		cmd.Env = append(cmd.Env, "MOLR_WORKSPACE="+work, "MOLR_GLOBAL="+platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID))
	}
	cmd.Stdout, cmd.Stderr = p.stdout, p.stderr
	if c.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(c.StdoutSink, p.stdout)
	}
	if c.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(c.StderrSink, p.stderr)
	}
	cmd.SysProcAttr = directProcessAttributes()
	cmd.WaitDelay = time.Second
	var ttyDone chan struct{}
	var cleanup func()
	defer func() {
		if !ok && cleanup != nil {
			cleanup()
		}
	}()
	setup := func(tty string) error {
		if configure == nil {
			return nil
		}
		var err error
		cleanup, err = configure(cmd, work, tty)
		return err
	}
	if c.TTY {
		master, slave, err := openPTY()
		if err != nil {
			return nil, err
		}
		p.terminal = master
		defer slave.Close()
		defer func() {
			if !ok {
				master.Close()
			}
		}()
		destination := cmd.Stdout
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		cmd.SysProcAttr.Setctty, cmd.SysProcAttr.Ctty = true, 0
		// Start the reader only once the child has successfully started.
		ttyDone = make(chan struct{})
		if err := setup(slave.Name()); err != nil {
			return nil, err
		}
		if err := r.options.Authorize(ctx, e, c); err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		go func() { io.Copy(destination, master); close(ttyDone) }()
	} else {
		if err := setup(""); err != nil {
			return nil, err
		}
		if err := r.options.Authorize(ctx, e, c); err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
	}
	ok = true
	// Direct mode shares the environment's loopback. These are explicitly
	// selected host ports, not ports in a task-private network namespace.
	p.forward = func(call context.Context, port int) (net.Conn, error) {
		if ctx.Err() != nil || !slices.Contains(c.ForwardPorts, port) {
			return nil, core.NewError(core.Forbidden, "port not forwarded by this task")
		}
		// Recheck the execution's authorization context. A forwarding HTTP request
		// has its own context and does not carry the delegated sandbox request.
		if err := r.options.Authorize(ctx, e, c); err != nil {
			return nil, err
		}
		conn, err := (&net.Dialer{}).DialContext(call, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		tracked := &forwardConn{Conn: conn, release: func() {}}
		tracked.setStop(context.AfterFunc(ctx, func() { tracked.Close() }))
		return tracked, nil
	}
	go func() {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		var waitErr, stopErr error
	loop:
		for {
			select {
			case waitErr = <-done:
				break loop
			case <-ctx.Done():
				stopErr = ctx.Err()
			case <-tick.C:
				stopErr = r.options.Authorize(ctx, e, c)
			}
			if stopErr != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				waitErr = <-done
				break
			}
		}
		// Best-effort process-group cleanup. Native processes can create another
		// session; this is not a whole-descendant containment guarantee.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if ttyDone != nil {
			select {
			case <-ttyDone:
			case <-time.After(time.Second):
			}
			p.terminal.Close()
			<-ttyDone
		}
		p.exit.Code = cmd.ProcessState.ExitCode()
		if stopErr != nil {
			p.exit.Code = 125
			p.exit.Reason = "authorization_changed"
			if stopErr == context.Canceled {
				p.exit.Reason = "canceled"
			}
			if stopErr == context.DeadlineExceeded {
				p.exit.Reason = "timeout"
				p.exit.TimedOut = true
			}
		}
		if waitErr != nil && p.exit.Code == 0 {
			p.exit.Code = 125
		}
		budget.mu.Lock()
		p.exit.OutputTruncated = budget.truncated
		budget.mu.Unlock()
		if cleanup != nil {
			cleanup()
		}
		if r.options.NoWorkspace {
			os.RemoveAll(work)
		}
		cancel()
		p.stdout.finish()
		p.stderr.finish()
		close(p.done)
	}()
	return p, nil
}

const seatbeltCloseDescriptors = `seen=0
for entry in /dev/fd/*; do
 fd=${entry##*/}
 case "$fd" in ''|*[!0-9]*) continue;; esac
 seen=1
 case "$fd" in 0|1|2) continue;; esac
 eval "exec $fd>&-" || exit 125
done
[ "$seen" = 1 ] || exit 125
exec "$@"`
