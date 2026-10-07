// Package app defines composition and lifecycle of the single Go server.
package app

import (
	"context"
	"reflect"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/sqlite"
)

// Application owns startup, recovery and shutdown of the single server.
// Startup order: validate config, instance lock, database/migrations, verify
// initialized owner, reconcile work, then scheduler and HTTP listener. A failed
// step unwinds acquired resources; no insecure fallback listener is permitted.
type Application interface {
	Run(context.Context) error
	Shutdown(context.Context) error
}
type Dependencies struct {
	Config    config.Config
	HTTP      api.Server
	Auth      auth.Service
	Bootstrap auth.Bootstrap
	Audit     audit.Service
	Database  sqlite.Database
	Restore   sqlite.Restorer
	Scheduler scheduler.Service
	Locker    platform.Locker
}

// Validate catches configuration and absent/typed-nil dependencies before any
// side effects. It does not claim the supplied implementations are secure.
func (d Dependencies) Validate() error {
	if err := d.Config.Validate(); err != nil {
		return err
	}
	value := reflect.ValueOf(d)
	typ := value.Type()
	for i := 1; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.IsNil() {
			return core.Invalid("dependencies."+typ.Field(i).Name, "required")
		}
		actual := field.Elem()
		switch actual.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if actual.IsNil() {
				return core.Invalid("dependencies."+typ.Field(i).Name, "required")
			}
		}
	}
	return nil
}
