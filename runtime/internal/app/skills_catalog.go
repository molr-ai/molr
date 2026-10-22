package app

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

type catalogSkill struct {
	Path        string
	Name        string
	Description string
	Source      string
	AssistantID core.ID
	Version     string
}
type skillAssistant struct {
	ID   core.ID
	Name string
}
type skillsCatalog struct {
	Items      []catalogSkill
	Assistants []skillAssistant
}

// Catalog inspection reads bounded local sources without activating agent context.
func inspectSkills(ctx context.Context, ad adapters, p core.Principal, id core.ID, requested string) (any, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if requested != "" {
		if requested == molrdocs.SkillPath && id == "" {
			return map[string]string{"Text": molrdocs.Skill, "Version": molrdocs.Revision}, nil
		}
		prefix, root := "/global", platform.UserGlobal(ad.dataDir, p.OwnerID)
		if id != "" {
			if err := (access{ad.store}).assistant(ctx, p, id); err != nil {
				return nil, err
			}
			prefix, root = "/work", platform.AssistantWorkspace(ad.dataDir, ad.storageConfig.WorkRoot, p.OwnerID, id)
		}
		relative := strings.TrimPrefix(requested, prefix+"/")
		parts := strings.Split(relative, "/")
		if !strings.HasPrefix(requested, prefix+"/") || len(parts) != 4 || parts[0] != ".agents" || parts[1] != "skills" || parts[3] != "SKILL.md" || parts[2] == "" || path.Clean(requested) != requested || strings.ContainsAny(requested, "\\\x00") {
			return nil, core.Invalid("path", "canonical skill path required")
		}
		if _, err := groupReference(ctx, ad.store, p.OwnerID, "sandbox", "local"); err != nil {
			return nil, err
		}
		scan := sourceScan{root: root, versions: map[string]string{}}
		body, exists, err := scan.read(ctx, relative)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, core.NewError(core.NotFound, "skill not found")
		}
		return map[string]string{"Text": body, "Version": sourceRevision(body)}, nil
	}
	if id != "" {
		return nil, core.Invalid("assistant_id", "assistant_id requires a skill path")
	}
	out := skillsCatalog{Items: []catalogSkill{{Path: molrdocs.SkillPath, Name: "molr-docs", Description: "Molr documentation", Source: "builtin", Version: molrdocs.Revision}}, Assistants: []skillAssistant{}}
	assistants, err := persistence.Select[assistant.Assistant](ctx, ad.store, "business.assistants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("State", assistant.Active)}, OrderBy: "ID"})
	if err != nil {
		return nil, err
	}
	for _, a := range assistants {
		out.Assistants = append(out.Assistants, skillAssistant{ID: a.ID, Name: a.Name})
	}
	if _, err := groupReference(ctx, ad.store, p.OwnerID, "sandbox", "local"); err != nil {
		if core.CodeOf(err) == core.Forbidden {
			return out, nil
		}
		return nil, err
	}
	add := func(root, prefix, source string, assistantID core.ID) error {
		documents, err := scanRootContext(ctx, root, agent.ContextRequest{}, ".agents/skills", prefix)
		if err != nil {
			return err
		}
		for _, d := range documents {
			var discovery struct{ Frontmatter string }
			_ = json.Unmarshal([]byte(d.Content), &discovery)
			var meta struct {
				Name        string
				Description string
			}
			// Metadata is presentation only; invalid YAML falls back to the directory name.
			if yaml.Unmarshal([]byte(discovery.Frontmatter), &meta) != nil {
				meta.Name, meta.Description = "", ""
			}
			if strings.TrimSpace(meta.Name) == "" {
				meta.Name = path.Base(path.Dir(d.Source))
			}
			out.Items = append(out.Items, catalogSkill{Path: d.Source, Name: meta.Name, Description: meta.Description, Source: source, AssistantID: assistantID, Version: d.Revision})
			if len(out.Items) > 4096 {
				return core.Invalid("skills", "skill catalog entry limit exceeded")
			}
		}
		return nil
	}
	if err := add(platform.UserGlobal(ad.dataDir, p.OwnerID), "/global", "shared", ""); err != nil {
		return nil, err
	}
	for _, a := range assistants {
		if err := add(platform.AssistantWorkspace(ad.dataDir, ad.storageConfig.WorkRoot, p.OwnerID, a.ID), "/work", "assistant", a.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}
