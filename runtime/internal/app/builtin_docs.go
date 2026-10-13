package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/policy"
)

const builtinDocsRoot = "builtin:molr"

func builtinTarget(t tools.FileTarget) bool {
	return t.ResourceID == "" && (t.Path == molrdocs.Root || strings.HasPrefix(t.Path, molrdocs.Root+"/"))
}

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
	return builtinDocsRoot, name, nil
}

type readableFile interface {
	fs.File
	io.Seeker
}

func openTextSource(root, name string) (readableFile, error) {
	if root != builtinDocsRoot {
		return openResourceFile(root, name)
	}
	f, err := molrdocs.FS.Open(name)
	if err != nil {
		return nil, builtinFileError(err)
	}
	if r, ok := f.(readableFile); ok {
		return r, nil
	}
	f.Close()
	return nil, core.Invalid("path", "text file required")
}
func textSourceVersion(f fs.File) (string, error) {
	if file, ok := f.(*os.File); ok {
		return resourceFileVersion(file)
	}
	return molrdocs.Revision, nil
}
func openDirectorySource(root, name string) (fs.ReadDirFile, error) {
	if root != builtinDocsRoot {
		return openResourceDirectory(root, name)
	}
	f, err := molrdocs.FS.Open(name)
	if err != nil {
		return nil, builtinFileError(err)
	}
	if d, ok := f.(fs.ReadDirFile); ok {
		return d, nil
	}
	f.Close()
	return nil, core.Invalid("path", "directory required")
}
func builtinFileError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return core.NewError(core.NotFound, "built-in document not found")
	}
	if errors.Is(err, fs.ErrInvalid) {
		return core.Invalid("path", "invalid built-in document path")
	}
	return err
}
func (f *resourceFileTools) readBuiltin(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	if r.Detail != "" || r.StartSeconds != nil || r.EndSeconds != nil || r.MaxFrames != 0 {
		return nil, core.Invalid("options", "media options cannot be used with documentation")
	}
	if !r.MetadataOnly {
		return f.readText(ctx, e, r)
	}
	file, err := openTextSource(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return json.Marshal(mediaReadResult{SandboxID: "local", Path: r.Path, Version: molrdocs.Revision, MediaType: "text/plain; charset=utf-8", SizeBytes: info.Size()})
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
