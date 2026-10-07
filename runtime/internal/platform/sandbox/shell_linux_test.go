//go:build linux && (amd64 || arm64)

package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestLinuxHostShellAndToolPriority(t *testing.T) {
	r := hostToolsRunner(t)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = `set -eu
printf 'shell='; /usr/bin/readlink /proc/$$/exe
test "$(command -v wget)" = /usr/bin/wget
wget --version | head -1 | grep 'GNU Wget'
test "$PATH" = /usr/bin:/bin:/usr/sbin:/sbin:/molr/bin
test -x /molr/bin/dos2unix
test "$(printf 'fallback\r\n' | PATH=/molr/bin dos2unix)" = fallback
for applet in $(/molr/busybox --list); do test -x "/molr/bin/$applet"; done
if touch /molr/bin/writable 2>/dev/null; then exit 31; fi
echo system-tools-and-busybox-fallback-ok
`
	out := assertNetworkRun(t, r, c)
	t.Log(out)
	if !strings.Contains(out, "system-tools-and-busybox-fallback-ok") {
		t.Fatal(out)
	}
}

func TestLinuxHostShellProfileLoadsOnlyOnce(t *testing.T) {
	r := hostToolsRunner(t)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "printf 'echo loaded >> /work/profile-count\nexport MOLR_PROFILE_MARK=ready\n' > /work/.profile"
	assertNetworkRun(t, r, c)
	c.Script = "test ! -e /work/profile-count; test -z \"${MOLR_PROFILE_MARK-}\""
	assertNetworkRun(t, r, c)
	c.TTY = true
	c.Script = `test "$MOLR_PROFILE_MARK" = ready && test "$(wc -l < /work/profile-count)" = 1 && echo profile-loaded-once`
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "profile-loaded-once") {
		t.Fatal(out)
	}
}

func TestLinuxHostShellSelection(t *testing.T) {
	for _, test := range []struct {
		name    string
		shells  map[string]string
		want    string
		warning bool
	}{
		{"system-sh", map[string]string{"sh": "/usr/bin/dash", "bash": "/usr/bin/bash"}, "dash", false},
		{"standalone-sh-with-dash", map[string]string{"sh": "/molr/busybox", "dash": "/usr/bin/dash", "bash": "/usr/bin/bash"}, "dash", false},
		{"standalone-sh-with-bash", map[string]string{"sh": "/molr/busybox", "bash": "/usr/bin/bash"}, "bash", false},
		{"standalone-sh-only", map[string]string{"sh": "/molr/busybox"}, "busybox", true},
		{"no-system-shell", map[string]string{}, "busybox", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := hostTTYFixture{hostTools: true, systemShells: test.shells, script: `set -eu; case "$(/usr/bin/readlink /proc/$$/exe)" in */` + test.want + `) ;; *) exit 32;; esac; printf 'selected-%s\n' ` + test.want}
			runHostToolsTTYFixture(t, fixture, func(terminal *ttyTestTerminal) {
				terminal.wait("selected-" + test.want)
				warning := strings.Contains(terminal.output(), "built-in applets may take precedence")
				if warning != test.warning {
					t.Fatalf("fallback notice mismatch: %s", terminal.output())
				}
			})
		})
	}
}

func TestLinuxHostToolFailureIsNotRetried(t *testing.T) {
	fixture := hostTTYFixture{hostTools: true, commands: map[string]string{
		"/usr/bin/wget": "#!/molr/busybox sh\nprintf 'called\\n' >> /work/calls\nexit 23\n",
	}, script: `wget --version; status=$?; test "$status" = 23 || exit 41; test "$(wc -l < /work/calls)" = 1 || exit 42; echo no-retry-ok`}
	runHostToolsTTYFixture(t, fixture, func(terminal *ttyTestTerminal) { terminal.wait("no-retry-ok") })
}
