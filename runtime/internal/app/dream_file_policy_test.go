package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"testing"
)

func TestDreamUsesCommonFileExecutorWithScopedAccess(t *testing.T) {
	f, files, _ := fileFixture(t)
	target := tools.FileTarget{Path: "/work/.agents/memory/note.md"}
	_, err := files.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "prior observation"})
	need(t, err)
	d := f.b.Services.Dreams.(*dream.Coordinator)
	run, err := d.Request(t.Context(), f.owner, dream.Request{AssistantID: f.execution.AssistantID, Mutation: core.Mutation{IdempotencyKey: "dream"}})
	need(t, err)
	ev := event.AssistantEvent{ID: run.EventID, OwnerID: run.OwnerID, AssistantID: run.AssistantID, OperationID: run.ID, Kind: event.DreamDue}
	_, err = d.Prepare(t.Context(), f.execution.Principal, ev)
	need(t, err)
	ctx := dreamFileContext(t.Context(), d, f.execution.Principal, ev)
	executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), FileRead: files.read, FileWrite: files.write, FileMove: files.move, FileDelete: files.delete})
	call := func(name string, args any, id string) (tools.Result, error) {
		raw, _ := json.Marshal(args)
		e := f.execution
		e.OperationID = core.ID(id)
		return executor.Execute(ctx, tools.Call{Execution: e, Name: name, Arguments: raw})
	}
	for i, p := range []string{"/work/project.txt", "/global/MEMORY.md", "/work/.agents/memory/.forgotten/private.md"} {
		_, err = call("file_write", tools.FileWriteRequest{FileTarget: tools.FileTarget{Path: p}, Text: "denied"}, "denied-"+string(rune('a'+i)))
		if err == nil {
			t.Fatal("Dream escaped memory scope", p)
		}
	}
	response, err := call("file_read", tools.FileReadRequest{FileTarget: target}, "read")
	need(t, err)
	var read fileReadResult
	need(t, json.Unmarshal(response.Content, &read))
	_, err = call("file_write", tools.FileWriteRequest{FileTarget: target, Text: "updated observation", Overwrite: true, ExpectedVersion: read.Version}, "update")
	need(t, err)
	response, err = call("file_read", tools.FileReadRequest{FileTarget: target}, "reread")
	need(t, err)
	need(t, json.Unmarshal(response.Content, &read))
	response, err = call("file_move", tools.FileMoveRequest{FileTarget: target, ToPath: "/work/.agents/memory/topic/note.md", CreateParents: true, ExpectedVersion: read.Version}, "move")
	need(t, err)
	var moved struct{ Version string }
	need(t, json.Unmarshal(response.Content, &moved))
	target.Path = "/work/.agents/memory/topic/note.md"
	_, err = call("file_delete", tools.FileDeleteRequest{FileTarget: target, ExpectedVersion: moved.Version}, "delete")
	need(t, err)
	need(t, d.Commit(t.Context(), f.execution.Principal, ev, "Updated, moved and deleted the obsolete topic."))
	stored, err := d.GetRun(t.Context(), f.owner, run.ID)
	need(t, err)
	if len(stored.FileChanges) != 3 {
		t.Fatal(stored.FileChanges)
	}
	_, err = d.Undo(t.Context(), f.owner, dream.UndoRequest{ID: run.ID, Mutation: core.Mutation{IdempotencyKey: "undo"}})
	need(t, err)
	original := readFileResult(t, files, f.execution, tools.FileTarget{Path: "/work/.agents/memory/note.md"})
	if original.Text != "prior observation" {
		t.Fatal("common effects not undoable", original.Text)
	}
}
