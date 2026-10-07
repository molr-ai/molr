// Package media holds provider-independent, durable references to file snapshots.
package media

import "molr.ai/runtime/internal/core"

// Part never serializes bytes into checkpoints, traces or tool text. Snapshot is
// an internal content digest, scoped to the execution owner and assistant.
type Part struct {
	FullAccess bool    `json:",omitempty"`
	SandboxID  core.ID `json:",omitempty"`
	Kind       string
	MIME       string
	Snapshot   string
	Scope      string
	ResourceID core.ID `json:",omitempty"`
	Path       string
	Version    string
	Detail     string   `json:",omitempty"`
	Timestamp  *float64 `json:",omitempty"`
	Data       []byte   `json:"-"`
}
