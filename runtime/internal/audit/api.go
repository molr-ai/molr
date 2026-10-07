// Package audit records security events separately from chat history and diagnostic logs.
package audit

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

type Outcome string

const (
	Succeeded Outcome = "succeeded"
	Denied    Outcome = "denied"
	Failed    Outcome = "failed"
)

// Record intentionally has no free-form request/body/secret field.
// SubjectID may be empty for failed unauthenticated login attempts.
type FileTarget struct {
	Scope       string
	AssistantID core.ID
	Path        string
}
type Record struct {
	File          *FileTarget `json:",omitempty"`
	ID            core.ID
	OwnerID       core.ID
	SubjectID     core.ID
	ActorKind     core.ActorKind
	Action        string
	Target        core.Object
	Outcome       Outcome
	ReasonCode    string
	RequestID     core.ID
	SourceAddress string
	At            time.Time
}
type Query struct {
	Action  string
	Outcome Outcome
	From    time.Time
	Until   time.Time
	Page    core.PageRequest
}

// Recorder is trusted-internal only. Reader checks current owner authorization.
type Recorder interface {
	Record(context.Context, Record) error
}
type Reader interface {
	List(context.Context, core.Principal, Query) (core.Page[Record], error)
}
type Service interface {
	Recorder
	Reader
}
