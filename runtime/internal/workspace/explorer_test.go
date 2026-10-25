package workspace

import (
	"context"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadRechecksAuthorizationBeforeCommit(t *testing.T) {
	root := t.TempDir()
	calls := 0
	s := New(func(context.Context, core.Principal, core.ID, bool, int64) (string, error) {
		calls++
		if calls > 1 {
			return "", core.NewError(core.Forbidden, "revoked")
		}
		return root, nil
	}, 1024)
	_, err := s.Upload(t.Context(), core.Principal{}, "assistant", Request{Path: "file.txt"}, strings.NewReader("synthetic"))
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "file.txt")); !os.IsNotExist(err) {
		t.Fatal("upload committed after revocation", err)
	}
}
