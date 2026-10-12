package app

import (
	"context"
	"encoding/json"
	"io"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type profileTools struct {
	assistants assistant.Service
	workspace  *resourceFileTools
}

func (p *profileTools) context(ctx context.Context, e core.Execution) (agent.ContextDocument, error) {
	if err := (access{p.workspace.adapters.store}).execution(ctx, e); err != nil {
		return agent.ContextDocument{}, err
	}
	a, err := p.assistants.Get(ctx, e.Principal, e.AssistantID)
	if err != nil {
		return agent.ContextDocument{}, err
	}
	raw, err := json.Marshal(a.Profile())
	return agent.ContextDocument{Kind: "profile", Source: "profile", Scope: "assistant", Revision: digestBytes(raw), Content: string(raw)}, err
}

func (p *profileTools) update(ctx context.Context, e core.Execution, r tools.ProfileUpdateRequest) (json.RawMessage, error) {
	if err := (access{p.workspace.adapters.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	if r.Name != nil {
		if _, err := assistant.NormalizeName(*r.Name); err != nil {
			return nil, err
		}
	}
	req := assistant.SelfProfileRequest{Mutation: core.Mutation{IdempotencyKey: string(e.OperationID), ExpectedVersion: r.ExpectedVersion}, Name: r.Name, Status: r.Status}
	if r.Avatar != nil {
		value := ""
		switch r.Avatar.Type {
		case "preset":
			value = "preset:" + r.Avatar.ID
		case "file":
			data, err := p.image(ctx, e, r.Avatar.FileTarget)
			if err != nil {
				return nil, err
			}
			value, err = assistant.AvatarFromImage(data)
			if err != nil {
				return nil, err
			}
		case "clear":
		default:
			return nil, core.Invalid("avatar.type", "unknown avatar type")
		}
		req.Avatar = &value
	}
	var result assistant.ProfileResult
	err := p.workspace.adapters.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := (access{p.workspace.adapters.store}).execution(ctx, e); err != nil {
			return err
		}
		var err error
		result, err = p.assistants.UpdateSelfProfile(ctx, e, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (p *profileTools) image(ctx context.Context, e core.Execution, target tools.FileTarget) ([]byte, error) {
	f := p.workspace
	if targetSandbox(target.SandboxID) != "local" {
		return nil, core.Invalid("sandbox_id", "download remote images locally first")
	}
	root, name, err := f.resolve(ctx, e, target, policy.Read)
	if err != nil {
		return nil, err
	}
	file, err := openResourceFile(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, assistant.AvatarImageLimit+1))
	if err != nil {
		return nil, core.NewError(core.Unavailable, "avatar image read failed")
	}
	if len(data) > assistant.AvatarImageLimit {
		return nil, core.Invalid("avatar", "image exceeds 10 MiB")
	}
	after, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, fileChanged()
	}
	if _, _, err = f.resolve(ctx, e, target, policy.Read); err != nil {
		return nil, err
	}
	return data, nil
}
