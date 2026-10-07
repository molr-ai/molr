// Package dream consolidates bounded daily evidence into private memories.
package dream

import (
	"context"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/trigger"
	"time"
)

type Config struct {
	MaxDurationSeconds   int
	OwnerID, AssistantID core.ID
	Enabled              bool
	Hour, Minute         int
	Timezone             string
	NextDay              time.Time
	Version              core.Version
}
type ConfigureRequest struct {
	MaxDurationSeconds int
	Mutation           core.Mutation
	AssistantID        core.ID
	Enabled            bool
	Hour, Minute       int
	Timezone           string
}
type Request struct {
	Mutation    core.Mutation
	AssistantID core.ID
}
type Evidence struct {
	Source  core.Object
	Version string
	At      time.Time
	Kind    string
	Text    string
}
type Run struct {
	ReviewPath, ReviewVersion         string
	ReviewRetained                    bool
	SessionID                         core.ID
	Deadline                          time.Time
	Batch                             int
	Backfill                          bool
	SessionState                      string
	Decisions                         []Decision
	Output                            string
	UndoRetained                      []memory.Ref
	Manual                            bool
	ID, OwnerID, AssistantID, EventID core.ID
	Day, Timezone                     string
	Start, End                        time.Time
	State                             string
	Summary                           string
	Sources                           []Evidence
	MemoryFiles                       []memory.Ref
	MemoryVersions                    map[string]string
	MemoryStarted                     map[string]bool
	MemoryUpdated                     map[string]bool
	FailureCode                       string
	CreatedAt, UpdatedAt              time.Time
}
type Candidate struct {
	Text              string
	Action, Path      string
	Context, Evidence string
	Sources           []core.Object
	Replaces          *memory.Ref
	ExpectedVersion   string
}
type Result struct {
	Summary  string
	Memories []Candidate
}
type UndoRequest struct {
	Mutation core.Mutation
	ID       core.ID
}
type Decision struct {
	Text, Action, Reason string
	Memory               *memory.Ref
	Replaces             *memory.Ref `json:",omitempty"`
	PreviousText         string      `json:",omitempty"`
}
type Window struct {
	AssistantID     core.ID
	Start, End      time.Time
	Timezone, State string
	Manual          bool
	SessionID       core.ID
}
type Service interface {
	GetRun(context.Context, core.Principal, core.ID) (Run, error)
	Conflicts(context.Context, core.Principal, core.ID, trigger.Rule, time.Time, time.Time) ([]Window, error)
	Windows(context.Context, core.Principal, core.ID, time.Time, time.Time) ([]Window, error)
	Undo(context.Context, core.Principal, UndoRequest) (Run, error)
	Get(context.Context, core.Principal, core.ID) (Config, error)
	Configure(context.Context, core.Principal, ConfigureRequest) (Config, error)
	Request(context.Context, core.Principal, Request) (Run, error)
	Runs(context.Context, core.Principal, core.ID) ([]Run, error)
}
type Processor interface {
	// Prepare returns the batch evidence as JSON; model instructions belong to the Agent prompt templates.
	Prepare(context.Context, core.Principal, event.AssistantEvent) (string, error)
	Commit(context.Context, core.Principal, event.AssistantEvent, string) error
}
