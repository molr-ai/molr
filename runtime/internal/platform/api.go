// Package platform defines local process, instance-lock and network-enforcement boundaries.
package platform

import (
	"context"
	"io"
	"net/netip"
	"time"

	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
)

type GPUConfig struct {
	AllDevices bool     `json:"all_devices"`
	Enabled    bool     `json:"enabled"`
	Devices    []string `json:"devices"`
}
type GPUDevice struct {
	UtilizationPercent *float64
	MemoryUsedBytes    *int64
	ID, Name, Vendor   string
	MemoryBytes        int64
	Available          bool
	Reason             string
}
type Capabilities struct {
	FullAccessTerminal bool
	Terminal           bool
	TerminalReason     string `json:",omitempty"`
	FullAccess         bool
	OS                 string
	ProcessIsolation   bool
	FileIsolation      bool
	NetworkIsolation   bool
	Shell              bool
	ShellType          string
	ResourceLimits     bool
	NetworkBackend     string
	NetworkProtocols   []string
	DiskProtection     string
	Reason             string
}

// Mount and NetworkTarget are trusted, deployment-resolved bindings. They must
// never be decoded from a model's tool arguments.
type Mount struct {
	Source, Destination string
	ReadOnly            bool
}
type NetworkTarget struct {
	Protocols        []string
	Addresses        []string
	Ports            []string
	ExcludeAddresses []string
	ExcludePorts     []string
}
type Limits struct {
	AllGPUDevices      bool     `json:"-"`
	GPUDeviceIDs       []string `json:",omitempty"`
	NetworkBytes       int64
	NetworkConnections int
	MemoryBytes        int64
	Processes          int
	CPUPercent         int
	TempBytes          int64
	MaxFileBytes       int64
}

// Command is assembled by a fixed trusted tool, never decoded directly from
// model output. There is no implicit shell or inherited secret environment.
type NetworkAuthorization struct {
	GroupSource      string                     `json:",omitempty"`
	GroupConstraints *policy.NetworkConstraints `json:",omitempty"`
	Source           string
	GrantID          core.ID
	Constraints      *policy.NetworkConstraints
	ExpiresAt        *time.Time
}
type Command struct {
	// NetworkAccess is an explicit all-IP grant for the native Seatbelt backend.
	NetworkAccess bool
	// BrowserAssets is set only by the trusted browser task planner.
	BrowserAssets           bool
	BrowserProxy            *browser.Proxy `json:"-"`
	GPUDeviceIDs            []string
	FullAccess              bool
	GroupComputeConstraints *policy.ComputeConstraints
	WorkspaceConstraints    *policy.StorageConstraints
	ComputeQuota            bool
	NetworkAuthorizations   []NetworkAuthorization

	ForwardPorts []int
	TTY          bool
	// Managed permits no wall-clock deadline; authorization and resource limits still apply.
	Managed bool
	// Trusted output sinks persist the full streams before preview truncation.
	StdoutSink, StderrSink io.Writer
	// Script is accepted only by the mandatory sandbox runner.
	Script string
	// AuthorizationDeadline is the earliest relevant grant expiry, when any.
	AuthorizationDeadline time.Time
	Mounts                []Mount
	NetworkTargets        []NetworkTarget
	Limits                Limits
	Executable            string
	Args                  []string
	Directory             string
	Environment           map[string]string
	ReadRoots             []string
	WriteRoots            []string
	AllowedOrigins        []string
	Timeout               time.Duration
	MaxOutputBytes        int64
	RequireIsolation      bool
}
type Exit struct {
	// Last observed network diagnostic, separate from the command exit cause.
	NetworkReason   string
	Reason          string
	Code            int
	TimedOut        bool
	OutputTruncated bool
}

// Process streams bounded output; Wait reaps it. Linux cancellation uses the
// managed cgroup; native execution kills the process group on a best-effort basis.
// Failed isolation never falls back to unrestricted execution.
type Process interface {
	Stdout() io.Reader
	Stderr() io.Reader
	Wait(context.Context) (Exit, error)
	Cancel(context.Context) error
}
type Runner interface {
	Capabilities(context.Context) (Capabilities, error)
	Start(context.Context, core.Execution, Command) (Process, error)
}

// Lock is held for the full server lifetime and prevents another instance from
// opening the same data directory. Closing releases the lock.
type Lock interface{ Close() error }
type Locker interface {
	Acquire(context.Context, string) (Lock, error)
}

// Network validation is enforced against resolved and connected addresses,
// redirects and bound origins, not only an initial URL string.
type Destination struct {
	ResourceID        core.ID
	URL               string
	ResolvedAddresses []netip.Addr
}
type NetworkGuard interface {
	Check(context.Context, core.Execution, Destination) error
}

// ValidateForwardPorts bounds task-owned TCP listeners. Targets are always the
// execution's loopback; callers cannot select a host or another namespace.
func ValidateForwardPorts(ports []int) error {
	if len(ports) > 4 {
		return core.Invalid("forward_ports", "at most four TCP ports per task")
	}
	seen := map[int]bool{}
	for _, port := range ports {
		if port < 1 || port > 65535 || seen[port] {
			return core.Invalid("forward_ports", "unique ports in 1..65535 required")
		}
		seen[port] = true
	}
	return nil
}

// NetworkDiagnostic is an observed sandbox event, not a connectivity probe.
type NetworkDiagnostic struct {
	Reason string
	At     time.Time
}
