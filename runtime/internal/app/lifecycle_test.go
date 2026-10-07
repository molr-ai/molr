package app

import (
	"context"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/sqlite"
	"molr.ai/runtime/tests/testkit"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
)

type lifecycleProbe struct {
	mu            sync.Mutex
	events        []string
	failures      map[string]error
	uninitialized bool
	ready         chan struct{}
	readyOnce     sync.Once
	running       int
}

func (p *lifecycleProbe) mark(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, name)
	if name == "http.serve" || name == "scheduler.run" {
		p.running++
		if p.running == 2 {
			p.readyOnce.Do(func() { close(p.ready) })
		}
	}
	return p.failures[name]
}

type observedDatabase struct {
	sqlite.Database
	probe *lifecycleProbe
}

func (d *observedDatabase) Migrate(ctx context.Context) error {
	if e := d.probe.mark("database.migrate"); e != nil {
		return e
	}
	return d.Database.Migrate(ctx)
}
func (d *observedDatabase) Close() error {
	if e := d.probe.mark("database.close"); e != nil {
		return e
	}
	return d.Database.Close()
}

type observedBootstrap struct {
	auth.Bootstrap
	probe *lifecycleProbe
}

func (b *observedBootstrap) Initialized(ctx context.Context) (bool, error) {
	if e := b.probe.mark("bootstrap.initialized"); e != nil {
		return false, e
	}
	b.probe.mu.Lock()
	uninitialized := b.probe.uninitialized
	b.probe.mu.Unlock()
	if uninitialized {
		return false, nil
	}
	return b.Bootstrap.Initialized(ctx)
}

type observedLock struct {
	platform.Lock
	probe *lifecycleProbe
}

func (l *observedLock) Close() error {
	if e := l.probe.mark("lock.close"); e != nil {
		return e
	}
	return l.Lock.Close()
}

type observedLocker struct {
	platform.Locker
	probe *lifecycleProbe
}

func (l *observedLocker) Acquire(ctx context.Context, path string) (platform.Lock, error) {
	if e := l.probe.mark("lock.acquire"); e != nil {
		return nil, e
	}
	lock, e := l.Locker.Acquire(ctx, path)
	if e != nil {
		return nil, e
	}
	return &observedLock{lock, l.probe}, nil
}

// Controlled lifecycle dependencies only: real Application remains the SUT.
// HTTP transport and scheduler business behavior are tested in their own suites.
type lifecycleHTTP struct{ probe *lifecycleProbe }

func (h *lifecycleHTTP) Handler() http.Handler { return http.NotFoundHandler() }
func (h *lifecycleHTTP) Serve(ctx context.Context) error {
	if e := h.probe.mark("http.serve"); e != nil {
		return e
	}
	<-ctx.Done()
	return nil
}
func (h *lifecycleHTTP) Shutdown(context.Context) error { return h.probe.mark("http.shutdown") }

type lifecycleScheduler struct {
	scheduler.Service
	probe *lifecycleProbe
}

func (s *lifecycleScheduler) Run(ctx context.Context) error {
	if e := s.probe.mark("scheduler.run"); e != nil {
		return e
	}
	<-ctx.Done()
	return nil
}
func (s *lifecycleScheduler) Shutdown(context.Context) error {
	return s.probe.mark("scheduler.shutdown")
}
func (s *lifecycleScheduler) Recover(context.Context) error { return s.probe.mark("scheduler.recover") }
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		dir := t.TempDir()
		db, err := sqlite.Open(filepath.Join(dir, "runtime.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		identity, err := auth.NewService(db.Store(), auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example"})
		testkit.RequireNoError(t, err)
		_, err = identity.Initialize(testkit.Context(t), auth.InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		auditService, err := audit.NewService(db.Store(), audit.Options{ValidatePrincipal: identity.ValidatePrincipal})
		testkit.RequireNoError(t, err)
		restorer, err := sqlite.NewRestorer(filepath.Join(dir, "runtime.db"))
		testkit.RequireNoError(t, err)
		probe := &lifecycleProbe{failures: make(map[string]error), ready: make(chan struct{})}
		cfg := config.Defaults(dir)
		cfg.HTTP.PublicURL = "https://molr.example"
		cfg.HTTP.CertFile = "cert.pem"
		cfg.HTTP.KeyFile = "key.pem"
		dependencies := Dependencies{Config: cfg, HTTP: &lifecycleHTTP{probe}, Auth: identity, Bootstrap: &observedBootstrap{identity, probe}, Audit: auditService, Database: &observedDatabase{db, probe}, Restore: restorer, Scheduler: &lifecycleScheduler{Service: scheduler.NewService(db.Store(), scheduler.Options{}), probe: probe}, Locker: &observedLocker{platform.NewLocker(), probe}}
		application, err := NewApplication(dependencies)
		testkit.RequireNoError(t, err)
		application.recoverCalls = func(context.Context) error { return probe.mark("calls.recover") }
		return contractFixture{Application: application, Dependencies: dependencies, Events: func() []string {
			probe.mu.Lock()
			defer probe.mu.Unlock()
			return append([]string(nil), probe.events...)
		}, Ready: probe.ready, FailAt: func(stage string, err error) { probe.mu.Lock(); probe.failures[stage] = err; probe.mu.Unlock() }, Uninitialized: func() { probe.mu.Lock(); probe.uninitialized = true; probe.mu.Unlock() }}
	}
}
