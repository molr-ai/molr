// Package model defines authorized model access and durable receipts for fixed providers.
package model

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
)

type Model struct {
	ID                    core.ID
	Name                  string
	ResourceID            core.ID
	SupportsTools         bool
	SupportsImages        bool
	SupportsAudio         bool
	ContextLimit          int // Explicit user override; zero means unset.
	ProviderContextLimit  int
	EffectiveContextLimit int
	ContextLimitSource    string
	OutputLimit           int // Explicit user override; zero means unset.
	ProviderOutputLimit   int
	EffectiveOutputLimit  int
	OutputLimitSource     string
	Pricing               *Pricing
	PricingInfo           *PricingInfo
}
type PricingInfo struct {
	Source                             string
	Automatic                          *Pricing
	RuleID, Version, Period, SourceURL string
	CalendarKnown                      bool
}

type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
)

// Protocol identifies a built-in wire adapter, not a dynamically registered plugin.
type Protocol string

const (
	OpenAIChat   Protocol = "openai-chat-completions"
	DeepSeekChat Protocol = "deepseek-chat-completions"
)

// StateSource binds opaque provider state to the exact backend that issued it.
// ModelName and Endpoint also protect against editing an existing resource.
type StateSource struct {
	Protocol   Protocol
	ResourceID core.ID
	ModelID    core.ID
	ModelName  string
	Endpoint   string
}

// ProviderState retains the complete original assistant message, including
// unknown extensions. Only its owning adapter selects fields to send; retaining
// a field does not authorize forwarding it. The Loop persists it opaquely.
type ProviderState struct {
	Source  StateSource
	Payload json.RawMessage
}

// Input is constructed by the Loop; it is NOT a conversation history object.
type Input struct {
	// ToolOperationID is trusted recovery metadata, never sent on the wire.
	ToolOperationID core.ID `json:",omitempty"`
	// ReadMessageIDs is trusted delivery metadata, never encoded by wire adapters.
	ReadMessageIDs []core.ID `json:",omitempty"`
	// RuntimeContext marks a replaceable private notification; never encoded on the wire.
	RuntimeContext bool `json:",omitempty"`
	Role           Role
	Text           string
	ProviderState  *ProviderState `json:",omitempty"`
	Media          []media.Part   `json:",omitempty"`
	FileIDs        []core.ID
	Calls          []ToolCall
	ReplyTo        string
}
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}
type Request struct {
	LogicalID       core.ID
	Attempt         int
	Execution       core.Execution
	ModelID         core.ID
	Inputs          []Input
	Tools           []ToolDefinition
	MaxOutputTokens int
	ReservationID   core.ID
}
type Usage struct {
	AudioInputTokens int64
	AudioUsageKnown  bool `json:",omitempty"`
	InputTokens      int64
	OutputTokens     int64
	CachedTokens     int64
}

// Timing is measured at the provider transport, never from replayed Loop deltas.
type Timing struct {
	RequestMs    float64
	FirstTokenMs *float64
	GenerationMs *float64
}
type Receipt struct {
	Failure      *Failure `json:",omitempty"`
	FinishReason string   `json:",omitempty"` // Provider terminal reason, retained with billing evidence.
	ModelID      core.ID
	ModelName    string
	Timing       *Timing `json:",omitempty"`
	Pricing      *Pricing
	PricingInfo  *PricingInfo

	ID                core.ID
	OperationID       core.ID
	ProviderRequestID string
	Usage             Usage
	Charged           *core.Money
	OutcomeKnown      bool
	CreatedAt         time.Time
}
type DeltaKind string

const (
	TextDelta          DeltaKind = "text"
	ProviderStateDelta DeltaKind = "provider_state"
	ToolCallDelta      DeltaKind = "tool_call"
	Completed          DeltaKind = "completed"
)

type Delta struct {
	ProviderState *ProviderState
	Kind          DeltaKind
	Text          string
	ToolCall      *ToolCall
	Receipt       *Receipt
}

// Stream.Next returns io.EOF only after a terminal result; failures remain
// errors and may require reconciliation. Close cancels local consumption but
// does not prove the provider stopped or incurred no cost. Receipts persist
// independently of consumers draining the stream; no credential escapes here.
type Stream interface {
	Next(context.Context) (Delta, error)
	Close() error
}

// Client checks execution, model-resource authorization and reservation on
// every request. No arbitrary provider URL or API key is accepted from a Loop.
type Catalog interface {
	List(context.Context, core.Principal) ([]Model, error)
}
type Client interface {
	Catalog
	Generate(context.Context, Request) (Stream, error)
	// Receipt looks up the local receipt ID, never an operation or provider ID.
	// The receipt must belong to execution's owner and operation.
	Receipt(ctx context.Context, execution core.Execution, receiptID core.ID) (Receipt, error)
	// Reconcile refreshes evidence for that same local receipt ID. It must not
	// dispatch Generate again or interpret receiptID as an operation ID.
	Reconcile(ctx context.Context, execution core.Execution, receiptID core.ID) (Receipt, error)
}
