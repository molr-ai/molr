package shellsession

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type watchControlKey struct{}

func watchControl(ctx context.Context) bool { v, _ := ctx.Value(watchControlKey{}).(bool); return v }

// StartWatcher is a trusted Runtime entry, not a model tool. Resource grants are
// checked by the same local/remote execution host as ordinary shell commands.
func (s *Manager) StartWatcher(ctx context.Context, e core.Execution, q tools.ShellRequest) (context.Context, error) {
	if err := s.host.CheckAssistant(ctx, e.Principal, e.AssistantID); err != nil {
		return nil, err
	}
	cap := &Control{e: e}
	cap.alive.Store(true)
	defer cap.alive.Store(false)
	launch := context.WithValue(ctx, managedShellKey{}, cap)
	q.ProtocolInput = true
	q.Background = true
	q.TTY = false
	q.FullAccess = false
	q.TimeoutSeconds = 0
	q.ForwardPorts = nil
	if _, err := s.Execute(launch, e, q); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active[shellKey(e)]
	if r == nil {
		return context.WithValue(ctx, watchControlKey{}, true), nil
	}
	return context.WithValue(context.WithValue(ctx, managedShellKey{}, r.scope), watchControlKey{}, true), nil
}

// WatcherState returns the durable state for the exact watcher execution.
func (s *Manager) WatcherState(ctx context.Context, e core.Execution) (Task, error) {
	t, err := persistence.Load[Task](ctx, s.store, shellTasksBucket, persistence.Key(e.Principal.OwnerID, e.OperationID))
	if err == nil && (t.Kind != "watcher" || t.AssistantID != e.AssistantID) {
		return Task{}, denied()
	}
	return t, err
}

// WatcherIO is restricted to the capability returned by StartWatcher.
func (s *Manager) WatcherIO(ctx context.Context, e core.Execution, q tools.ShellTaskRequest) (json.RawMessage, error) {
	if !watchControl(ctx) || q.TaskID != e.OperationID {
		return nil, denied()
	}
	return s.Manage(ctx, e.Principal, e.AssistantID, q)
}
