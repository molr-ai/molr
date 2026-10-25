package shellsession

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"runtime"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// The controller belongs to a browser window and authenticated session, never
// to a client-supplied assistant identity. Restarted tasks are not resumed.
type TerminalState struct {
	Control                    string
	Generation                 uint64
	Rows, Cols                 uint16
	NeedsRead                  bool
	ControllerIsCurrentSession bool   `json:",omitempty"`
	ControllerClientID         string `json:",omitempty"`
	session                    core.ID
	client                     string
	sequence                   uint64
	readExecution              core.ID
}

func terminalGeneration(t *TerminalState) uint64 {
	if t == nil {
		return 0
	}
	return t.Generation
}
func terminalView(t *TerminalState, p core.Principal) *TerminalState {
	if t == nil {
		return nil
	}
	copy := *t
	copy.ControllerIsCurrentSession = p.Kind == core.ActorOwner && p.SessionID != "" && t.session == p.SessionID
	if copy.ControllerIsCurrentSession {
		copy.ControllerClientID = t.client
	}
	return &copy
}
func shellNeedsReminder(t Task) bool {
	return t.Mode != "foreground" && (t.Terminal == nil || t.Terminal.Control == "assistant")
}

func (s *Manager) StartTerminal(ctx context.Context, p core.Principal, id core.ID, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if err := s.host.CheckAssistant(ctx, p, id); err != nil {
		return nil, err
	}
	var q struct {
		RequestID  core.ID `json:"request_id"`
		ClientID   string  `json:"client_id"`
		ComputeID  core.ID `json:"compute_id"`
		FullAccess bool    `json:"full_access"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 4096 || d.Decode(&q) != nil || q.RequestID.Validate() != nil || q.ComputeID.Validate() != nil || q.ClientID == "" || len(q.ClientID) > 128 || p.SessionID == "" {
		return nil, core.Invalid("terminal", "request, client and compute required")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, core.Invalid("terminal", "trailing arguments")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return nil, core.WithReason(core.NewError(core.FailedPrecondition, "interactive terminals unavailable"), "terminal_platform_unsupported")
	}
	// A request ID is retained by the UI until startup is known. Never replay a
	// shell launch when its response is lost.
	if previous, err := persistence.Load[Task](ctx, s.store, shellTasksBucket, persistence.Key(p.OwnerID, q.RequestID)); err == nil {
		if previous.AssistantID != id || previous.CreatedBy != "user" || previous.Terminal == nil {
			return nil, denied()
		}
		if previous.ComputeID != q.ComputeID || previous.FullAccess != q.FullAccess {
			return nil, core.NewError(core.Conflict, "terminal request already used with a different resource or execution mode")
		}
		return json.Marshal(map[string]any{"Task": previous})
	} else if !persistence.Missing(err) {
		return nil, err
	}
	e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: id, Kind: core.ActorAssistant, ExecutionID: randomOperation()}, AssistantID: id, OperationID: q.RequestID}
	cap := &Control{e: e, terminal: &TerminalState{Control: "user", Generation: 1, Rows: 24, Cols: 80, session: p.SessionID, client: q.ClientID}}
	cap.alive.Store(true)
	defer cap.alive.Store(false)
	if err := s.terminalAudit(ctx, p, q.RequestID, "terminal.start"); err != nil {
		return nil, err
	}
	script := TerminalScript("")
	if runtime.GOOS == "windows" {
		script = TerminalScript("powershell")
	}
	return s.Execute(context.WithValue(ctx, managedShellKey{}, cap), e, tools.ShellRequest{FullAccess: q.FullAccess, ComputeID: q.ComputeID, TTY: true, Script: script, NetworkMode: "auto"})
}
func (s *Manager) terminalAudit(ctx context.Context, p core.Principal, task core.ID, action string) error {
	return s.host.RecordAudit(ctx, audit.Record{ID: randomOperation(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: action, Target: core.Object{Kind: "operation", ID: task}, Outcome: audit.Succeeded, At: time.Now().UTC()})
}
func (s *Manager) terminalManage(ctx context.Context, p core.Principal, id core.ID, r *runningShell, q tools.ShellTaskRequest) (json.RawMessage, error) {
	// Serialize mutations and handoffs through the same gate. The version is
	// checked after waiting, including for assistant calls queued before takeover.
	r.terminalGate.Lock()
	defer r.terminalGate.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	state := r.task.Terminal
	control := r.scope.control
	running := r.task.State == "running"
	generation := state.Generation
	s.mu.Unlock()
	if !running || control == nil {
		return nil, core.NewError(core.FailedPrecondition, "terminal is starting or finished")
	}
	if q.Action == "read" {
		raw, err := control(ctx, q)
		if err != nil {
			return nil, err
		}
		var output struct {
			Output     string
			NextCursor int64
		}
		_ = json.Unmarshal(raw, &output)
		s.mu.Lock()
		if p.Kind == core.ActorAssistant && q.Generation == state.Generation && state.Control == "assistant" && output.NextCursor-q.Cursor < 16<<10 {
			state.NeedsRead = false
			state.readExecution = p.ExecutionID
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["Terminal"], _ = json.Marshal(terminalView(state, p))
		s.mu.Unlock()
		return json.Marshal(fields)
	}
	if p.Kind == core.ActorOwner {
		watch, err := s.identity.WatchPrincipal(p)
		if err != nil {
			return nil, err
		}
		defer watch.Close()
		if _, err = watch.Check(ctx); err != nil {
			return nil, err
		}
	}
	if q.Generation != generation {
		return nil, core.WithReason(core.NewError(core.Conflict, "terminal control changed; read current task state"), "terminal_control_changed")
	}
	if q.Action == "takeover" || q.Action == "release" {
		if err := p.RequireOwner(); err != nil {
			return nil, err
		}
		if p.SessionID == "" || q.ClientID == "" {
			return nil, core.Invalid("client_id", "authenticated window required")
		}
		if q.Action == "release" && (state.Control != "user" || state.session != p.SessionID || state.client != q.ClientID) {
			return nil, denied()
		}
		next := *state
		next.Generation++
		next.sequence = 0
		next.readExecution = ""
		next.NeedsRead = true
		next.Control = "assistant"
		next.session = ""
		next.client = ""
		if q.Action == "takeover" {
			next.Control = "user"
			next.session = p.SessionID
			next.client = q.ClientID
		}
		s.mu.Lock()
		updated := r.task
		updated.Terminal = &next
		err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := s.save(ctx, updated); err != nil {
				return err
			}
			if err := s.terminalAudit(ctx, p, r.task.ID, "terminal."+q.Action); err != nil {
				return err
			}
			if q.Action == "release" {
				details, _ := json.Marshal(map[string]any{"TerminalTaskID": r.task.ID, "Generation": next.Generation, "Notice": "The user returned this terminal. Read shell_task output with this generation through its latest page before continuing. The foreground program may still be running; takeover did not interrupt it. Do not replay old input."})
				return event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: randomOperation(), OwnerID: p.OwnerID, AssistantID: id, Kind: event.TerminalControlReturned, DeliveryMode: event.WhenIdle, Details: details, CreatedAt: time.Now().UTC()})
			}
			return nil
		})
		if err == nil {
			r.task = updated
			if shellNeedsReminder(updated) {
				s.armLocked(updated)
			} else {
				s.disarmLocked(p.OwnerID, id)
			}
			_ = refreshShellReminder(ctx, s.store, p.OwnerID, id, time.Now(), s.reminderDetailsLocked(p.OwnerID, id, time.Now()))
		}
		view := terminalView(r.task.Terminal, p)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"Terminal": view})
	}
	if p.Kind == core.ActorAssistant {
		if state.Control != "assistant" {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "The user controls this terminal. Read output or wait for control to be returned."), "terminal_user_controlled")
		}
		if state.NeedsRead || state.readExecution != "" && state.readExecution != p.ExecutionID {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Read the latest terminal output with the current generation before operating."), "terminal_read_required")
		}
	} else {
		if state.Control != "user" || state.session != p.SessionID || state.client != q.ClientID {
			return nil, core.WithReason(core.NewError(core.Conflict, "take control in this window before operating"), "terminal_control_changed")
		}
		if q.Sequence == 0 || q.Sequence <= state.sequence {
			return nil, core.NewError(core.Conflict, "stale terminal input")
		}
	}
	if q.Action != "write" && q.Action != "resize" && q.Action != "interrupt" && q.Action != "stop" {
		return nil, core.Invalid("action", "terminal action unavailable")
	}
	s.mu.Lock()
	state.sequence = q.Sequence
	s.mu.Unlock()
	if q.Action == "stop" {
		if err := s.terminalAudit(ctx, p, r.task.ID, "terminal.stop"); err != nil {
			return nil, err
		}
		r.cancel()
		return json.RawMessage(`{"Stopping":true}`), nil
	}
	raw, err := control(ctx, q)
	if err == nil && q.Action == "resize" {
		s.mu.Lock()
		state.Rows = q.Rows
		state.Cols = q.Cols
		s.mu.Unlock()
	}
	return raw, err
}

// Resolve omitted scripts against the execution node, not the client platform.
func TerminalScript(shell string) string {
	if shell == "powershell" {
		return " "
	}
	return "exec /bin/sh -i"
}
