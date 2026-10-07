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
	if _, err := groupReference(ctx, f.adapters.store, request.Execution.Principal.OwnerID, "sandbox", "local"); err != nil {
		if core.ReasonOf(err) == "group_resource_denied" {
			prefs, err := ownerPreferencesContext(ctx, f.adapters.store, request.Execution.Principal.OwnerID)
			return []agent.ContextDocument{prefs}, err
		}
		return nil, err
	}
	root := platform.AssistantWorkspace(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, request.Execution.Principal.OwnerID, request.Execution.AssistantID)
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
	return append(append(append(global, sources...), preferences), revisions...), nil
}

type sourceScan struct {
	root        string
	visited     int
	bytes       int
	versions    map[string]string
	directories map[string]bool
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
	var walk func(string, int) error
	walk = func(directory string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 {
			return core.Invalid("context_source", "workspace context scan depth exceeded")
		}
		dir, err := openResourceDirectory(root, directory)
		if core.CodeOf(err) == core.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		defer dir.Close()
		version, err := resourceFileVersion(dir)
		if err != nil {
			return err
		}
		// Bounded enumeration, including ignored entries, avoids unbounded reads.
		entries, err := dir.ReadDir(4097 - scan.visited)
		if err != nil && err != io.EOF {
			return core.NewError(core.Unavailable, "workspace context listing failed")
		}
		scan.visited += len(entries)
		if scan.visited > 4096 {
			return core.Invalid("context_source", "workspace context scan entry limit exceeded")
		}
		scan.versions[directory] = version
		scan.directories[directory] = true
		for _, entry := range entries {
			name := path.Join(directory, entry.Name())
			if entry.Name() == "AGENTS.md" {
				body, exists, err := scan.read(ctx, name)
				if err != nil {
					return err
				}
				if exists {
					documents = append(documents, agent.ContextDocument{Kind: "project_instruction", Source: path.Join(prefix, name), Scope: path.Join(prefix, directory), Revision: sourceRevision(body), Content: body})
				}
			}
			if directory == ".agents/skills" && entry.IsDir() {
				skillDir, err := openResourceDirectory(root, name)
				if err != nil {
					return err
				}
				skillVersion, err := resourceFileVersion(skillDir)
				skillDir.Close()
				if err != nil {
					return err
				}
				scan.versions[name], scan.directories[name] = skillVersion, true
				source := path.Join(name, "SKILL.md")
				body, exists, err := scan.read(ctx, source)
				if err != nil {
					return err
				}
				if exists {
					source = path.Join(prefix, source)
					content := skillDiscovery(entry.Name(), source, body)
					if loaded[source] {
						content = body
					}
					documents = append(documents, agent.ContextDocument{Kind: "skill", Source: source, Scope: prefix, Revision: sourceRevision(body), Content: content, Loaded: loaded[source]})
				}
				continue
			}
			if !entry.IsDir() {
				continue
			}
			switch entry.Name() {
			case ".git", "node_modules", ".cache", ".venv", "vendor", "dist", "build":
				continue
			}
			if directory == ".agents" && entry.Name() != "skills" {
				continue
			}
			if err := walk(name, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(start, 0); err != nil {
		return nil, err
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
