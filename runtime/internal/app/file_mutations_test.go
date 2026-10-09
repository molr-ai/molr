package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func TestCommonFileMoveDeleteReceiptsAndVisibility(t *testing.T) {
	f, files, target := fileFixture(t)
	executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), FileRead: files.read, FileWrite: files.write, FileMove: files.move, FileDelete: files.delete})
	defs, err := executor.Definitions(t.Context(), f.execution.Principal)
	need(t, err)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["file_move"] || !names["file_delete"] {
		t.Fatal("new tools unavailable to ordinary assistants", names)
	}
	ctx := tools.WithVisibleTools(t.Context(), []string{"file_read"})
	defs, err = executor.Definitions(ctx, f.execution.Principal)
	need(t, err)
	if len(defs) != 1 || defs[0].Name != "file_read" {
		t.Fatal(defs)
	}
	before := readFileResult(t, files, f.execution, target)
	raw, _ := json.Marshal(tools.FileMoveRequest{FileTarget: target, ToPath: "/write/subdir/moved", ExpectedVersion: before.Version, CreateParents: true})
	call := tools.Call{Execution: f.execution, Name: "file_move", Arguments: raw}
	call.Execution.OperationID = "move-file"
	denied, err := executor.Execute(ctx, call)
	if core.CodeOf(err) != core.Forbidden || denied.Failure == nil {
		t.Fatal("hidden tool was executable", denied, err)
	}
	result, err := executor.Execute(t.Context(), call)
	need(t, err)
	var moved struct{ Version string }
	need(t, json.Unmarshal(result.Content, &moved))
	if moved.Version == "" {
		t.Fatal("missing common move version")
	}
	if _, err = os.Stat(filepath.Join(f.root, "write/data")); !os.IsNotExist(err) {
		t.Fatal("source remains", err)
	}
	replay, err := executor.Execute(t.Context(), call)
	need(t, err)
	if string(replay.Content) != string(result.Content) {
		t.Fatal("move did not reuse receipt")
	}
	target.Path = "/write/subdir/moved"
	raw, _ = json.Marshal(tools.FileDeleteRequest{FileTarget: target, ExpectedVersion: moved.Version})
	call.Name, call.Arguments, call.Execution.OperationID = "file_delete", raw, "delete-file"
	_, err = executor.Execute(t.Context(), call)
	need(t, err)
	dest := filepath.Join(f.root, "write/subdir/moved")
	need(t, os.WriteFile(dest, []byte("later file"), 0600))
	_, err = executor.Execute(t.Context(), call)
	need(t, err)
	data, err := os.ReadFile(dest)
	need(t, err)
	if string(data) != "later file" {
		t.Fatal("replayed deletion removed later file")
	}
	nested := tools.WithVisibleTools(ctx, []string{"file_read", "file_delete"})
	denied, err = executor.Execute(nested, call)
	if core.CodeOf(err) != core.Forbidden || denied.Failure == nil {
		t.Fatal("nested filter expanded visibility")
	}
}
func TestFileMoveDeleteVersionsDestinationsAndLinks(t *testing.T) {
	f, files, target := fileFixture(t)
	before := readFileResult(t, files, f.execution, target)
	need(t, os.WriteFile(filepath.Join(f.root, "write/existing"), []byte("keep"), 0600))
	if _, err := files.move(t.Context(), f.execution, tools.FileMoveRequest{FileTarget: target, ToPath: "/write/existing", ExpectedVersion: before.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("destination replaced", err)
	}
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte("later edit"), 0600))
	if _, err := files.delete(t.Context(), f.execution, tools.FileDeleteRequest{FileTarget: target, ExpectedVersion: before.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale delete", err)
	}
	if _, err := files.move(t.Context(), f.execution, tools.FileMoveRequest{FileTarget: target, ToPath: "/write/new", ExpectedVersion: before.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale move", err)
	}
	need(t, os.Symlink(filepath.Join(f.root, "read/data"), filepath.Join(f.root, "write/link")))
	link := target
	link.Path = "/write/link"
	if _, err := files.delete(t.Context(), f.execution, tools.FileDeleteRequest{FileTarget: link, ExpectedVersion: before.Version}); err == nil {
		t.Fatal("deleted symlink")
	}
	if _, err := files.move(t.Context(), f.execution, tools.FileMoveRequest{FileTarget: link, ToPath: "/write/moved-link", ExpectedVersion: before.Version}); err == nil {
		t.Fatal("moved symlink")
	}
	dir := target
	dir.Path = "/write"
	if _, err := files.delete(t.Context(), f.execution, tools.FileDeleteRequest{FileTarget: dir, ExpectedVersion: before.Version}); err == nil {
		t.Fatal("deleted directory")
	}
}
func TestFileMoveRequiresDestinationWritePermission(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	f.grant(t, f.storage.ID, "write-scope", []policy.Action{policy.Read, policy.Write}, policy.Scope{PathPrefixes: []string{"/write"}}, nil)
	files := &resourceFileTools{adapters: f.ad}
	target := tools.FileTarget{ResourceID: f.storage.ID, Path: "/write/data"}
	before := readFileResult(t, files, f.execution, target)
	if _, err := files.move(t.Context(), f.execution, tools.FileMoveRequest{FileTarget: target, ToPath: "/read/stolen", ExpectedVersion: before.Version}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("destination scope was not checked", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "write/data")); err != nil {
		t.Fatal("denied move changed source", err)
	}
}
