package buildinfo

import (
	"runtime"
	"testing"
)

func TestInjectedReleaseIdentityWinsOverVCSFallback(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := Version, Commit, BuiltAt
	t.Cleanup(func() { Version, Commit, BuiltAt = oldVersion, oldCommit, oldBuiltAt })
	Version, Commit, BuiltAt = "0.2.0-rc.1", "release-commit", "2026-10-11T00:00:00Z"
	got := Current()
	if got.Version != Version || got.Commit != Commit || got.BuiltAt != BuiltAt || got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Fatal(got)
	}
}
