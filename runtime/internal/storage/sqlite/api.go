// Package sqlite defines transactional persistence and offline backup/restore.
package sqlite

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type Schema struct {
	Version           uint32
	CheckpointVersion uint32
}
type Backup struct {
	ID        core.ID
	Schema    Schema
	CreatedAt time.Time
}

// Database owns one local DB connection pool. Backup returns an opaque artifact,
// not a public filesystem URL. Restore is offline-only and invalidates auth data.
type Database interface {
	Store() persistence.Store
	Schema(context.Context) (Schema, error)
	Migrate(context.Context) error
	Check(context.Context) error
	Backup(context.Context) (Backup, error)
	Close() error
}
type Restorer interface {
	Restore(context.Context, Backup) error
}
