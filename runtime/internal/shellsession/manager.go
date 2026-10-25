// Package shellsession owns durable command, terminal and browser sessions.
// The application supplies authorization and routing capabilities; sessions do
// not import the application composition or its local/remote backend internals.
package shellsession

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/maintenance"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/portforward"
	"time"
)

type Host interface {
	Execute(context.Context, core.Execution, tools.ShellRequest) (json.RawMessage, error)
	CheckExecution(context.Context, core.Execution) error
	CheckAssistant(context.Context, core.Principal, core.ID) error
	AuthorizeForward(context.Context, core.Principal) error
	ResolveCompute(context.Context, core.Principal, core.ID, core.ID) (core.ID, error)
	ConcurrencyLimit(context.Context, core.ID, core.ID) (int, error)
	DialForward(context.Context, core.Execution, core.ID, int) (portforward.Stream, error)
	CaptureLogs(context.Context, core.Principal, core.ID, core.ID) error
	Workspace(core.ID, core.ID) string
	BrowserSandboxAvailable() bool
	BrowserSupported(bool) bool
	AuthorizeSession(context.Context, core.Execution) error
	RecordAudit(context.Context, audit.Record) error
	ProxyCipher() cipher.AEAD
	VerifyProxy(context.Context, core.Principal, auth.Verification) error
}

type Options struct {
	Identity       auth.Service
	ForwardHTTP    config.HTTP
	Forward        config.PortForward
	Browser        config.Browser
	Maintenance    maintenance.Service
	ReadScreenshot func(context.Context, core.Execution, tools.FileReadRequest) (json.RawMessage, error)
}

func New(store persistence.Store, host Host, options Options) *Manager {
	return &Manager{store: store, host: host, identity: options.Identity, forwardHTTP: options.ForwardHTTP,
		forwardConfig: options.Forward, browserConfig: options.Browser, maintenance: options.Maintenance, browserReadShot: options.ReadScreenshot,
		forwardSlots: make(chan struct{}, 128), active: map[string]*runningShell{}, reminders: map[string]time.Time{}, foregroundWait: 60 * time.Second, executeCommand: host.Execute}
}
func shellKey(e core.Execution) string {
	return string(e.Principal.OwnerID) + "/" + string(e.OperationID)
}
func randomOperation() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID("sbread_" + hex.EncodeToString(b[:]))
}
func denied() error               { return core.NewError(core.Forbidden, "object access denied") }
func digestBytes(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }

func OutputPrefix(e core.Execution) string {
	return platform.WorkspaceStateDirectory + "/tool-shell-" + digestBytes([]byte(string(e.Principal.OwnerID) + "/" + string(e.OperationID)))[:32]
}
