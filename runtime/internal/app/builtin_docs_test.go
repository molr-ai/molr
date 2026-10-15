package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/textfiles"
)

func TestBuiltinDocsWithoutSandbox(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	files := &resourceFileTools{adapters: f.ad, router: &sandboxRouter{adapters: f.ad}}
	target := tools.FileTarget{Path: molrdocs.SkillPath}
	raw, err := files.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target})
	need(t, err)
	var read fileReadResult
	need(t, json.Unmarshal(raw, &read))
	if !strings.Contains(read.Text, "name: molr-docs") || read.Path != target.Path || read.Version != molrdocs.Revision {
		t.Fatal("wrong built-in document", read)
	}
	docs, err := files.contextSources(t.Context(), agent.ContextRequest{Execution: f.execution})
	need(t, err)
	if docs[0].Source != molrdocs.SkillPath || docs[0].Loaded || strings.Contains(docs[0].Content, "# Molr documentation") {
		t.Fatal("eager or missing built-in skill", docs)
	}
	caps, err := f.b.Services.Agent.Capabilities(t.Context(), f.owner, f.execution.AssistantID)
	need(t, err)
	if !caps.Skills.Supported || len(caps.Skills.Items) != 1 || caps.Skills.Items[0].ID != molrdocs.SkillPath {
		t.Fatal("missing built-in capability", caps.Skills)
	}
	for _, tool := range caps.Tools {
		if (tool.Name == "file_read" || tool.Name == "file_list" || tool.Name == "file_search") && !tool.Available {
			t.Fatal("built-in read tool unavailable", tool)
		}
	}
	raw, err = files.list(t.Context(), f.execution, tools.FileListRequest{FileTarget: tools.FileTarget{Path: "/molr"}})
	need(t, err)
	if !strings.Contains(string(raw), "/molr/.agents") {
		t.Fatal(string(raw))
	}
	raw, err = files.search(t.Context(), f.execution, tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: "/molr/.agents/skills/molr-docs/references/docs"}, Pattern: "助理", MaxResults: 3})
	need(t, err)
	var result struct{ textfiles.SearchResult }
	need(t, json.Unmarshal(raw, &result))
	if len(result.Matches) != 3 || !result.Truncated || result.Complete {
		t.Fatal(string(raw))
	}
	for _, m := range result.Matches {
		if !strings.HasPrefix(m.Path, "/molr/") || m.Version != molrdocs.Revision {
			t.Fatal(m)
		}
	}
	_, err = files.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/work/MEMORY.md"}})
	if core.CodeOf(err) != core.NotFound {
		t.Fatal("missing workspace file should be reported with commands disabled", err)
	}
	// Built-in documentation remains inline independently of command execution.
	raw, err = files.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/molr/.agents/skills/molr-docs/references/docs/dev/reference/http-api.md"}, Limit: 2000})
	need(t, err)
	if len(raw) > toolInlineLimit {
		t.Fatalf("document output requires workspace: %d", len(raw))
	}
	need(t, json.Unmarshal(raw, &read))
	if !read.Truncated || read.NextOffset <= 1 {
		t.Fatal("missing continuation", read)
	}
}

func TestBuiltinDocsRejectMutationsAndForeignPaths(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	files := &resourceFileTools{adapters: f.ad, router: &sandboxRouter{adapters: f.ad}}
	target := tools.FileTarget{Path: molrdocs.SkillPath}
	mutations := []func() error{
		func() error {
			_, e := files.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "changed", Overwrite: true})
			return e
		},
		func() error {
			_, e := files.edit(t.Context(), f.execution, tools.FileEditRequest{FileTarget: target, OldText: "Molr", NewText: "changed", ExpectedVersion: molrdocs.Revision})
			return e
		},
		func() error {
			_, e := files.delete(t.Context(), f.execution, tools.FileDeleteRequest{FileTarget: target, ExpectedVersion: molrdocs.Revision})
			return e
		},
		func() error {
			_, e := files.move(t.Context(), f.execution, tools.FileMoveRequest{FileTarget: target, ToPath: "/molr/other", ExpectedVersion: molrdocs.Revision})
			return e
		},
	}
	for _, mutate := range mutations {
		if err := mutate(); core.CodeOf(err) != core.Forbidden {
			t.Fatal("mutation not denied", err)
		}
	}
	for _, bad := range []tools.FileTarget{
		{Path: "/molr/../global/MEMORY.md"}, {Path: "/molr//.agents"}, {Path: molrdocs.SkillPath, SandboxID: "remote"}, {Path: molrdocs.SkillPath, FullAccess: true},
		{Path: "/molr/.agents/skills/molr-docs/references/internal-docs/secret.md"}, {Path: "/molr/.agents/skills/molr-docs/references/docs/en/user/README.md"},
		{Path: molrdocs.SkillPath, ResourceID: f.storage.ID},
	} {
		if _, err := files.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: bad}); err == nil {
			t.Fatal("accepted invalid target", bad)
		}
	}
	other := f.execution
	other.Principal.OwnerID = "another-owner"
	if _, err := files.read(t.Context(), other, tools.FileReadRequest{FileTarget: target}); err == nil {
		t.Fatal("accepted invalid execution identity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := files.search(ctx, f.execution, tools.FileSearchRequest{FileTarget: tools.FileTarget{Path: "/molr"}, Pattern: "Molr"}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestBuiltinSkillActivationAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name, args, result string
		loaded             bool
	}{
		{"read", `{"path":"` + molrdocs.SkillPath + `"}`, `{"SandboxID":"local","Text":"part"}`, true},
		{"metadata", `{"path":"` + molrdocs.SkillPath + `"}`, `{"SizeBytes":100}`, false},
		{"failure", `{"path":"` + molrdocs.SkillPath + `"}`, `{"Text":"part","error":{}}`, false},
		{"remote", `{"path":"` + molrdocs.SkillPath + `","sandbox_id":"remote"}`, `{"Text":"part"}`, false},
		{"external", `{"path":"` + molrdocs.SkillPath + `","resource_id":"other"}`, `{"Text":"part"}`, false},
		{"same name", `{"path":"/work/.agents/skills/molr-docs/SKILL.md"}`, `{"Text":"part"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := agent.ContextRequest{Inputs: []model.Input{{Role: model.Assistant, Calls: []model.ToolCall{{ID: "read", Name: "file_read", Arguments: json.RawMessage(tc.args)}}}, {Role: model.Tool, ReplyTo: "read", Text: tc.result}}}
			doc := builtinSkill(request)
			if doc.Loaded != tc.loaded {
				t.Fatal(doc)
			}
			if doc.Loaded {
				doc.Revision = "old-bundle"
				doc.Content = "old content"
				restored := builtinSkill(agent.ContextRequest{Previous: []agent.ContextDocument{doc}})
				if !restored.Loaded || restored.Revision != molrdocs.Revision || restored.Content != molrdocs.Skill {
					t.Fatal("stale restored skill", restored)
				}
			}
		})
	}
}
