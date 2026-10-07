// Package core defines the small shared vocabulary for the fixed-function Runtime.
package core

import (
	"fmt"
	"io"
	"strings"
)

// ID is an opaque identifier, never a filesystem path or an authorization grant.
type ID string

func (id ID) Validate() error {
	if len(id) == 0 || len(id) > 128 {
		return Invalid("id", "must contain 1 to 128 ASCII identifier characters")
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return Invalid("id", "contains invalid characters")
		}
	}
	return nil
}

// Version is an optimistic concurrency token. Persisted objects start at one.
type Version uint64

type ActorKind string

const (
	ActorOwner     ActorKind = "owner"
	ActorAssistant ActorKind = "assistant"
)

// Principal is supplied ONLY by auth or the trusted scheduler, never decoded
// from a client body or model output. Validate checks shape, not current access.
// Services must additionally check revocation, object ownership and policy.
type Principal struct {
	OwnerID     ID
	Kind        ActorKind
	SubjectID   ID
	SessionID   ID
	TokenID     ID
	ExecutionID ID
}

func (p Principal) Validate() error {
	if p.OwnerID.Validate() != nil || p.SubjectID.Validate() != nil {
		return NewError(Unauthenticated, "invalid identity")
	}
	switch p.Kind {
	case ActorOwner:
		if p.SubjectID != p.OwnerID || p.ExecutionID != "" || (p.SessionID == "") == (p.TokenID == "") {
			return NewError(Unauthenticated, "invalid owner identity")
		}
		if p.SessionID != "" {
			return p.SessionID.Validate()
		}
		return p.TokenID.Validate()
	case ActorAssistant:
		if p.SessionID != "" || p.TokenID != "" || p.ExecutionID.Validate() != nil {
			return NewError(Unauthenticated, "invalid assistant identity")
		}
	default:
		return NewError(Unauthenticated, "missing identity")
	}
	return nil
}
func (p Principal) RequireOwner() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Kind != ActorOwner {
		return NewError(Forbidden, "owner access required")
	}
	return nil
}

// Mutation scopes idempotency to the authenticated owner, operation kind and
// target. Reusing a key with a different payload must return Conflict.
// ExpectedVersion is required when updating an existing object.
type Mutation struct {
	IdempotencyKey  string
	ExpectedVersion Version
}

func (m Mutation) Validate(update bool) error {
	if len(m.IdempotencyKey) < 1 || len(m.IdempotencyKey) > 128 || strings.TrimSpace(m.IdempotencyKey) != m.IdempotencyKey {
		return Invalid("idempotency_key", "required, up to 128 characters")
	}
	for _, r := range m.IdempotencyKey {
		if r < 33 || r > 126 {
			return Invalid("idempotency_key", "must be printable ASCII without spaces")
		}
	}
	if update && m.ExpectedVersion == 0 {
		return Invalid("expected_version", "required for updates")
	}
	return nil
}

// PageRequest uses an opaque, owner- and query-bound cursor. A cursor never
// grants permission. Zero Limit means the default, negative limits are invalid.
type PageRequest struct {
	Cursor string
	Limit  int
}

func (p PageRequest) Normalize() (PageRequest, error) {
	if p.Limit < 0 || p.Limit > 200 || len(p.Cursor) > 2048 {
		return p, Invalid("page", "invalid cursor or limit")
	}
	if p.Limit == 0 {
		p.Limit = 50
	}
	return p, nil
}

type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Money uses millionths of the named currency, never floating point values.
type Money struct {
	Currency string
	Micros   int64
}

func (m Money) Validate() error {
	if m.Micros < 0 || len(m.Currency) != 3 {
		return Invalid("money", "non-negative amount and three-letter currency required")
	}
	for _, c := range m.Currency {
		if c < 'A' || c > 'Z' {
			return Invalid("currency", "must be uppercase ASCII")
		}
	}
	return nil
}

// Secret prevents accidental logging and serialization. Reveal is reserved for
// trusted authentication/credential adapters, not Agent-facing tools.
type Secret struct{ value string }

func NewSecret(value string) Secret            { return Secret{value: value} }
func (s Secret) Reveal() string                { return s.value }
func (s Secret) Empty() bool                   { return s.value == "" }
func (s Secret) String() string                { return "[REDACTED]" }
func (s Secret) GoString() string              { return "[REDACTED]" }
func (s Secret) Format(f fmt.State, verb rune) { _, _ = io.WriteString(f, "[REDACTED]") }
func (s Secret) MarshalJSON() ([]byte, error)  { return []byte(`"[REDACTED]"`), nil }
func (s Secret) MarshalText() ([]byte, error)  { return []byte("[REDACTED]"), nil }

// Object identifies a policy target without granting access to it.
type Object struct {
	Kind string
	ID   ID
}

// Execution binds all model/tool operations to trusted persisted execution state.
type Execution struct {
	Principal   Principal
	AssistantID ID
	TaskID      ID
	OperationID ID
}

func (e Execution) Validate() error {
	if err := e.Principal.Validate(); err != nil {
		return err
	}
	if e.Principal.Kind != ActorAssistant || e.AssistantID != e.Principal.SubjectID {
		return NewError(Forbidden, "assistant execution required")
	}
	if e.OperationID.Validate() != nil {
		return Invalid("operation_id", "required")
	}
	if e.TaskID != "" {
		return e.TaskID.Validate()
	}
	return nil
}
