package app

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

const shellTasksBucket = "sandbox.shell_tasks"
const shellTaskLimit = 10
const shellReminderInterval = 5 * time.Minute

type shellTask struct {
	FullAccess               bool
	Forwards                 []shellForward `json:",omitempty"`
	ID, OwnerID, AssistantID core.ID
	SandboxID                core.ID
	Mode, State, Script      string
	StartedAt                time.Time
	FinishedAt               *time.Time `json:",omitempty"`
	StdoutPath, StderrPath   string
	Result                   json.RawMessage `json:",omitempty"`
	Failure                  string          `json:",omitempty"`
	CompletionSent           bool
}
type managedShellKey struct{}

// Unexported, process-local capability; never derived from tool arguments. It
// authorizes only the original execution while the manager retains its slot.
type managedShell struct {
	e       core.Execution
	alive   atomic.Bool
	control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)
	routed  func(core.ID) error
	started func(string, string, func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) error
}

func managedShellFor(ctx context.Context, e core.Execution) *managedShell {
	v, _ := ctx.Value(managedShellKey{}).(*managedShell)
	if v != nil && v.alive.Load() && v.e == e {
		return v
	}
	return nil
}
func managedPrincipal(ctx context.Context, p core.Principal) bool {
	v, _ := ctx.Value(managedShellKey{}).(*managedShell)
	return v != nil && v.alive.Load() && v.e.Principal == p
}

type runningShell struct {
	forwards []*taskForward
	task     shellTask
	cancel   context.CancelFunc
	done     chan struct{}
	ready    chan struct{}
	scope    *managedShell
	result   json.RawMessage
	err      error
}
type shellTasks struct {
	forwardConfig  config.PortForward
	forwardSlots   chan struct{}
	store          persistence.Store
	router         *sandboxRouter
	mu             sync.Mutex
	active         map[string]*runningShell
	reminders      map[string]time.Time
	stopped        bool
	wg             sync.WaitGroup
	foregroundWait time.Duration
	executeCommand func(context.Context, core.Execution, tools.ShellRequest) (json.RawMessage, error)
}

func newShellTasks(store persistence.Store, router *sandboxRouter) *shellTasks {
	return &shellTasks{store: store, router: router, forwardSlots: make(chan struct{}, 128), active: map[string]*runningShell{}, reminders: map[string]time.Time{}, foregroundWait: 60 * time.Second, executeCommand: router.shellExecute}
}
func (s *shellTasks) save(ctx context.Context, t shellTask) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var previous shellTask
		err := tx.Get(ctx, shellTasksBucket, persistence.Key(t.OwnerID, t.ID), &previous)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err := tx.Put(ctx, shellTasksBucket, persistence.Key(t.OwnerID, t.ID), t); err != nil {
			return err
		}
		if previous.State == t.State {
			return nil
		}
		return realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: randomOperation(), OwnerID: t.OwnerID, Topic: "shell.tasks", Object: core.Object{Kind: "assistant", ID: t.AssistantID}, CreatedAt: time.Now().UTC()})
	})
}
func (s *shellTasks) execute(ctx context.Context, e core.Execution, req tools.ShellRequest) (json.RawMessage, error) {
	if err := (access{s.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	if len(req.ForwardPorts) > 0 {
		g, err := groupPolicy(ctx, s.store, e.Principal.OwnerID)
		if err != nil {
			return nil, err
		}
		if !g.Administrator {
			return nil, core.WithReason(core.NewError(core.Forbidden, "public port forwarding requires administrator"), "group_port_forward_unavailable")
		}
	}
	if err := platform.ValidateForwardPorts(req.ForwardPorts); err != nil {
		return nil, err
	}
	resource, err := resource.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, req.ComputeID)
	if err != nil {
		return nil, err
	}
	sandboxID := targetSandbox(req.SandboxID)
	if sandboxOf(resource) != sandboxID {
		return nil, core.Invalid("sandbox_id", "explicit sandbox_id must match the compute resource node")
	}
	ref, err := groupReference(ctx, s.store, e.Principal.OwnerID, "sandbox", sandboxID)
	if err != nil {
		return nil, err
	}
	key := shellKey(e)
	s.mu.Lock()
	if !req.FullAccess && ref != nil && ref.Quota.Concurrent > 0 {
		active := 0
		for _, r := range s.active {
			if r.task.OwnerID == e.Principal.OwnerID && r.task.SandboxID == sandboxID && !r.task.FullAccess {
				active++
			}
		}
		if active >= ref.Quota.Concurrent {
			s.mu.Unlock()
			return nil, groupQuotaExceeded()
		}
	}

	if s.stopped {
		s.mu.Unlock()
		return nil, core.NewError(core.Unavailable, "shell manager stopped")
	}
	count := 0
	for _, r := range s.active {
		if r.task.OwnerID == e.Principal.OwnerID && r.task.AssistantID == e.AssistantID && !r.task.FullAccess {
			count++
		}
	}
	if !req.FullAccess && count >= shellTaskLimit {
		s.mu.Unlock()
		return nil, core.NewError(core.RateLimited, "assistant has 10 running executions; stop or wait for an existing task")
	}
	if _, ok := s.active[key]; ok {
		s.mu.Unlock()
		return nil, core.NewError(core.Conflict, "shell task already exists")
	}
	// Reject replay after restart even if operation acknowledgement was lost.
	if _, err := persistence.Load[shellTask](ctx, s.store, shellTasksBucket, persistence.Key(e.Principal.OwnerID, e.OperationID)); err == nil {
		s.mu.Unlock()
		return nil, core.NewError(core.Conflict, "shell task already recorded; inspect it instead of replaying")
	} else if !persistence.Missing(err) {
		s.mu.Unlock()
		return nil, err
	}
	mode := "foreground"
	if req.Background {
		mode = "background"
	}
	if req.TTY {
		mode = "tty"
	}
	cap := &managedShell{e: e}
	cap.alive.Store(true)
	run, cancel := context.WithCancel(context.WithValue(context.WithoutCancel(ctx), managedShellKey{}, cap))
	r := &runningShell{task: shellTask{FullAccess: req.FullAccess, SandboxID: sandboxID, ID: e.OperationID, OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, Mode: mode, State: "running", Script: req.Script, StartedAt: time.Now().UTC()}, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), scope: cap}
	r.task.StdoutPath = "/work/" + shellOutputPrefix(e) + "-stdout.txt"
	r.task.StderrPath = "/work/" + shellOutputPrefix(e) + "-stderr.txt"
	cap.routed = func(id core.ID) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r.task.SandboxID = id
		return s.save(context.Background(), r.task)
	}
	cap.started = func(out, stderr string, control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if out != "" {
			r.task.StdoutPath, r.task.StderrPath = out, stderr
		}
		cap.control = control
		err := s.save(context.Background(), r.task)
		if err != nil {
			r.cancel()
			return err
		}
		select {
		case <-r.ready:
		default:
			close(r.ready)
		}
		return nil
	}
	if err := s.prepareForwards(run, r, req.ForwardPorts); err != nil {
		cancel()
		cap.alive.Store(false)
		s.mu.Unlock()
		return nil, err
	}
	if err := s.save(ctx, r.task); err != nil {
		cancel()
		for _, f := range r.forwards {
			f.close()
		}
		cap.alive.Store(false)
		s.mu.Unlock()
		return nil, err
	}
	s.active[key] = r
	s.armLocked(r.task)
	s.wg.Add(1)
	for i, f := range r.forwards {
		s.wg.Add(1)
		go s.serveForward(run, e, r, i, f)
	}
	go s.run(run, e, req, r)
	s.mu.Unlock()
	if mode == "tty" {
		select {
		case <-r.ready:
			return s.receipt(r)
		case <-r.done:
			if r.err != nil {
				return nil, r.err
			}
			return s.receipt(r)
		case <-ctx.Done():
			cancel()
			<-r.done
			return nil, ctx.Err()
		}
	}
	if mode == "background" {
		return s.receipt(r)
	}
	timer := time.NewTimer(s.foregroundWait)
	defer timer.Stop()
	select {
	case <-r.done:
		return r.result, r.err
	case <-ctx.Done():
		cancel()
		<-r.done
		return nil, ctx.Err()
	case <-timer.C:
		s.mu.Lock()
		select {
		case <-r.done:
			s.mu.Unlock()
			return r.result, r.err
		default:
		}
		r.task.Mode = "background"
		err := s.save(context.Background(), r.task)
		s.armLocked(r.task)
		s.mu.Unlock()
		if err != nil {
			cancel()
			<-r.done
			return nil, err
		}
		return s.receipt(r)
	}
}
func (s *shellTasks) receipt(r *runningShell) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	note := "Background completion/failure will arrive as an event; no polling is needed. One aggregate running reminder arrives every 5 minutes for this assistant."
	if r.task.Mode == "tty" {
		note = "TTY session opened. Use shell_task to read/write/interrupt/stop. No completion event; one aggregate running reminder arrives every 5 minutes while sessions or background tasks remain."
	}
	return json.Marshal(struct {
		Task   shellTask
		Notice string
	}{r.task, note})
}
func (s *shellTasks) run(ctx context.Context, e core.Execution, req tools.ShellRequest, r *runningShell) {
	defer s.wg.Done()
	defer r.cancel()
	result, err := s.executeCommand(ctx, e, req)
	for _, f := range r.forwards {
		f.close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range r.task.Forwards {
		r.task.Forwards[i].State = "closed"
	}
	r.scope.alive.Store(false)
	if err == nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(result, &fields) == nil && fields != nil {
			fields["TaskID"], _ = json.Marshal(r.task.ID)
			result, _ = json.Marshal(fields)
		}
	}
	r.result, r.err = result, err
	r.task.Result = result
	finished := time.Now().UTC()
	r.task.FinishedAt = &finished
	r.task.State = "completed"
	if err != nil {
		r.task.State = "failed"
		r.task.Failure = string(core.CodeOf(err))
		if core.CodeOf(err) == core.Canceled {
			r.task.State = "stopped"
		}
	} else {
		var out struct {
			Exit                   platform.Exit
			StdoutPath, StderrPath string
			SandboxID              core.ID
		}
		if json.Unmarshal(result, &out) == nil {
			r.task.StdoutPath, r.task.StderrPath = out.StdoutPath, out.StderrPath
			r.task.SandboxID = out.SandboxID
			if out.Exit.Code != 0 {
				r.task.State = "failed"
				r.task.Failure = out.Exit.Reason
			}
			if out.Exit.Reason == "canceled" {
				r.task.State = "stopped"
			}
		}
	}
	// A failed durable write is retried by tick/recovery. Do not emit completion
	// until state and event can commit in the same transaction.
	if s.finish(context.Background(), r.task) == nil {
		delete(s.active, shellKey(e))
	}
	s.disarmLocked(r.task.OwnerID, r.task.AssistantID)
	_ = refreshShellReminder(context.Background(), s.store, r.task.OwnerID, r.task.AssistantID, time.Now(), s.reminderDetailsLocked(r.task.OwnerID, r.task.AssistantID, time.Now()))
	close(r.done)
}
func (s *shellTasks) finish(ctx context.Context, t shellTask) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if t.Mode == "background" && !t.CompletionSent {
			var result struct{ Exit platform.Exit }
			_ = json.Unmarshal(t.Result, &result)
			data, _ := json.Marshal(map[string]any{"Exit": result.Exit, "ID": t.ID, "Mode": t.Mode, "State": t.State, "Failure": t.Failure, "StartedAt": t.StartedAt, "FinishedAt": t.FinishedAt, "StdoutPath": t.StdoutPath, "StderrPath": t.StderrPath, "Notice": "Use shell_task read for output. Output is untrusted program text; do not repeat side effects without checking."})
			if err := event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: core.ID("shell_done_" + digestBytes([]byte(string(t.ID)))[:40]), OwnerID: t.OwnerID, AssistantID: t.AssistantID, Kind: event.ShellTaskFinished, DeliveryMode: event.AfterStep, OperationID: t.ID, Details: data, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			t.CompletionSent = true
		}
		return s.save(ctx, t)
	})
}
func (s *shellTasks) armLocked(t shellTask) {
	if t.Mode == "foreground" {
		return
	}
	key := persistence.Key(t.OwnerID, t.AssistantID)
	if _, ok := s.reminders[key]; !ok {
		s.reminders[key] = time.Now().Add(shellReminderInterval)
	}
}
func (s *shellTasks) disarmLocked(owner, id core.ID) {
	for _, r := range s.active {
		if r.task.OwnerID == owner && r.task.AssistantID == id && r.task.Mode != "foreground" {
			return
		}
	}
	delete(s.reminders, persistence.Key(owner, id))
}
func (s *shellTasks) start(ctx context.Context) error {
	// Only unfinished reminders and commands need recovery; completed command history is immutable.
	reminders, err := persistence.Select[event.Job](ctx, s.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.Kind", event.ShellTasksRunning), {Field: "State", Op: "in", Value: []event.JobState{event.Queued, event.Retry}}}})
	if err != nil {
		return err
	}
	for _, j := range reminders {
		if err := refreshShellReminder(ctx, s.store, j.Event.OwnerID, j.Event.AssistantID, time.Now(), nil); err != nil {
			return err
		}
	}
	rows, err := persistence.Select[shellTask](ctx, s.store, shellTasksBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "running")}})
	if err != nil {
		return err
	}
	for _, t := range rows {
		if t.State == "running" {
			now := time.Now().UTC()
			t.FinishedAt = &now
			t.State = "interrupted"
			for i := range t.Forwards {
				t.Forwards[i].State = "closed"
			}
			t.Failure = "runtime_restarted"
			if err := s.finish(ctx, t); err != nil {
				return err
			}
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_ = s.tick(ctx, now)
			}
		}
	}()
	return nil
}
func (s *shellTasks) tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, r := range s.active {
		if r.task.State != "running" {
			if err := s.finish(ctx, r.task); err != nil {
				return err
			}
			delete(s.active, key)
			s.disarmLocked(r.task.OwnerID, r.task.AssistantID)
		}
	}
	for key, due := range s.reminders {
		if now.Before(due) {
			continue
		}
		var owner, id core.ID
		for _, r := range s.active {
			if persistence.Key(r.task.OwnerID, r.task.AssistantID) == key {
				owner, id = r.task.OwnerID, r.task.AssistantID
				break
			}
		}
		data := s.reminderDetailsLocked(owner, id, now)
		if len(data) == 0 {
			delete(s.reminders, key)
			continue
		}
		if err := appendShellReminder(ctx, s.store, owner, id, now, data); err != nil {
			return err
		}
		s.reminders[key] = now.Add(shellReminderInterval)
	}
	return nil
}
func (s *shellTasks) close() {
	s.mu.Lock()
	s.stopped = true
	for _, r := range s.active {
		r.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *shellTasks) tool(ctx context.Context, e core.Execution, q tools.ShellTaskRequest) (json.RawMessage, error) {
	if err := (access{s.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	return s.manage(ctx, e.Principal, e.AssistantID, q)
}
func (s *shellTasks) manage(ctx context.Context, p core.Principal, id core.ID, q tools.ShellTaskRequest) (json.RawMessage, error) {
	if err := (access{s.store}).assistant(ctx, p, id); err != nil {
		return nil, err
	}
	if q.Action == "list" {
		scope := []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", id)}
		var rows []shellTask
		err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
			running, err := persistence.Select[shellTask](ctx, s.store, shellTasksBucket, persistence.Query{Where: append(append([]persistence.Filter{}, scope...), persistence.Eq("State", "running")), OrderBy: "StartedAtKey", Descending: true})
			if err != nil {
				return err
			}
			history, err := persistence.Select[shellTask](ctx, s.store, shellTasksBucket, persistence.Query{Where: append(scope, persistence.Filter{Field: "State", Op: "!=", Value: "running"}), OrderBy: "StartedAtKey", Descending: true, Limit: 50})
			if err != nil {
				return err
			}
			rows = append(running, history...)
			return nil
		})
		if err != nil {
			return nil, err
		}
		items := []shellTask{}
		for _, t := range rows {
			if t.OwnerID == p.OwnerID && t.AssistantID == id {
				if len(t.Result) > 0 {
					var meta map[string]json.RawMessage
					if json.Unmarshal(t.Result, &meta) == nil {
						delete(meta, "Stdout")
						delete(meta, "Stderr")
						t.Result, _ = json.Marshal(meta)
					}
				}
				items = append(items, t)
			}
		}

		return json.Marshal(map[string]any{"Items": items, "Limit": shellTaskLimit, "LimitAppliesTo": "isolated"})
	}
	if q.Action == "stop_all" {
		s.mu.Lock()
		for _, r := range s.active {
			if r.task.OwnerID == p.OwnerID && r.task.AssistantID == id {
				r.cancel()
			}
		}
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"Stopping": true})
	}
	t, err := persistence.Load[shellTask](ctx, s.store, shellTasksBucket, persistence.Key(p.OwnerID, q.TaskID))
	if err != nil {
		return nil, err
	}
	if t.AssistantID != id || t.OwnerID != p.OwnerID {
		return nil, denied()
	}
	s.mu.Lock()
	r := s.active[string(p.OwnerID)+"/"+string(q.TaskID)]
	var control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)
	if r != nil {
		control = r.scope.control
	}
	s.mu.Unlock()
	if q.Action == "stop" {
		if r != nil {
			r.cancel()
		}
		return json.Marshal(map[string]bool{"Stopping": r != nil})
	}
	if control != nil {
		return control(ctx, q)
	}
	if q.Action == "read" && t.State != "running" {
		var result struct{ OutputComplete bool }
		_ = json.Unmarshal(t.Result, &result)
		var captureErr error
		if !result.OutputComplete && t.SandboxID != "" && t.SandboxID != "local" {
			target, err := persistence.Load[struct {
				SandboxID core.ID
				Identity  sm.Identity
			}](ctx, s.store, "sandbox.operation_targets", persistence.Key(p.OwnerID, t.ID))
			if err == nil {
				captureErr = s.router.mirrorShellLogs(ctx, target.Identity.Execution(), t.SandboxID)
			} else {
				captureErr = err
			}
		}
		root := platform.AssistantWorkspace(s.router.adapters.dataDir, s.router.adapters.storageConfig.WorkRoot, p.OwnerID, id)
		raw, err := readShellOutput(root, t.StdoutPath, t.StderrPath, q)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["OutputComplete"], _ = json.Marshal(result.OutputComplete)
		// Retained local output is stable even when capture was truncated by quota.
		fields["Finished"], _ = json.Marshal(captureErr == nil)
		if captureErr != nil {
			fields["CaptureReason"], _ = json.Marshal(string(core.CodeOf(captureErr)))
		}
		fields["SandboxID"] = json.RawMessage(`"local"`)
		return json.Marshal(fields)
	}
	return nil, core.NewError(core.FailedPrecondition, "task is starting or no longer running")
}
func readShellOutput(root, out, stderr string, q tools.ShellTaskRequest) (json.RawMessage, error) {
	path := out
	if q.Stream == "stderr" {
		path = stderr
	}
	if !strings.HasPrefix(path, "/work/"+platform.WorkspaceStateDirectory+"/tool-shell-") {
		return nil, core.NewError(core.NotFound, "output not ready")
	}
	f, err := openResourceFile(root, strings.TrimPrefix(path, "/work/"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 16<<10)
	n, err := f.ReadAt(buf, q.Cursor)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return json.Marshal(map[string]any{"Output": string(buf[:n]), "NextCursor": q.Cursor + int64(n), "Path": path})
}

func (s *shellTasks) reminderDetailsLocked(owner, id core.ID, now time.Time) json.RawMessage {
	items := []map[string]any{}
	for _, r := range s.active {
		t := r.task
		if t.OwnerID == owner && t.AssistantID == id && t.Mode != "foreground" && t.State == "running" {
			script := t.Script
			if len(script) > 512 {
				script = script[:512]
			}
			items = append(items, map[string]any{"ID": t.ID, "Mode": t.Mode, "State": t.State, "ElapsedSeconds": int64(now.Sub(t.StartedAt).Seconds()), "Script": script})
		}
	}
	if len(items) == 0 {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"Tasks": items, "Notice": "Background tasks are still running; TTY sessions are still open. No polling needed for background completion."})
	return data
}
