package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"strings"
)

// ShellRequest contains resource references and explicit Full Access program invocations,
// never trusted host mounts or sandbox options.
type ShellRequest struct {
	Executable string   `json:"executable,omitempty"`
	Args       []string `json:"args,omitempty"`
	// ProtocolInput is reserved for trusted watcher launches, never accepted by the model tool.
	ProtocolInput   bool   `json:"protocol_input,omitempty"`
	ForwardProtocol string `json:"forward_protocol,omitempty"`
	// Browser is a trusted task launch descriptor; never accepted from Shell JSON.
	Browser        *BrowserLaunch `json:"-"`
	SandboxID      core.ID        `json:"sandbox_id,omitempty"`
	FullAccess     bool           `json:"full_access,omitempty"`
	ForwardPorts   []int          `json:"forward_ports,omitempty"`
	TTY            bool           `json:"tty,omitempty"`
	Background     bool           `json:"background,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	ComputeID      core.ID        `json:"compute_id"`
	Script         string         `json:"script"`
	Directory      string         `json:"directory,omitempty"`
	Files          []ShellFile    `json:"files,omitempty"`
	NetworkMode    string         `json:"network_mode,omitempty"`
	NetworkIDs     []core.ID      `json:"network_ids,omitempty"`
}
type ShellFile struct {
	ResourceID core.ID `json:"resource_id"`
	Path       string  `json:"path"`
	Write      bool    `json:"write"`
}

func parseShell(raw json.RawMessage) (ShellRequest, error) {
	var r ShellRequest
	if len(raw) > 128<<10 {
		return r, core.Invalid("shell", "arguments too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return r, core.Invalid("shell", "invalid shell request")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return r, core.Invalid("shell", "trailing arguments")
	}
	if r.Executable != "" {
		if !r.FullAccess || r.Script != "" {
			return r, core.Invalid("executable", "program execution requires full_access and excludes script")
		}
		if err := policy.ValidateProgramInvocation(r.Executable, r.Args); err != nil {
			return r, err
		}
	} else if len(r.Args) > 0 {
		return r, core.Invalid("args", "executable required")
	}
	if r.ProtocolInput {
		return r, core.Invalid("protocol_input", "reserved watcher input")
	}
	if r.NetworkMode != "" && r.NetworkMode != "auto" && r.NetworkMode != "none" {
		return r, core.Invalid("network_mode", "auto or none required")
	}
	if r.NetworkMode == "none" && len(r.NetworkIDs) > 0 {
		return r, core.Invalid("network_mode", "none cannot select networks")
	}
	if (r.TTY && r.Background) || r.TimeoutSeconds < 0 || r.TimeoutSeconds > 604800 {
		return r, core.Invalid("shell", "tty and background are exclusive; timeout_seconds must be 0..604800 (0 means no deadline)")
	}
	if r.SandboxID != "" && r.SandboxID.Validate() != nil {
		return r, core.Invalid("sandbox_id", "invalid sandbox ID")
	}
	if r.ComputeID.Validate() != nil || (!r.TTY && r.Script == "" && r.Executable == "") || len(r.Script) > 65536 || strings.ContainsRune(r.Script, 0) || len(r.Files) > 32 || len(r.NetworkIDs) > 32 {
		return r, core.Invalid("shell", "bounded script and compute resource required")
	}
	if r.ForwardProtocol != "" && r.ForwardProtocol != "http" {
		return r, core.Invalid("forward_protocol", "only authenticated http forwarding is available")
	}
	if err := platform.ValidateForwardPorts(r.ForwardPorts); err != nil {
		return r, err
	}
	for _, f := range r.Files {
		if f.ResourceID.Validate() != nil {
			return r, core.Invalid("files", "resource ID required")
		}
	}
	for _, id := range r.NetworkIDs {
		if id.Validate() != nil {
			return r, core.Invalid("network_ids", "resource ID required")
		}
	}
	return r, nil
}
