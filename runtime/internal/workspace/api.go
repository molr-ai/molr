package workspace

import (
	"context"
	"io"
	"molr.ai/runtime/internal/core"
	"time"
)

// Workspace operations use conditional filesystem versions. Unknown write results
// must be reconciled by reading; these endpoints never replay a filesystem write.
type Request struct {
	Action          string
	Path            string
	ToPath          string
	ExpectedVersion string
	Text            string
	Offset          int
	Limit           int
}
type Entry struct {
	Name       string
	Path       string
	Type       string
	Size       int64
	ModifiedAt time.Time
	Version    string
	MediaType  string `json:",omitempty"`
}
type Listing struct {
	Entries    []Entry
	Version    string
	NextOffset int
	Truncated  bool
}
type Text struct {
	Entry
	Text      string
	Editable  bool
	Truncated bool
}
type Content struct {
	io.ReadSeekCloser
	Entry Entry
}
type Service interface {
	Query(context.Context, core.Principal, core.ID, Request) (any, error)
	Upload(context.Context, core.Principal, core.ID, Request, io.Reader) (Entry, error)
	Download(context.Context, core.Principal, core.ID, Request) (Content, error)
}
