package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// Compose authorized owner preferences and assistant workspace sources. File
// sources never come from the server checkout, home or implicit storage grants.
func (f *resourceFileTools) contextSources(ctx context.Context, request agent.ContextRequest) ([]agent.ContextDocument, error) {
	if err := (access{f.adapters.store}).execution(ctx, request.Execution); err != nil {
		return nil, err
	}
	reason, err := f.workspaceAvailability(ctx, request.Execution)
	if err != nil {
		return nil, err
	}
	workspace := workspaceDocument(reason)
	if reason != "" {
		prefs, err := ownerPreferencesContext(ctx, f.adapters.store, request.Execution.Principal.OwnerID)
		return []agent.ContextDocument{workspace, prefs}, err
	}

	root := platform.AssistantWorkspace(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, request.Execution.Principal.OwnerID, request.Execution.AssistantID)
	if err := platform.EnsureWorkspace(root); err != nil {
		return nil, err
	}
	sources, err := workspaceContext(ctx, root, request)
	if err != nil {
		return nil, err
	}
	if err := (access{f.adapters.store}).execution(ctx, request.Execution); err != nil {
		return nil, err
	}
	preferences, err := ownerPreferencesContext(ctx, f.adapters.store, request.Execution.Principal.OwnerID)
	if err != nil {
		return nil, err
	}
	global, err := scanRootContext(ctx, platform.UserGlobal(f.adapters.dataDir, request.Execution.Principal.OwnerID), request, ".", "/global")
	if err != nil {
		return nil, err
	}
	revisions, err := f.memoryFileRevisions(ctx, request)
	if err != nil {
		return nil, err
	}
	return append(append(append(global, sources...), workspace, preferences), revisions...), nil
}

type sourceScan struct {
	root        string
	bytes       int
	versions    map[string]string
	directories map[string]bool
}

// Pin directory revisions even when AGENTS.md/SKILL.md is absent, so a
// concurrent create/delete cannot silently become an incomplete snapshot.
func (s *sourceScan) directory(name string) (bool, error) {
	dir, err := openResourceDirectory(s.root, name)
	if core.CodeOf(err) == core.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer dir.Close()
	version, err := resourceFileVersion(dir)
	if err != nil {
		return false, err
	}
	s.versions[name], s.directories[name] = version, true
	return true, nil
}

func (s *sourceScan) read(ctx context.Context, name string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	file, err := openResourceFile(s.root, name)
	if core.CodeOf(err) == core.NotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	before, err := resourceFileVersion(file)
	if err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return "", false, core.NewError(core.Unavailable, "context source read failed")
	}
	s.bytes += len(data)
	if len(data) > 64<<10 || s.bytes > 512<<10 || !textFile(data) {
		return "", false, core.Invalid("context_source", "bounded UTF-8 context source required")
	}
	after, err := resourceFileVersion(file)
	if err != nil {
		return "", false, err
	}
	if before != after {
		return "", false, fileChanged()
	}
	s.versions[name] = after
	return string(data), true, nil
}
func sourceRevision(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func workspaceContext(ctx context.Context, root string, request agent.ContextRequest) ([]agent.ContextDocument, error) {
	return scanWorkspaceContext(ctx, root, request, ".")
}
func scanWorkspaceContext(ctx context.Context, root string, request agent.ContextRequest, start string) ([]agent.ContextDocument, error) {
	return scanRootContext(ctx, root, request, start, "/work")
}
func scanRootContext(ctx context.Context, root string, request agent.ContextRequest, start, prefix string) ([]agent.ContextDocument, error) {
	scan := &sourceScan{root: root, versions: map[string]string{}, directories: map[string]bool{}}
	documents := []agent.ContextDocument{}
	loaded := map[string]bool{}
	for _, d := range request.Previous {
		expected := request.SandboxID
		if expected == "" {
			expected = "local"
		}
		if strings.HasPrefix(d.Scope, "sandbox:") {
			if !strings.HasPrefix(d.Scope, "sandbox:"+string(expected)+":") {
				continue
			}
		} else if expected != "local" {
			continue
		}
		if d.Kind == "skill" && d.Loaded {
			loaded[d.Source] = true
		}
	}
	// Successful file_read replies activate a skill. Arbitrary model text, failed
	// calls and external resource reads cannot claim that a workspace skill loaded.
	calls := map[string]model.ToolCall{}
	for _, input := range request.Inputs {
		if input.Role == model.Assistant {
			for _, call := range input.Calls {
				calls[call.ID] = call
			}
		}
		if input.Role != model.Tool {
			continue
		}
		call, ok := calls[input.ReplyTo]
		if !ok || call.Name != "file_read" {
			continue
		}
		// Text file_read successes include Text (including an empty file).
		// Metadata-only and multimodal reads do not activate a text skill.
		// Tool errors now remain in history as structured failure envelopes;
		// those must not activate a skill, including on checkpoint recovery.
		var result struct {
			SandboxID core.ID
			Text      *string
			Error     json.RawMessage `json:"error"`
			OK        *bool           `json:"ok"`
		}
		if json.Unmarshal([]byte(input.Text), &result) != nil || result.Text == nil || len(result.Error) != 0 || (result.OK != nil && !*result.OK) {
			continue
		}
		expectedSandbox := request.SandboxID
		if expectedSandbox == "" {
			expectedSandbox = "local"
		}
		actualSandbox := result.SandboxID
		if actualSandbox == "" {
			actualSandbox = "local"
		}
		if actualSandbox != expectedSandbox {
			continue
		}
		var args struct {
			ResourceID string `json:"resource_id"`
			Path       string `json:"path"`
		}
		if json.Unmarshal(call.Arguments, &args) != nil || args.ResourceID != "" {
			continue
		}
		requested := args.Path
		if prefix == "/global" {
			if !strings.HasPrefix(requested, "/global/") {
				continue
			}
			requested = "/work/" + strings.TrimPrefix(requested, "/global/")
		}
		name, err := workspaceRelativePath(requested)
		if err == nil {
			loaded[path.Join(prefix, name)] = true
		}
	}
	// Read fixed instruction paths instead of recursively enumerating work files.
	if start == "." {
		directories, err := instructionDirectories(request, prefix)
		if err != nil {
			return nil, err
		}
		for _, directory := range directories {
			exists, err := scan.directory(directory)
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			name := path.Join(directory, "AGENTS.md")
			body, exists, err := scan.read(ctx, name)
			if err != nil {
				return nil, err
			}
			if exists {
				documents = append(documents, agent.ContextDocument{Kind: "project_instruction", Source: path.Join(prefix, name), Scope: path.Join(prefix, directory), Revision: sourceRevision(body), Content: body})
			}
		}
	}
	// Only the shallow skill catalog is enumerated. Ordinary files, dependency
	// trees, imports and runtime artifacts cannot consume its entry budget.
	dir, err := openResourceDirectory(root, ".agents/skills")
	if core.CodeOf(err) != core.NotFound {
		if err != nil {
			return nil, err
		}
		version, err := resourceFileVersion(dir)
		if err != nil {
			dir.Close()
			return nil, err
		}
		entries, err := dir.ReadDir(4097)
		dir.Close()
		if err != nil && err != io.EOF {
			return nil, core.NewError(core.Unavailable, "skill catalog listing failed")
		}
		if len(entries) > 4096 {
			return nil, core.Invalid("context_source", "skill catalog entry limit exceeded")
		}
		scan.versions[".agents/skills"], scan.directories[".agents/skills"] = version, true
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			exists, err := scan.directory(path.Join(".agents/skills", entry.Name()))
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fileChanged()
			}
			name := path.Join(".agents/skills", entry.Name(), "SKILL.md")
			body, exists, err := scan.read(ctx, name)
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			source := path.Join(prefix, name)
			content := skillDiscovery(entry.Name(), source, body)
			if loaded[source] {
				content = body
			}
			documents = append(documents, agent.ContextDocument{Kind: "skill", Source: source, Scope: prefix, Revision: sourceRevision(body), Content: content, Loaded: loaded[source]})
		}
	}

	// Detect edits/replacements during collection. Fail instead of reporting a
	// partial snapshot as removals. Retry at the next normal scheduler attempt.
	for name, version := range scan.versions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var file *os.File
		var err error
		if scan.directories[name] {
			file, err = openResourceDirectory(root, name)
		} else {
			file, err = openResourceFile(root, name)
		}
		if err != nil {
			return nil, err
		}
		current, err := resourceFileVersion(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		if current != version {
			return nil, fileChanged()
		}
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Source < documents[j].Source })
	return documents, nil
}

// Preserve frontmatter verbatim as discovery metadata instead of pretending to
// implement YAML. Skill bodies are injected only after an explicit file read.
func skillDiscovery(name, source, body string) string {
	metadata := ""
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	if strings.HasPrefix(normalized, "---\n") {
		if end := strings.Index(normalized[4:], "\n---"); end >= 0 && end <= 8192 {
			metadata = normalized[4 : 4+end]
		}
	}
	data, _ := json.Marshal(struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Frontmatter string `json:"frontmatter,omitempty"`
	}{name, source, metadata})
	return string(data)
}

func workspaceSkills(ctx context.Context, root string) ([]agent.SkillInfo, error) {
	sources, err := scanWorkspaceContext(ctx, root, agent.ContextRequest{}, ".agents/skills")
	if err != nil {
		return nil, err
	}
	skills := []agent.SkillInfo{}
	for _, d := range sources {
		if d.Kind != "skill" {
			continue
		}
		skills = append(skills, agent.SkillInfo{ID: d.Source, Name: path.Base(path.Dir(d.Source)), Description: "Workspace workflow: " + d.Source, Version: d.Revision, Enabled: true, RequiredTools: []string{"file_read"}, RequiredActions: []string{"read"}})
	}
	return skills, nil
}
