package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type RunnerOptions struct {
	Authorize          func(context.Context, core.Execution, Command) error
	AllowedExecutables []string
}
type ProcessRunner struct{ options RunnerOptions }

func NewRunner(options RunnerOptions) *ProcessRunner { return &ProcessRunner{options: options} }
func (r *ProcessRunner) Capabilities(ctx context.Context) (Capabilities, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return Capabilities{}, err
	}
	return Capabilities{OS: runtime.GOOS}, nil
}
func (r *ProcessRunner) Start(ctx context.Context, execution core.Execution, command Command) (Process, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return nil, err
	}
	if err := execution.Validate(); err != nil {
		return nil, err
	}
	if command.RequireIsolation {
		return nil, core.NewError(core.FailedPrecondition, "OS sandbox is not configured")
	}
	if r.options.Authorize == nil {
		return nil, core.NewError(core.Forbidden, "process execution disabled")
	}
	if err := r.options.Authorize(ctx, execution, command); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(command.Executable) || !filepath.IsAbs(command.Directory) || command.Timeout <= 0 || command.MaxOutputBytes <= 0 || command.MaxOutputBytes > 64<<20 {
		return nil, core.Invalid("command", "absolute paths and bounded execution required")
	}
	allowed := false
	for _, path := range r.options.AllowedExecutables {
		if filepath.Clean(path) == filepath.Clean(command.Executable) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, core.NewError(core.Forbidden, "executable not allowed")
	}
	cmd := exec.Command(command.Executable, command.Args...)
	cmd.Dir = command.Directory
	cmd.Env = []string{}
	for name, value := range command.Environment {
		if name == "" || strings.ContainsAny(name, "=\x00") || strings.ContainsRune(value, 0) {
			return nil, core.Invalid("environment", "invalid environment")
		}
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	configureProcess(cmd)
	budget := &outputBudget{remaining: command.MaxOutputBytes}
	p := &runningProcess{cmd: cmd, done: make(chan struct{}), stdout: newOutput(budget), stderr: newOutput(budget), budget: budget}
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, core.Wrap(core.Unavailable, "cannot start approved command", err)
	}
	go func() {
		err := cmd.Wait()
		exit := Exit{Code: cmd.ProcessState.ExitCode()}
		if err != nil {
			var status *exec.ExitError
			if !errors.As(err, &status) {
				p.err = core.Wrap(core.Unavailable, "process wait failed", err)
			}
		}
		exit.TimedOut = p.timedOut.Load()
		if p.canceled.Load() && p.err == nil {
			code := core.Canceled
			if exit.TimedOut {
				code = core.DeadlineExceeded
			}
			p.err = core.NewError(code, "process interrupted")
		}
		exit.OutputTruncated = budget.truncated.Load()
		p.exit = exit
		p.stdout.finish()
		p.stderr.finish()
		close(p.done)
	}()
	go func() {
		timer := time.NewTimer(command.Timeout)
		defer timer.Stop()
		select {
		case <-p.done:
			return
		case <-ctx.Done():
		case <-timer.C:
			p.timedOut.Store(true)
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = p.Cancel(cleanup)
	}()
	return p, nil
}

type outputBudget struct {
	mu        sync.Mutex
	remaining int64
	truncated atomic.Bool
}
type output struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	closed  bool
	changed chan struct{}
	budget  *outputBudget
}

func newOutput(budget *outputBudget) *output {
	return &output{changed: make(chan struct{}), budget: budget}
}
func (o *output) Write(data []byte) (int, error) {
	full := len(data)
	o.budget.mu.Lock()
	n := int64(full)
	if n > o.budget.remaining {
		n = o.budget.remaining
		o.budget.truncated.Store(true)
	}
	o.budget.remaining -= n
	o.budget.mu.Unlock()
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed && n > 0 {
		_, _ = o.buffer.Write(data[:int(n)])
		close(o.changed)
		o.changed = make(chan struct{})
	}
	return full, nil
}
func (o *output) Read(data []byte) (int, error) {
	for {
		o.mu.Lock()
		if o.buffer.Len() > 0 {
			n, err := o.buffer.Read(data)
			o.mu.Unlock()
			return n, err
		}
		if o.closed {
			o.mu.Unlock()
			return 0, io.EOF
		}
		changed := o.changed
		o.mu.Unlock()
		<-changed
	}
}
func (o *output) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.closed = true
		close(o.changed)
	}
}

type runningProcess struct {
	cmd            *exec.Cmd
	stdout, stderr *output
	budget         *outputBudget
	done           chan struct{}
	exit           Exit
	err            error
	timedOut       atomic.Bool
	canceled       atomic.Bool
	cancelOnce     sync.Once
}

func (p *runningProcess) Stdout() io.Reader { return p.stdout }
func (p *runningProcess) Stderr() io.Reader { return p.stderr }
func (p *runningProcess) Wait(ctx context.Context) (Exit, error) {
	select {
	case <-p.done:
		return p.exit, p.err
	case <-ctx.Done():
		return Exit{}, persistence.CheckContext(ctx)
	}
}
func (p *runningProcess) Cancel(ctx context.Context) error {
	p.cancelOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		p.canceled.Store(true)
		go terminateProcess(p.cmd, p.done)
	})
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	}
}
