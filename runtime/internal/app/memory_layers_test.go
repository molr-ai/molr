package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type memoryLayerAuthorizer struct{}

func (memoryLayerAuthorizer) Require(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}
func TestCoreContextInvalidatedByTopicEditAndForget(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "edit", true: "forget"}[remove], func(t *testing.T) {
			root := t.TempDir()
			db, err := sqlitestore.Open(filepath.Join(root, "db"))
			need(t, err)
			defer db.Close()
			files := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(context.Context, core.ID, memory.RootQuery) ([]memory.Root, error) {
				return []memory.Root{{Scope: memory.Assistant, AssistantID: "assistant", Directory: filepath.Join(root, "work")}, {Scope: memory.Global, Directory: filepath.Join(root, "global")}}, nil
			}})
			service := memory.NewService(db, memory.Options{Files: files, Authorizer: memoryLayerAuthorizer{}, CheckAssistant: func(context.Context, core.Principal, core.ID) error { return nil }})
			topic, err := files.Put(t.Context(), memory.Memory{Ref: memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: ".agents/memory/工作约定.md"}, OwnerID: "owner", State: memory.Active, Text: "汇报先写结论", Sources: []core.Object{{Kind: "message", ID: "source"}}}, "", nil)
			need(t, err)
			_, err = files.Put(t.Context(), memory.Memory{Ref: memory.Ref{Scope: memory.Assistant, AssistantID: "assistant", Path: "MEMORY.md"}, OwnerID: "owner", State: memory.Active, Text: "稳定认知：汇报先写结论", Dependencies: []memory.Dependency{{Ref: topic.Ref, Version: topic.Version}}}, "", nil)
			need(t, err)
			p := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
			request := agent.ContextRequest{Execution: core.Execution{Principal: p, AssistantID: "assistant"}}
			access := memoryAccess{memories: service}
			before, err := access.context(t.Context(), request)
			need(t, err)
			if len(before) != 1 || !strings.Contains(before[0].Content, "稳定认知") {
				t.Fatal(before)
			}
			if remove {
				need(t, files.Delete(t.Context(), topic, topic.Version, true, nil))
			} else {
				changed := topic
				changed.Text = "汇报先写风险"
				_, err = files.Put(t.Context(), changed, topic.Version, nil)
				need(t, err)
			}
			after, err := access.context(t.Context(), request)
			need(t, err)
			if len(after) != 1 || strings.Contains(after[0].Content, "汇报先写结论") || after[0].Revision == before[0].Revision {
				t.Fatal("stale core remained in context", after)
			}
		})
	}
}

func TestDreamMaintenanceAdmissionDoesNotRequireExecutionIdentity(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	_, err := f.b.Services.Memories.Create(t.Context(), f.owner, memory.CreateRequest{Ref: memory.Ref{Scope: memory.Assistant, AssistantID: f.execution.AssistantID, Path: ".agents/memory/约定.md"}, Text: "以后工作汇报先写结论", Mutation: memory.Mutation{IdempotencyKey: "topic"}})
	need(t, err)
	run, err := f.b.Services.Dreams.Request(t.Context(), f.owner, dream.Request{AssistantID: f.execution.AssistantID, Mutation: core.Mutation{IdempotencyKey: "maintenance"}})
	need(t, err)
	if !run.Maintenance || run.State != "queued" {
		t.Fatal(run)
	}
}
