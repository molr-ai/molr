// Package files separates authorized file operations from private object storage paths.
package files

import (
	"context"
	"io"
	"time"

	"molr.ai/runtime/internal/core"
)

type State string

const (
	Staged State = "staged"
	Ready  State = "ready"
	Failed State = "failed"
)

type File struct {
	AssistantID core.ID // Set by Runtime for assistant uploads, never supplied by the caller.
	ID          core.ID
	OwnerID     core.ID
	TaskID      core.ID
	Name        string
	MediaType   string
	Size        int64
	SHA256      string
	State       State
	CreatedAt   time.Time
}
type UploadRequest struct {
	Mutation  core.Mutation
	Name      string
	MediaType string
	Size      int64
	TaskID    core.ID
	Body      io.Reader
}

// Download never returns a local path. Caller must close Body. Untrusted types
// require safe Content-Disposition/CSP handling by the HTTP adapter.
type Download struct {
	File File
	Body io.ReadCloser
}
type Service interface {
	Upload(context.Context, core.Principal, UploadRequest) (File, error)
	Get(context.Context, core.Principal, core.ID) (File, error)
	Open(context.Context, core.Principal, core.ID) (Download, error)
	Delete(context.Context, core.Principal, core.ID) error
}

// Objects is a trusted filesystem port, not an Agent-accessible capability.
// Put stages data under generated IDs, validates byte limits and hashes it.
// Commit is idempotent; metadata publication occurs only after object commit.
// Recovery resolves orphan/staged records because SQL and files are not atomic.
type Objects interface {
	Put(context.Context, core.ID, io.Reader, int64) (StagedObject, error)
	Commit(context.Context, StagedObject) error
	Open(context.Context, core.ID) (io.ReadCloser, error)
	Remove(context.Context, core.ID) error
	Recover(context.Context) error
}
type StagedObject struct {
	ID     core.ID
	Size   int64
	SHA256 string
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (File, error)
	Insert(context.Context, File) error
	SetState(context.Context, core.ID, core.ID, State) error
	Delete(context.Context, core.ID, core.ID) error
}
