//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
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
	"time"
)

func (r *Runner) checkDirect() error {
	if _, err := os.Stat(windowsShell()); err != nil {
		return unavailable("shell_unavailable")
	}
	return nil
}
func (r *Runner) startDirect(parent context.Context, e core.Execution, c platform.Command) (*process, error) {
	return r.startHost(parent, e, c)
}

// configure runs before exec, after allocating stdio. It must not add inherited FDs.
func (r *Runner) startHost(parent context.Context, e core.Execution, c platform.Command) (*process, error) {
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
		if !filepath.IsAbs(directory) && (c.FullAccess || (!strings.HasPrefix(directory, "/resources/") && directory != "/tmp" && !strings.HasPrefix(directory, "/tmp/"))) {
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
	if len(c.Script) > 12000 {
		return nil, core.Invalid("script", "Windows command scripts are limited to 12000 UTF-8 bytes")
	}
	if c.TTY && !c.FullAccess && r.terminalReason != "" {
		return nil, unavailable(r.terminalReason)
	}
	if c.TTY && !terminalAPIAvailable() {
		return nil, unavailable("terminal_platform_unsupported")
	}
	cmd := exec.Command(windowsShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); "+c.Script)
	cmd.Dir = directory
	cmd.Env = os.Environ()
	if !r.options.NoWorkspace {
		cmd.Env = append(cmd.Env, "MOLR_WORKSPACE="+work, "MOLR_GLOBAL="+platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID))
	}
	var cleanup func() error
	if !c.FullAccess {
		var err error
		cleanup, err = r.configureContainer(ctx, cmd, work, c)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !ok && cleanup != nil {
				if cleanup() != nil {
					r.closed = true
				}
			}
		}()
	}
	if c.TTY && c.FullAccess {
		spec := containerLaunch{TTY: true, FullAccess: true, Script: c.Script, Directory: cmd.Dir, Environment: cmd.Env}
		data, _ := json.Marshal(spec)
		helper := r.options.Helper
		var err error
		if helper == "" {
			helper, err = os.Executable()
			if err != nil {
				return nil, err
			}
		}
		cmd.Path = helper
		cmd.Args = []string{helper, "sandbox-helper", "windows-appcontainer"}
		cmd.Stdin = bytes.NewReader(data)
	}
	initialize := func() error { return nil }
	closePipe := func() {}
	if c.TTY {
		var err error
		initialize, closePipe, err = terminalPipe(cmd, p)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !ok {
				closePipe()
			}
		}()
	}
	cmd.Stdout, cmd.Stderr = p.stdout, p.stderr
	if c.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(c.StdoutSink, p.stdout)
	}
	if c.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(c.StderrSink, p.stderr)
	}
	cmd.WaitDelay = time.Second
	if err := r.options.Authorize(ctx, e, c); err != nil {
		return nil, err
	}
	job, err := startWindowsJob(cmd)
	if err != nil {
		return nil, err
	}
	if err = initialize(); err != nil {
		terminateWindowsJob(job)
		cmd.Wait()
		closeWindowsJob(job)
		return nil, err
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
		defer closePipe()
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
				_ = terminateWindowsJob(job)
				waitErr = <-done
				break
			}
		}
		p.err = terminateWindowsJob(job)
		closeWindowsJob(job)
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
		if r.options.NoWorkspace {
			os.RemoveAll(work)
		}
		if cleanup != nil && p.err == nil {
			p.err = cleanup()
		}
		cancel()
		p.stdout.finish()
		p.stderr.finish()
		close(p.done)
	}()
	return p, nil
}
