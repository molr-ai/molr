package releaseplatform

import "testing"

func TestSupportMatchesInstallableTargets(t *testing.T) {
	for _, target := range []struct {
		os, arch  string
		supported bool
	}{
		{"linux", "amd64", true}, {"linux", "arm64", true},
		{"linux", "386", false}, {"darwin", "arm64", false}, {"windows", "amd64", false},
		{"linux", "x86_64", false}, // Runtime uses canonical Go architecture names, not uname aliases.
	} {
		if Supported(target.os, target.arch) != target.supported {
			t.Fatalf("unexpected support for %s/%s", target.os, target.arch)
		}
	}
}
