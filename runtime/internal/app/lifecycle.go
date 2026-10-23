package app

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"sync"
)

// Runtime owns lifecycle coordination; product composition belongs to Build.
type Runtime struct {
	refreshPrices    func(context.Context)
	runChannels      func(context.Context) error
	checkUpdates     func(context.Context)
	collectContexts  func(context.Context)
	collectAuth      func(context.Context)
	startShellTasks  func(context.Context) error
	recoverCalls     func(context.Context) error
	stopShellTasks   func()
	prepareSandbox   func(context.Context)
	dependencies     Dependencies
	mu               sync.Mutex
	started, stopped bool
	cancel           context.CancelFunc
	done             chan struct{}
	result           error
}

func NewApplication(dependencies Dependencies) (*Runtime, error) {
	if err := dependencies.Validate(); err != nil {
		return nil, err
	}
	return &Runtime{dependencies: dependencies, done: make(chan struct{})}, nil
}
func (a *Runtime) Run(ctx context.Context) (result error) {
	a.mu.Lock()
	if a.started || a.stopped {
		a.mu.Unlock()
		return core.NewError(core.FailedPrecondition, "application already started or stopped")
	}
	a.started = true
	runctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()
	defer cancel()
	var lock platform.Lock
	databaseOpened := false
	workersStarted := false
	var workers sync.WaitGroup
	defer func() {
		cancel()
		shutdownCtx, stop := context.WithTimeout(context.Background(), a.dependencies.Config.Execution.ShutdownTimeout.Value())
		defer stop()
		if workersStarted {
			result = errors.Join(result, a.dependencies.HTTP.Shutdown(shutdownCtx), a.dependencies.Scheduler.Shutdown(shutdownCtx))
			finished := make(chan struct{})
			go func() { workers.Wait(); close(finished) }()
			select {
			case <-finished:
			case <-shutdownCtx.Done():
				result = errors.Join(result, shutdownCtx.Err())
				a.mu.Lock()
				a.result = result
				a.stopped = true
				close(a.done)
				a.mu.Unlock()
				return
			}
		}
		if a.stopShellTasks != nil {
			a.stopShellTasks()
		}
		if databaseOpened {
			result = errors.Join(result, a.dependencies.Database.Close())
		}
		if lock != nil {
			result = errors.Join(result, lock.Close())
		}
		a.mu.Lock()
		a.result = result
		a.stopped = true
		close(a.done)
		a.mu.Unlock()
	}()
	if err := a.dependencies.Validate(); err != nil {
		return err
	}
	var err error
	lock, err = a.dependencies.Locker.Acquire(runctx, a.dependencies.Config.DataDir)
	if err != nil {
		return err
	}
	databaseOpened = true
	if err = a.dependencies.Database.Migrate(runctx); err != nil {
		return err
	}
	if err = a.dependencies.Database.Check(runctx); err != nil {
		return err
	}
	initialized, err := a.dependencies.Bootstrap.Initialized(runctx)
	if err != nil {
		return err
	}
	if !initialized {
		return core.NewError(core.FailedPrecondition, "initialize the owner using the local CLI before serving")
	}
	if a.prepareSandbox != nil {
		a.prepareSandbox(runctx)
	}
	if a.startShellTasks != nil {
		if err = a.startShellTasks(runctx); err != nil {
			return err
		}
	}
	if a.recoverCalls != nil {
		if err = a.recoverCalls(runctx); err != nil {
			return err
		}
	}
	if err = a.dependencies.Scheduler.Recover(runctx); err != nil {
		return err
	}
	if err = runctx.Err(); err != nil {
		return err
	}
	workersStarted = true
	if a.collectContexts != nil {
		workers.Add(1)
		go func() { defer workers.Done(); a.collectContexts(runctx) }()
	}
	if a.collectAuth != nil {
		workers.Add(1)
		go func() { defer workers.Done(); a.collectAuth(runctx) }()
	}
	if a.checkUpdates != nil {
		workers.Add(1)
		go func() { defer workers.Done(); a.checkUpdates(runctx) }()
	}
	if a.refreshPrices != nil {
		workers.Add(1)
		go func() { defer workers.Done(); a.refreshPrices(runctx) }()
	}
	results := make(chan error, 3)
	if a.runChannels != nil {
		workers.Add(1)
		go func() { defer workers.Done(); results <- a.runChannels(runctx) }()
	}
	workers.Add(2)
	go func() { defer workers.Done(); results <- a.dependencies.Scheduler.Run(runctx) }()
	go func() { defer workers.Done(); results <- a.dependencies.HTTP.Serve(runctx) }()
	select {
	case <-runctx.Done():
		return nil
	case err := <-results:
		if runctx.Err() != nil {
			return nil
		}
		if err == nil {
			return core.NewError(core.Unavailable, "server component stopped unexpectedly")
		}
		return err
	}
}
func (a *Runtime) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if !a.started && !a.stopped {
		a.stopped = true
		close(a.done)
	}
	cancel := a.cancel
	done := a.done
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		a.mu.Lock()
		result := a.result
		a.mu.Unlock()
		return result
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ Application = (*Runtime)(nil)
