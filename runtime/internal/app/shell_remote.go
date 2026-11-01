package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/shellsession"
	"path/filepath"
	"time"
)

type shellRemoteOutput struct {
	changes                *shellOutputBudget
	Identity               sm.Identity
	StdoutPath, StderrPath string
}
type shellRemoteControl struct {
	WaitRevision           *uint64 `json:",omitempty"`
	Target                 sm.Identity
	Request                tools.ShellTaskRequest
	StdoutPath, StderrPath string
}

func (b *remoteSandboxBackend) shellControl(ctx context.Context, q sm.Request) (json.RawMessage, error) {
	var r shellRemoteControl
	if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
		return nil, err
	}
	if r.Target.OwnerID != q.Identity.OwnerID || r.Target.AssistantID != q.Identity.AssistantID {
		return nil, denied()
	}
	if r.Target.Execution().Validate() != nil {
		return nil, denied()
	}
	raw, _ := json.Marshal(r.Request)
	request, err := tools.ParseShellTask(raw)
	if err != nil {
		return nil, err
	}
	if (q.Method == "shell_read" || q.Method == "shell_observe") && request.Action != "read" {
		return nil, core.Invalid("action", "read-only method required")
	}
	if request.Action == "read" {
		started := false
		var revision uint64
		if v, ok := b.shellOutputs.Load(shellKey(r.Target.Execution())); ok {
			started = true
			o := v.(shellRemoteOutput)
			if o.Identity != r.Target {
				return nil, denied()
			}
			r.StdoutPath, r.StderrPath = o.StdoutPath, o.StderrPath
			if o.changes != nil {
				revision, err = o.changes.observe(ctx, r.WaitRevision)
				if err != nil {
					return nil, err
				}
			}
		}
		root := b.operationRoot(r.Target)
		out, err := shellsession.ReadOutput(root, r.StdoutPath, r.StderrPath, request)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(out, &fields); err != nil {
			return nil, err
		}
		fields["Started"], _ = json.Marshal(started)
		fields["OutputRevision"], _ = json.Marshal(revision)
		return json.Marshal(fields)
	}
	if request.Action != "write" && request.Action != "interrupt" && request.Action != "resize" {
		return nil, core.Invalid("action", "terminal action required")
	}
	data := request.Data
	if request.Action == "interrupt" {
		data = "\x03"
	}
	err = b.runner.Terminal(ctx, r.Target.Execution(), data, request.Rows, request.Cols)
	return json.RawMessage(`{"Accepted":true}`), err
}
func (s *sandboxRouter) remoteShellControl(ctx context.Context, e core.Execution, id core.ID, q tools.ShellTaskRequest) (json.RawMessage, error) {

	c, err := s.connection(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, e.Principal, c)
	if err != nil {
		return nil, err
	}
	epoch, err := s.epoch(ctx, e.Principal.OwnerID)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(shellRemoteControl{WaitRevision: q.WaitOutput, Target: identity(e), Request: q, StdoutPath: "/work/" + shellsession.OutputPrefix(e) + "-stdout.txt", StderrPath: "/work/" + shellsession.OutputPrefix(e) + "-stderr.txt"})
	i := identity(e)
	i.OperationID = randomOperation()
	method := "shell_control"
	if q.Action == "read" {
		method = "shell_read"
		if q.WaitOutput != nil {
			method = "shell_observe"
		}
	}
	data, err := client.Execute(ctx, sm.Request{Identity: i, Method: method, Arguments: raw, Epoch: epoch, Deadline: time.Now().Add(15 * time.Second)}, func(context.Context) error { return nil })
	if err == nil && q.Action == "read" {
		err = s.mirrorShellLogs(ctx, e, id)
	}
	return data, err
}

// Reads keep mirroring logs after the task's start has been recorded.
func observeRemoteShell(ctx context.Context, scope *shellsession.Control, taskID core.ID, gpuIDs []string, control func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error)) {
	started := false
	var revision *uint64
	backoff := 100 * time.Millisecond
	for {
		raw, err := control(ctx, tools.ShellTaskRequest{Action: "read", TaskID: taskID, WaitOutput: revision})
		if err == nil {
			var status struct {
				Started        bool
				OutputRevision uint64
			}
			if json.Unmarshal(raw, &status) == nil && status.Started {
				if !started {
					started = scope.Started("", "", gpuIDs, control) == nil
				}
				if started {
					if revision == nil || *revision != status.OutputRevision {
						if scope.OutputChanged != nil {
							scope.OutputChanged()
						}
					}
					value := status.OutputRevision
					revision = &value
					backoff = 100 * time.Millisecond
					continue
				}
			}
		}
		// Only startup/transient failures retry; healthy output is a remote wait.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

// Operation spools are internal recovery evidence, never assistant workspaces.
func (b *remoteSandboxBackend) operationRoot(i sm.Identity) string {
	return filepath.Join(b.cfg.DataDir, "operations", digestBytes([]byte(string(i.OwnerID)+"/"+string(i.AssistantID)+"/"+string(i.ExecutionID)+"/"+string(i.OperationID))))
}
