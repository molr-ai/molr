package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/shellsession"
	"path/filepath"
	"time"
)

type shellLogChunk struct {
	Data       []byte
	NextCursor int64
	More       bool
}

func (b *remoteSandboxBackend) shellLog(ctx context.Context, q sm.Request) (json.RawMessage, error) {
	var r shellRemoteControl
	if err := decodeSandboxArgs(q.Arguments, &r); err != nil {
		return nil, err
	}
	if r.Target.OwnerID != q.Identity.OwnerID || r.Target.AssistantID != q.Identity.AssistantID || r.Target.Execution().Validate() != nil || r.Request.Cursor < 0 || (r.Request.Stream != "stdout" && r.Request.Stream != "stderr") {
		return nil, denied()
	}
	name := shellsession.OutputPrefix(r.Target.Execution()) + "-" + r.Request.Stream + ".txt"
	f, err := openResourceFile(b.operationRoot(r.Target), name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, 64<<10)
	n, err := f.ReadAt(data, r.Request.Cursor)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return json.Marshal(shellLogChunk{data[:n], r.Request.Cursor + int64(n), n == len(data)})
}
func (s *sandboxRouter) mirrorShellLogs(ctx context.Context, e core.Execution, id core.ID) error {
	c, err := s.connection(ctx, e.Principal, id)
	if err != nil {
		return err
	}
	client, err := s.client(ctx, e.Principal, c)
	if err != nil {
		return err
	}
	epoch, err := s.epoch(ctx, e.Principal.OwnerID)
	if err != nil {
		return err
	}
	root := platform.AssistantWorkspace(s.adapters.dataDir, s.adapters.storageConfig.WorkRoot, e.Principal.OwnerID, e.AssistantID)
	if err = platform.EnsureWorkspace(root); err != nil {
		return err
	}
	limits, err := workspaceConstraints(ctx, s.adapters.store, e.Principal.OwnerID, e.AssistantID, "local")
	if err != nil {
		return err
	}
	maxBytes := int64(64 << 20)
	if limits != nil {
		maxBytes = min(maxBytes, limits.MaxFileBytes)
	}
	if err = applyWorkspaceQuota(root, limits, s.adapters.storageConfig.WorkspaceProjectQuotas); err != nil {
		return err
	}
	for _, stream := range []string{"stdout", "stderr"} {
		name := shellsession.OutputPrefix(e) + "-" + stream + ".txt"
		unlock, err := s.files.Lock(ctx, filepath.Join(root, name))
		if err != nil {
			return err
		}
		err = func() error {
			offset := int64(0)
			f, err := openResourceFile(root, name)
			if err == nil {
				st, e := f.Stat()
				f.Close()
				if e != nil {
					return e
				}
				offset = st.Size()
			} else if core.CodeOf(err) != core.NotFound {
				return err
			}
			for {
				raw, _ := json.Marshal(shellRemoteControl{Target: identity(e), Request: tools.ShellTaskRequest{Stream: stream, Cursor: offset}})
				i := identity(e)
				i.OperationID = randomOperation()
				data, err := client.Execute(ctx, sm.Request{Identity: i, Epoch: epoch, Method: "shell_output", Arguments: raw, Deadline: time.Now().Add(15 * time.Second)}, func(ctx context.Context) error { _, err := s.connection(ctx, e.Principal, id); return err })
				if err != nil {
					return err
				}
				var chunk shellLogChunk
				if err = json.Unmarshal(data, &chunk); err != nil {
					return err
				}
				if len(chunk.Data) > 64<<10 || chunk.NextCursor != offset+int64(len(chunk.Data)) || chunk.NextCursor > maxBytes {
					return core.Invalid("output", "invalid remote output chunk")
				}
				if err = appendOutputChunk(filepath.Join(root, platform.WorkspaceStateDirectory), filepath.Base(name), offset, chunk.Data); err != nil {
					return err
				}
				offset = chunk.NextCursor
				if !chunk.More {
					return nil
				}
			}
		}()
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *sandboxRouter) archiveRemoteShellResult(ctx context.Context, e core.Execution, id core.ID, raw json.RawMessage) json.RawMessage {
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return raw
	}
	if err := s.mirrorShellLogs(ctx, e, id); err != nil {
		result["OutputComplete"] = json.RawMessage("false")
		result["OutputNote"], _ = json.Marshal("Remote command outcome is recorded; local output capture is incomplete. Read the shell task to retry log retrieval, never rerun the command merely to retrieve output.")
	}
	result["SandboxID"], _ = json.Marshal(id)
	result["OutputSandboxID"] = json.RawMessage(`"local"`)
	result["StdoutPath"], _ = json.Marshal("/work/" + shellsession.OutputPrefix(e) + "-stdout.txt")
	result["StderrPath"], _ = json.Marshal("/work/" + shellsession.OutputPrefix(e) + "-stderr.txt")
	out, _ := json.Marshal(result)
	return out
}
