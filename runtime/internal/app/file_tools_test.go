package app

import (
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/filetools"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
)

func fileFixture(t *testing.T) (resourceAccessFixture, *resourceFileTools, tools.FileTarget) {
	t.Helper()
	f := newResourceAccessFixture(t, nil)
	f.grant(t, f.storage.ID, "files", []policy.Action{policy.Read, policy.Write}, policy.Scope{}, nil)
	return f, newResourceFileTools(resourceFileOptions{adapters: f.ad}), tools.FileTarget{ResourceID: f.storage.ID, Path: "/write/data"}
}
func readFileResult(t *testing.T, fs *resourceFileTools, e core.Execution, target tools.FileTarget) filetools.ReadResult {
	t.Helper()
	raw, err := fs.Read(t.Context(), e, tools.FileReadRequest{FileTarget: target})
	need(t, err)
	var out filetools.ReadResult
	need(t, json.Unmarshal(raw, &out))
	return out
}
func TestFileReadRangesLargeFilesAndExplicitTruncation(t *testing.T) {
	f, fs, target := fileFixture(t)
	// Larger than the old 1 MiB ceiling; a small requested range remains cheap.
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte(strings.Repeat("hello 世界\r\n", 100000)), 0600))
	raw, err := fs.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, Offset: 90000, Limit: 2})
	need(t, err)
	var out filetools.ReadResult
	need(t, json.Unmarshal(raw, &out))
	if out.Text != "hello 世界\r\nhello 世界\r\n" || out.StartLine != 90000 || out.EndLine != 90001 || !out.Truncated || out.NextOffset != 90002 || out.TotalLines != nil || out.Version == "" {
		t.Fatalf("wrong range: %+v", out)
	}
	raw, err = fs.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, Offset: 100000, Limit: 2})
	need(t, err)
	out = filetools.ReadResult{}
	need(t, json.Unmarshal(raw, &out))
	if out.Truncated || out.TotalLines == nil || *out.TotalLines != 100000 || out.NextOffset != 0 {
		t.Fatalf("wrong EOF: %+v", out)
	}
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte(strings.Repeat(strings.Repeat("a", 999)+"\n", 100)), 0600))
	raw, err = fs.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, Limit: 200})
	need(t, err)
	out = filetools.ReadResult{}
	need(t, json.Unmarshal(raw, &out))
	if len(out.Text) > filetools.OutputLimit || !out.Truncated || out.NextOffset != 66 {
		t.Fatalf("output bound missing: bytes=%d next=%d", len(out.Text), out.NextOffset)
	}
}
func TestFileReadRejectsLongLinesAndReportsEmpty(t *testing.T) {
	f, fs, target := fileFixture(t)
	for _, data := range [][]byte{[]byte(strings.Repeat("x", filetools.OutputLimit+1))} {
		need(t, os.WriteFile(filepath.Join(f.root, "write/data"), data, 0600))
		if _, err := fs.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target}); core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("unsupported content accepted: %v", err)
		}
	}
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), nil, 0600))
	out := readFileResult(t, fs, f.execution, target)
	if out.Text != "" || out.StartLine != 0 || out.EndLine != 0 || out.TotalLines == nil || *out.TotalLines != 0 || out.Truncated {
		t.Fatalf("ambiguous empty result: %+v", out)
	}
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte("small\n"+strings.Repeat("x", filetools.OutputLimit+1)), 0600))
	raw, err := fs.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, Limit: 1})
	need(t, err)
	out = filetools.ReadResult{}
	need(t, json.Unmarshal(raw, &out))
	if out.Text != "small\n" || !out.Truncated || out.NextOffset != 2 {
		t.Fatal("unrequested long line discarded valid requested range")
	}
}
func TestFileListPaginationScopeAndVersion(t *testing.T) {
	f, fs, _ := fileFixture(t)
	target := tools.FileTarget{ResourceID: f.storage.ID, Path: "/read"}
	for i := 0; i < 6; i++ {
		need(t, os.WriteFile(filepath.Join(f.root, "read", fmt.Sprintf("file-%d", i)), nil, 0600))
	}
	need(t, os.Symlink("../secret", filepath.Join(f.root, "read/link")))
	seen := map[string]bool{}
	req := tools.FileListRequest{FileTarget: target, Limit: 2}
	pages := 0
	for {
		raw, err := fs.List(t.Context(), f.execution, req)
		need(t, err)
		var out filetools.ListResult
		need(t, json.Unmarshal(raw, &out))
		pages++
		for _, entry := range out.Entries {
			if seen[entry.Name] || !strings.HasPrefix(entry.Path, "/resources/"+string(f.storage.ID)+"/read/") {
				t.Fatal("duplicate/out-of-scope directory entry")
			}
			seen[entry.Name] = true
			if entry.Name == "link" && entry.Type != "symlink" {
				t.Fatal("link target followed")
			}
		}
		if !out.Truncated {
			break
		}
		req.Offset = out.NextOffset
		req.ExpectedVersion = out.Version
		if pages > 5 {
			t.Fatal("pagination did not progress")
		}
	}
	if len(seen) != 8 {
		t.Fatalf("missing entries %v", seen)
	}
	need(t, os.WriteFile(filepath.Join(f.root, "read/new"), nil, 0600))
	if _, err := fs.List(t.Context(), f.execution, req); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale directory page accepted")
	}
	if _, err := fs.List(t.Context(), f.execution, tools.FileListRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/read/link"}}); err == nil {
		t.Fatal("listed symlink target")
	}
}
func TestFileWriteExplicitCreateReplaceAndVersions(t *testing.T) {
	f, fs, target := fileFixture(t)
	if _, err := fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "bad"}); core.CodeOf(err) != core.Conflict {
		t.Fatal("implicit overwrite")
	}
	before := readFileResult(t, fs, f.execution, target)
	raw, err := fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "replacement", Overwrite: true, ExpectedVersion: before.Version})
	need(t, err)
	var result filetools.MutationResult
	need(t, json.Unmarshal(raw, &result))
	after := readFileResult(t, fs, f.execution, target)
	if after.Text != "replacement" || result.Version != after.Version || before.Version == after.Version {
		t.Fatal("replacement/version mismatch")
	}
	if _, err = fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "stale", Overwrite: true, ExpectedVersion: before.Version}); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale overwrite accepted")
	}
	target.Path = "/write/new"
	_, err = fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: ""})
	need(t, err)
	if _, err = fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: ""}); core.CodeOf(err) != core.Conflict {
		t.Fatal("duplicate create accepted")
	}
}
func TestFileEditExactMatchConflictAndConcurrentUpdates(t *testing.T) {
	f, fs, target := fileFixture(t)
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte("one\r\ntwo one\r\n"), 0600))
	before := readFileResult(t, fs, f.execution, target)
	req := tools.FileEditRequest{FileTarget: target, OldText: "one", NewText: "changed", ExpectedVersion: before.Version}
	if _, err := fs.Edit(t.Context(), f.execution, req); core.ReasonOf(err) != "file_edit_ambiguous" {
		t.Fatalf("ambiguous edit accepted: %v", err)
	}
	req.OldText = "missing"
	if _, err := fs.Edit(t.Context(), f.execution, req); core.ReasonOf(err) != "file_edit_no_match" {
		t.Fatal("missing match accepted")
	}
	req.OldText = "one"
	req.ReplaceAll = true
	raw, err := fs.Edit(t.Context(), f.execution, req)
	need(t, err)
	var result filetools.MutationResult
	need(t, json.Unmarshal(raw, &result))
	if result.Replacements != 2 || readFileResult(t, fs, f.execution, target).Text != "changed\r\ntwo changed\r\n" {
		t.Fatal("exact edits changed line endings")
	}
	if _, err = fs.Edit(t.Context(), f.execution, req); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale edit accepted")
	}
	current := readFileResult(t, fs, f.execution, target)
	req.ExpectedVersion = current.Version
	req.OldText = "two changed"
	req.NewText = "new"
	req.ReplaceAll = false
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := fs.Edit(t.Context(), f.execution, req); errors <- err }()
	}
	wg.Wait()
	close(errors)
	success, conflicts := 0, 0
	for err := range errors {
		if err == nil {
			success++
		} else if core.CodeOf(err) == core.Conflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("concurrent edits lost updates: %d %d", success, conflicts)
	}
}
func TestFileEditRequiresReadAndWriteOnSamePath(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	fs := newResourceFileTools(resourceFileOptions{adapters: f.ad})
	target := tools.FileTarget{ResourceID: f.storage.ID, Path: "/write/data"}
	f.grant(t, f.storage.ID, "write", []policy.Action{policy.Write}, policy.Scope{PathPrefixes: []string{"/write"}}, nil)
	f.grant(t, f.storage.ID, "read-other", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/read"}}, nil)
	_, err := fs.Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "write-only", Overwrite: true})
	need(t, err)
	if _, err = fs.Edit(t.Context(), f.execution, tools.FileEditRequest{FileTarget: target, OldText: "write-only", NewText: "bad", ExpectedVersion: "unknown"}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("edit inferred read permission")
	}
	if _, err = fs.List(t.Context(), f.execution, tools.FileListRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/"}}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("scoped read became root listing")
	}
	listing := f.list(t)
	if lookupOperation(t, listing, f.storage.ID, "/write", policy.Write, "file_edit").Available {
		t.Fatal("catalog advertised edit without matching read grant")
	}
	capabilities, err := f.b.Services.Agent.Capabilities(t.Context(), f.owner, f.execution.AssistantID)
	need(t, err)
	for _, tool := range capabilities.Tools {
		if tool.Name == "file_edit" && slices.Contains(tool.ResourceIDs, f.storage.ID) {
			t.Fatal("UI advertised disjoint read/write grants as editable")
		}
	}
	f.grant(t, f.storage.ID, "read-matching", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/write"}}, nil)
	capabilities, err = f.b.Services.Agent.Capabilities(t.Context(), f.owner, f.execution.AssistantID)
	need(t, err)
	found := false
	for _, tool := range capabilities.Tools {
		if tool.Name == "file_edit" {
			found = tool.Available && slices.Contains(tool.ResourceIDs, f.storage.ID)
		}
	}
	if !found {
		t.Fatal("separate matching read/write grants not recognized")
	}
}

func TestFileToolsExecuteWithReceiptsAndIdempotency(t *testing.T) {
	f, fs, target := fileFixture(t)
	ex := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), FileRead: fs.Read, FileList: fs.List, FileWrite: fs.Write, FileEdit: fs.Edit})
	input, _ := json.Marshal(tools.FileReadRequest{FileTarget: target})
	call := tools.Call{Execution: f.execution, Name: "file_read", Arguments: input}
	call.Execution.OperationID = "read-file"
	result, err := ex.Execute(t.Context(), call)
	need(t, err)
	var before filetools.ReadResult
	need(t, json.Unmarshal(result.Content, &before))
	input, _ = json.Marshal(tools.FileEditRequest{FileTarget: target, OldText: "original", NewText: "edited", ExpectedVersion: before.Version})
	call.Name = "file_edit"
	call.Arguments = input
	call.Execution.OperationID = "edit-file"
	first, err := ex.Execute(t.Context(), call)
	need(t, err)
	second, err := ex.Execute(t.Context(), call)
	need(t, err)
	if string(first.Content) != string(second.Content) || readFileResult(t, fs, f.execution, target).Text != "edited" {
		t.Fatal("edit replay changed file twice")
	}
	input, _ = json.Marshal(tools.FileWriteRequest{FileTarget: target, Text: "must-not-replace"})
	call.Name = "file_write"
	call.Arguments = input
	call.Execution.OperationID = "rejected-write"
	if _, err = ex.Execute(t.Context(), call); core.CodeOf(err) != core.Conflict {
		t.Fatal("create overwrote file")
	}
	op, err := operation.NewRepository(f.b.store).Get(t.Context(), f.owner.OwnerID, call.Execution.OperationID)
	need(t, err)
	if op.State != operation.Failed {
		t.Fatalf("known no-effect rejection marked %s", op.State)
	}
}
