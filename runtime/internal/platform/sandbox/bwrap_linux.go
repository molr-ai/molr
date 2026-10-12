//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"os"
	"strings"
)

// chrome is an existing application userns exemption, not Chromium's sandbox.
// Never load policies or change global settings. Explicit administrator launchers
// retain their policy choice. The actual loaded profile is tested by execution.
func chromeProfileCandidate(bwrap string) bool {
	if bwrap != "/usr/bin/bwrap" {
		return false
	}
	enabled, err := os.ReadFile("/sys/module/apparmor/parameters/enabled")
	if err != nil || strings.TrimSpace(string(enabled)) != "Y" {
		return false
	}
	f, err := pin("/usr/bin/aa-exec", false)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 && info.Mode()&0022 == 0
}

func (r *Runner) bwrapCommand(args ...string) []string {
	command := []string{r.options.Bwrap}
	if r.bwrapProfile != "" {
		command = []string{"/usr/bin/aa-exec", "-p", r.bwrapProfile, "--", r.options.Bwrap}
	}
	return append(command, args...)
}

// Called only while Prepare holds the Runner lock, before accepting commands.
// Only fixed, trusted probes may be retried. Both candidates use the complete
// Molr sandbox; failure never authorizes host execution or --no-sandbox.
func (r *Runner) prepareBwrap(ctx context.Context, profiles []string) error {
	var err error
	for _, profile := range profiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.bwrapProfile = profile
		err = r.check()
		if err == nil {
			err = r.probeIsolation(ctx)
		}
		if err == nil {
			return nil
		}
	}
	return err
}
