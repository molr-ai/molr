package filetools

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandaloneFilesRespectHostRevocationAndVersions(t *testing.T) {
	root := t.TempDir()
	revoked := false
	fs := New(Options{Resolve: func(ctx context.Context, e core.Execution, q tools.FileTarget, _ ...policy.Action) (string, string, error) {
		if revoked {
			return "", "", core.NewError(core.Forbidden, "revoked")
		}
		if !filepath.IsLocal(q.Path) {
			return "", "", core.Invalid("path", "local required")
		}
		return root, q.Path, nil
	}, CheckSize: func(context.Context, core.Execution, tools.FileTarget, int64) error { return nil }})
	e := core.Execution{}
	target := tools.FileTarget{Path: "note.txt"}
	result, err := fs.Write(t.Context(), e, tools.FileWriteRequest{FileTarget: target, Text: "first\n"})
	if err != nil {
		t.Fatal(err)
	}
	var written MutationResult
	if err = json.Unmarshal(result, &written); err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Edit(t.Context(), e, tools.FileEditRequest{FileTarget: target, ExpectedVersion: "stale", OldText: "first", NewText: "second"}); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	result, err = fs.Read(t.Context(), e, tools.FileReadRequest{FileTarget: target})
	if err != nil || !strings.Contains(string(result), "first") {
		t.Fatal(string(result), err)
	}
	revoked = true
	if _, err = fs.Delete(t.Context(), e, tools.FileDeleteRequest{FileTarget: target, ExpectedVersion: written.Version}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revocation ignored", err)
	}
	revoked = false
	if _, err = fs.Read(t.Context(), e, tools.FileReadRequest{FileTarget: target}); err != nil {
		t.Fatal("revoked deletion removed file", err)
	}
}
