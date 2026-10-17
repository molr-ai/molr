// Package sandboxmanager defines a platform-neutral resource boundary. The same
// Backend is called in-process or through the authenticated standalone service.
package sandboxmanager

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

const ProtocolVersion = 9
const LeaseDuration = 15 * time.Second
const MaxMessage = 96 << 20
const MaxRequest = 32 << 20

type DirectoryRequest struct{ SourceID, Path string }
type Directory struct {
	Path     string `json:",omitempty"`
	ID, Name string
	ReadOnly bool
}
type Network struct {
	ID, Name string
	Target   platform.NetworkTarget
}
type Info struct {
	Build                  buildinfo.Info
	AllowDirectoryPaths    bool
	GPUs                   []platform.GPUDevice `json:",omitempty"`
	WorkspaceProjectQuotas bool
	ResourceQuotas         bool
	UserGroupQuotas        bool
	AuthorizationCeilings  platform.Limits
	PortForwarding         bool
	ShellSessions          bool
	Protocol               int
	Enabled                bool
	Storage                bool
	Capabilities           platform.Capabilities
	Directories            []Directory
	Networks               []Network
	Limits                 platform.Limits
	TimeoutSeconds         int
}

// Identity is a delegated execution identity from an authenticated Runtime, not
// an owner API principal. The service credential binds one trusted Runtime.
type Identity struct{ OwnerID, AssistantID, ExecutionID, OperationID core.ID }

func (i Identity) Execution() core.Execution {
	return core.Execution{Principal: core.Principal{Kind: core.ActorAssistant, OwnerID: i.OwnerID, SubjectID: i.AssistantID, ExecutionID: i.ExecutionID}, AssistantID: i.AssistantID, OperationID: i.OperationID}
}

type Scope struct {
	ResourceID  core.ID
	DirectoryID string
	Path        string
	Read, Write bool
}
type Request struct {
	FullAccess              bool
	ComputeConstraints      *policy.ComputeConstraints
	GroupComputeConstraints *policy.ComputeConstraints `json:",omitempty"`
	NetworkAuthorizations   []platform.NetworkAuthorization
	Identity                Identity
	Method                  string
	Arguments               json.RawMessage
	Scopes                  []Scope
	Networks                []string
	Compute                 bool
	// Epoch is advanced by Runtime when grants are changed or resources revoked.
	Epoch                       uint64
	Deadline                    time.Time
	TextRange                   bool
	Images, Audio, AudioPricing bool
	Protocol                    string
}
type Reply struct {
	State  string
	Result json.RawMessage
	Code   core.Code
	Reason core.Reason
}
type Backend interface {
	Info(context.Context) (Info, error)
	Call(context.Context, Request) (json.RawMessage, error)
	Cancel(context.Context, Identity) error
	Close() error
}
