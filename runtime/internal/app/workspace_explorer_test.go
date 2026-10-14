package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestWorkspaceExplorerLifecycleAndConflict(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := &workspaceExplorer{ad: f.ad, maxUpload: 2 << 20}
	id := f.execution.AssistantID
	call := func(q api.WorkspaceRequest) any {
		t.Helper()
		v, err := s.Query(t.Context(), f.owner, id, q)
		need(t, err)
		return v
	}
	call(api.WorkspaceRequest{Action: "mkdir", Path: "projects/folder"})
	created := call(api.WorkspaceRequest{Action: "write", Path: "projects/folder/你好..txt", Text: "original\r\n"}).(api.WorkspaceEntry)
	if _, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "write", Path: created.Path, Text: "overwrite"}); core.CodeOf(err) != core.Conflict {
		t.Fatalf("implicit overwrite: %v", err)
	}
	read := call(api.WorkspaceRequest{Action: "read", Path: created.Path}).(api.WorkspaceText)
	if read.Text != "original\r\n" || !read.Editable {
		t.Fatalf("read: %+v", read)
	}
	updated := call(api.WorkspaceRequest{Action: "write", Path: created.Path, ExpectedVersion: read.Version, Text: "assistant edit"}).(api.WorkspaceEntry)
	for _, action := range []string{"write", "move", "delete"} {
		_, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: action, Path: created.Path, ToPath: "outputs/moved", ExpectedVersion: read.Version, Text: "stale"})
		if core.CodeOf(err) != core.Conflict {
			t.Fatalf("%s stale version: %v", action, err)
		}
	}
	moved := call(api.WorkspaceRequest{Action: "move", Path: created.Path, ToPath: "outputs/moved", ExpectedVersion: updated.Version}).(api.WorkspaceEntry)
	call(api.WorkspaceRequest{Action: "delete", Path: moved.Path, ExpectedVersion: moved.Version})
	folder := call(api.WorkspaceRequest{Action: "stat", Path: "projects/folder"}).(api.WorkspaceEntry)
	call(api.WorkspaceRequest{Action: "move", Path: folder.Path, ToPath: "outputs/folder", ExpectedVersion: folder.Version})
	folder = call(api.WorkspaceRequest{Action: "stat", Path: "outputs/folder"}).(api.WorkspaceEntry)
	call(api.WorkspaceRequest{Action: "delete", Path: folder.Path, ExpectedVersion: folder.Version})
	data := []byte{0, 255, 4, 2, 3}
	upload, err := s.Upload(t.Context(), f.owner, id, api.WorkspaceRequest{Path: "outputs/image.bin"}, bytes.NewReader(data))
	need(t, err)
	content, err := s.Download(t.Context(), f.owner, id, api.WorkspaceRequest{Path: upload.Path, ExpectedVersion: upload.Version})
	need(t, err)
	got, err := io.ReadAll(content)
	content.Close()
	need(t, err)
	if !bytes.Equal(got, data) {
		t.Fatal("binary changed")
	}
	outputs := call(api.WorkspaceRequest{Action: "stat", Path: "outputs"}).(api.WorkspaceEntry)
	_, err = s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "delete", Path: "outputs", ExpectedVersion: outputs.Version})
	if core.CodeOf(err) != core.Conflict {
		t.Fatalf("nonempty directory deleted: %v", err)
	}
}
func TestWorkspaceExplorerConfinementAndLimits(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := &workspaceExplorer{ad: f.ad, maxUpload: 1024}
	id := f.execution.AssistantID
	_, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list"})
	need(t, err)
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, id)
	need(t, os.WriteFile(filepath.Join(root, "file"), []byte("hello"), 0600))
	need(t, os.Symlink(f.root, filepath.Join(root, "link")))
	need(t, os.Link(filepath.Join(root, "file"), filepath.Join(root, "hard")))
	for _, path := range []string{"../secret", "/etc/passwd", "projects/../outputs", "link/read/data", "hard", "file", "a\\b"} {
		_, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "read", Path: path})
		if err == nil {
			t.Fatalf("unsafe read %q", path)
		}
	}
	for _, p := range []core.Principal{f.execution.Principal, {Kind: core.ActorOwner, OwnerID: "other", SubjectID: "other"}} {
		_, err := s.Query(t.Context(), p, id, api.WorkspaceRequest{Action: "list"})
		if err == nil {
			t.Fatal("foreign principal accepted")
		}
	}
	_, err = s.Upload(t.Context(), f.owner, id, api.WorkspaceRequest{Path: "large"}, strings.NewReader(strings.Repeat("a", 1025)))
	if err == nil {
		t.Fatal("upload limit ignored")
	}
	s.ad.storageConfig.MaxFileBytes = 2
	_, err = s.Upload(t.Context(), f.owner, id, api.WorkspaceRequest{Path: "quota"}, strings.NewReader("123"))
	if core.ReasonOf(err) != "storage_file_limit" {
		t.Fatalf("single file quota ignored: %v", err)
	}
	s.ad.storageConfig.Enabled = false
	_, err = s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list"})
	if core.ReasonOf(err) != "sandbox_disabled" {
		t.Fatalf("disabled sandbox: %v", err)
	}
}
func TestWorkspaceExplorerPaginationAndExternalEdits(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := &workspaceExplorer{ad: f.ad, maxUpload: 2 << 20}
	id := f.execution.AssistantID
	_, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list"})
	need(t, err)
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, id)
	for i := 0; i < 5; i++ {
		need(t, os.WriteFile(filepath.Join(root, "outputs", fmt.Sprintf("file-%d", i)), []byte("before"), 0600))
	}
	first, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list", Path: "outputs", Limit: 2})
	need(t, err)
	page := first.(api.WorkspaceListing)
	if len(page.Entries) != 2 || !page.Truncated || page.NextOffset != 2 {
		t.Fatal(page)
	}
	next, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list", Path: "outputs", Offset: 2, Limit: 2, ExpectedVersion: page.Version})
	need(t, err)
	if next.(api.WorkspaceListing).Entries[0].Path == page.Entries[0].Path {
		t.Fatal("repeated page")
	}
	need(t, os.WriteFile(filepath.Join(root, "outputs", "new"), nil, 0600))
	_, err = s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "list", Path: "outputs", Offset: 2, Limit: 2, ExpectedVersion: page.Version})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("stale listing accepted")
	}
	need(t, os.WriteFile(filepath.Join(root, page.Entries[0].Path), []byte("external"), 0600))
	_, err = s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "write", Path: page.Entries[0].Path, ExpectedVersion: page.Entries[0].Version, Text: "stale"})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("external edit overwritten")
	}
	need(t, os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("a", fileMutationLimit+1)), 0600))
	raw, err := s.Query(t.Context(), f.owner, id, api.WorkspaceRequest{Action: "read", Path: "large.txt"})
	need(t, err)
	text := raw.(api.WorkspaceText)
	if text.Editable || !text.Truncated || len(text.Text) != fileMutationLimit {
		t.Fatal("large text boundary")
	}
}
