package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/filetools"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/policy"
	"path"
	"strings"
)

func (f *resourceFileTools) resolveBuiltin(ctx context.Context, e core.Execution, t tools.FileTarget, actions ...policy.Action) (string, string, error) {
	if err := (access{f.adapters.store}).execution(ctx, e); err != nil {
		return "", "", err
	}
	if targetSandbox(t.SandboxID) != "local" || t.FullAccess {
		return "", "", core.Invalid("path", "built-in documentation requires local file tools without full_access")
	}
	for _, action := range actions {
		if action != policy.Read {
			return "", "", core.NewError(core.Forbidden, "built-in documentation is read-only")
		}
	}
	if path.Clean(t.Path) != t.Path || strings.ContainsAny(t.Path, "\\\x00") {
		return "", "", core.Invalid("path", "canonical built-in path required")
	}
	name := strings.TrimPrefix(strings.TrimPrefix(t.Path, molrdocs.Root), "/")
	if name == "" {
		name = "."
	}
	return filetools.BuiltinRoot, name, nil
}

func builtinSkill(request agent.ContextRequest) agent.ContextDocument {
	loaded := loadedSkills(request, molrdocs.Root)[molrdocs.SkillPath]
	content := skillDiscovery("molr-docs", molrdocs.SkillPath, molrdocs.Skill)
	if loaded {
		content = molrdocs.Skill
	}
	return agent.ContextDocument{Kind: "skill", Source: molrdocs.SkillPath, Scope: molrdocs.Root, Revision: molrdocs.Revision, Content: content, Loaded: loaded}
}
func builtinSkillInfo() agent.SkillInfo {
	return agent.SkillInfo{ID: molrdocs.SkillPath, Name: "molr-docs", Description: "Built-in, read-only Molr documentation · /molr", Version: molrdocs.Revision, Enabled: true, RequiredTools: []string{"file_read", "file_search"}, RequiredActions: []string{"read"}}
}
