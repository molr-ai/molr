package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"path"
	"strings"
)

func dreamFileContext(ctx context.Context, d *dream.Coordinator, p core.Principal, event event.AssistantEvent) context.Context {
	guard := &filePolicy{}
	guard.Check = func(ctx context.Context, e core.Execution, t tools.FileTarget, actions ...policy.Action) error {
		if e.Principal != p || e.AssistantID != event.AssistantID {
			return core.NewError(core.Forbidden, "dream execution mismatch")
		}
		write := false
		for _, a := range actions {
			write = write || a == policy.Write
		}
		if t.FullAccess || t.ResourceID != "" || (t.SandboxID != "" && t.SandboxID != "local") {
			return core.NewError(core.Forbidden, "dream files are restricted to local memory")
		}
		canonical := t.Path
		if !strings.HasPrefix(canonical, "/") {
			canonical = "/work/" + canonical
		}
		if !write && path.Clean(canonical) == canonical && strings.HasPrefix(canonical, "/work/"+platform.WorkspaceStateDirectory+"/tool-result-") && strings.HasSuffix(canonical, ".json") {
			return d.CheckFileAccess(ctx, p, event, false)
		}
		ref, err := dream.FileRef(p, canonical, false)
		if err != nil {
			ref, err = dream.FileRef(p, canonical, true)
		}
		if err != nil {
			return err
		}
		if write && ref.Scope != memory.Assistant {
			return core.NewError(core.Forbidden, "shared memory is read-only")
		}
		return d.CheckFileAccess(ctx, p, event, write)
	}
	guard.Read = func(ctx context.Context, e core.Execution, t tools.FileTarget) (json.RawMessage, error) {
		ref, err := dream.FileRef(p, t.Path, false)
		if err != nil {
			return nil, nil
		}
		diagnostics, err := d.RememberRead(ctx, p, event, ref)
		if err != nil || len(diagnostics) == 0 {
			return nil, err
		}
		return json.Marshal(diagnostics)
	}
	guard.Before = func(ctx context.Context, e core.Execution, kind string, t tools.FileTarget, dest *tools.FileTarget, version string, after []byte) (func() error, error) {
		ref, err := dream.FileRef(p, t.Path, false)
		if err != nil {
			return nil, err
		}
		var to *memory.Ref
		if dest != nil {
			v, err := dream.FileRef(p, dest.Path, false)
			if err != nil {
				return nil, err
			}
			to = &v
		}
		return d.BeforeFileChange(ctx, p, event, e.OperationID, kind, ref, to, version, after)
	}
	return context.WithValue(ctx, filePolicyKey{}, guard)
}
