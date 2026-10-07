// Package todo manages user todos without implicitly starting assistant work.
package todo

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

type Todo struct {
	ID          core.ID
	OwnerID     core.ID
	Title       string
	Notes       string
	AssistantID core.ID
	DueAt       *time.Time
	StartAt     *time.Time
	EndAt       *time.Time
	Completed   bool
	Version     core.Version
}
type Query struct {
	AssistantID core.ID
	Completed   *bool
	Page        core.PageRequest
}
type CreateRequest struct {
	Mutation    core.Mutation
	Title       string
	Notes       string
	AssistantID core.ID
	DueAt       *time.Time
	StartAt     *time.Time
	EndAt       *time.Time
}
type UpdateRequest struct {
	CreateRequest
	ID        core.ID
	Completed bool
}
type DeleteRequest struct {
	Mutation core.Mutation
	ID       core.ID
}

// Assigning an assistant does not create a task, grant access or trigger a Loop.
type Service interface {
	Create(context.Context, core.Principal, CreateRequest) (Todo, error)
	Get(context.Context, core.Principal, core.ID) (Todo, error)
	List(context.Context, core.Principal, Query) (core.Page[Todo], error)
	Update(context.Context, core.Principal, UpdateRequest) (Todo, error)
	Delete(context.Context, core.Principal, DeleteRequest) error
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Todo, error)
	List(context.Context, core.ID, Query) (core.Page[Todo], error)
	Insert(context.Context, Todo) error
	Save(context.Context, Todo, core.Version) error
	Delete(context.Context, core.ID, core.ID, core.Version) error
}
