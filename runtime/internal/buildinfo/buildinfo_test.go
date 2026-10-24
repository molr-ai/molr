package buildinfo

import (
	"runtime"
	"testing"
)

func TestInjectedReleaseIdentityWinsOverVCSFallback(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt, oldDistribution, oldImage := Version, Commit, BuiltAt, Distribution, ContainerImage
	t.Cleanup(func() {
		Version, Commit, BuiltAt, Distribution, ContainerImage = oldVersion, oldCommit, oldBuiltAt, oldDistribution, oldImage
	})
	Version, Commit, BuiltAt = "0.2.0-rc.1", "release-commit", "2026-10-11T00:00:00Z"
	Distribution, ContainerImage = "container", "ghcr.io/example/molr"
	got := Current()
	if got.Distribution != Distribution || got.ContainerImage != ContainerImage || got.Version != Version || got.Commit != Commit || got.BuiltAt != BuiltAt || got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Fatal(got)
	}
}
