package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/storage/sqlite"
)

// ServeSandbox runs only the resource service: no owner account, Agent Loop,
// conversations, model credentials or Runtime API is initialized.
func ServeSandbox(ctx context.Context, cfg config.Config, token core.Secret) (err error) {
	cfg.Sandbox.ProtectedRoots = newMaintenance(cfg.ConfigPath, false).ProtectedRoots()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.HTTP.TLSMode != config.DirectTLS {
		return core.Invalid("tls_mode", "standalone sandbox requires direct TLS")
	}
	lock, err := platform.NewLocker().Acquire(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	db, err := sqlite.Open(filepath.Join(cfg.DataDir, "sandbox.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		return err
	}
	backend, err := newSandboxBackend(ctx, cfg, db.Store())
	if err != nil {
		return err
	}
	defer backend.Close()
	manager, err := sm.New(ctx, db.Store(), backend)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, manager.Close()) }()
	handler, err := sm.Handler(manager, token)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.HTTP.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stop, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = server.Shutdown(stop)
		case <-done:
		}
	}()
	err = server.ListenAndServeTLS(cfg.HTTP.CertFile, cfg.HTTP.KeyFile)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
