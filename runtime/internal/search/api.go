// Package search defines authorized multi-source search and rebuildable index maintenance.
package search

import (
	"context"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
)

type Source string

const (
	Messages   Source = "messages"
	Assistants Source = "assistants"
	Resources  Source = "resources"
	Tasks      Source = "tasks"
	Memories   Source = "memories"
)

type Query struct {
	Text            string
	Sources         []Source
	AssistantID     core.ID
	IncludeArchived bool
	Page            core.PageRequest
}
type Hit struct {
	Memory      *memory.Ref `json:",omitempty"`
	Source      Source
	Object      core.Object
	AssistantID core.ID
	Title       string
	Snippet     string
	FileID      core.ID
	Score       float64
}

// Counts and snippets are computed only over currently authorized objects.
type Results struct {
	Hits   core.Page[Hit]
	Counts map[Source]int
}
type Service interface {
	Search(context.Context, core.Principal, Query) (Results, error)
}

// Indexing uses source versions so stale jobs cannot resurrect deleted data.
// Rebuild is owner-only; internal updates do not grant permission to query.
type Change struct {
	OwnerID core.ID
	Object  core.Object
	Version core.Version
	Deleted bool
}
type Maintenance interface {
	Rebuild(context.Context, core.Principal) (core.ID, error)
	Status(context.Context, core.Principal, core.ID) (IndexStatus, error)
}
type Indexer interface {
	Maintenance
	Apply(context.Context, Change) error
}
type IndexStatus struct {
	ID          core.ID
	State       string
	Processed   int64
	FailureCode core.Code
}
