// Package resource owns resource identity, safe descriptors and protected provider bindings.
package resource

import (
	"context"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
)

type Kind string

const (
	Storage    Kind = "storage"
	Compute    Kind = "compute"
	Network    Kind = "network"
	Tool       Kind = "tool"
	Model      Kind = "model"
	Connection Kind = "connection"
)

type State string

const (
	Available    State = "available"
	Offline      State = "offline"
	Disconnected State = "disconnected"
)

// Resource is safe for authorized UI/Agent views; it has no secret or raw binding.
type Resource struct {
	SandboxID core.ID `json:",omitempty"`
	ID        core.ID
	OwnerID   core.ID
	Kind      Kind
	Name      string
	Provider  string
	// Actions declares this provider's supported assistant permissions.
	Actions []string
	State   State
	Version core.Version
}

// Binding is trusted-internal; endpoint, credential refs and filesystem roots
// must never be copied into Agent-visible descriptors or arbitrary request URLs.
type Binding struct {
	SandboxID       core.ID
	SandboxResource string
	// SandboxTarget names a deployment-configured network target. It is not a URL.
	SandboxTarget   string
	PricingProvider string

	ResourceID     core.ID
	Endpoint       string
	RootPath       string
	CredentialID   core.ID
	AllowedOrigins []string
	// Models contains optional manual overrides and discovered provider models.
	Models []ModelConfig
}

const MaxProviderModels = 4096

// ModelConfig is owner-supplied configuration for the fixed compatible model
// adapter. Prices are USD millionths per million tokens; explicit zero is free.
// A missing config never implies a zero-cost provider.
type ModelConfig struct {
	Name                 string
	ContextLimit         int // Zero means no manual override.
	ProviderContextLimit int `json:",omitempty"`
	OutputLimit          int // Zero means no manual override.
	ProviderOutputLimit  int `json:",omitempty"`
	SupportsTools        bool
	SupportsImages       bool
	SupportsAudio        bool
	// Nil pricing is unknown. An explicit zero tariff confirms free usage.
	ProviderPricing *ModelPricing `json:",omitempty"` // Discovered tariff; owner overrides remain in Pricing.
	Pricing         *ModelPricing
	Discovered      bool
}
type ModelPricing struct {
	AudioInputMicrosPerMillion  *int64 `json:",omitempty"`
	Currency                    string
	InputMicrosPerMillion       int64
	OutputMicrosPerMillion      int64
	CachedInputMicrosPerMillion int64
}
type Query struct {
	Kind Kind
	Name string
	Page core.PageRequest
}
type ConnectRequest struct {
	Mutation core.Mutation
	Kind     Kind
	Name     string
	Provider string
	Binding  Binding
}

// TestConnectionRequest is transient and never saved as a resource or receipt.
type TestConnectionRequest struct {
	Provider string
	Binding  Binding
	APIKey   core.Secret
}
type ConnectionTest struct{ ModelCount int }
type ProviderConnection struct {
	PricingProvider string

	Resource     Resource
	Endpoint     string
	CredentialID core.ID
}
type SaveProviderRequest struct {
	PricingProvider string

	Mutation     core.Mutation
	ID           core.ID
	Name         string
	Endpoint     string
	UseAPIKey    bool
	APIKey       core.Secret
	Verification auth.Verification
}
type UpdateRequest struct {
	Mutation core.Mutation
	ID       core.ID
	Name     string
}
type DisconnectRequest struct {
	Mutation core.Mutation
	ID       core.ID
}
type DeleteProviderRequest struct {
	Mutation     core.Mutation
	ID           core.ID
	Verification auth.Verification
}
type RefreshModelsRequest struct {
	Mutation core.Mutation
	ID       core.ID
}
type ConfigureModelRequest struct {
	Mutation core.Mutation
	ID       core.ID
	Model    ModelConfig
}
type Status struct {
	State      State
	ReasonCode string
	CheckedAt  time.Time
}

// Connection management is owner-only and uses fixed supported providers.
type Service interface {
	ProviderConnection(context.Context, core.Principal, core.ID) (ProviderConnection, error)
	SaveProvider(context.Context, core.Principal, SaveProviderRequest) (Resource, error)
	TestConnection(context.Context, core.Principal, TestConnectionRequest) (ConnectionTest, error)
	Connect(context.Context, core.Principal, ConnectRequest) (Resource, error)
	Get(context.Context, core.Principal, core.ID) (Resource, error)
	List(context.Context, core.Principal, Query) (core.Page[Resource], error)
	Update(context.Context, core.Principal, UpdateRequest) (Resource, error)
	Disconnect(context.Context, core.Principal, DisconnectRequest) error
	DeleteProvider(context.Context, core.Principal, DeleteProviderRequest) error
	Check(context.Context, core.Principal, core.ID) (Status, error)
	RefreshModels(context.Context, core.Principal, RefreshModelsRequest) (Resource, error)
	ConfigureModel(context.Context, core.Principal, ConfigureModelRequest) (Resource, error)
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Resource, error)
	List(context.Context, core.ID, Query) (core.Page[Resource], error)
	Insert(context.Context, Resource, Binding) error
	Save(context.Context, Resource, core.Version) error
	Binding(context.Context, core.ID, core.ID) (Binding, error)
	BindingForModel(context.Context, core.ID, core.ID) (Binding, error)
}
