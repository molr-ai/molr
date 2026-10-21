package installation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLaunchAgentExplicitStart(t *testing.T) {
	for _, tc := range []struct {
		name, domain, fail               string
		loaded, running, resume, noStart bool
	}{
		{name: "fresh GUI registration", domain: "gui"},
		{name: "fresh SSH registration", domain: "user"},
		{name: "pending GUI registration", domain: "gui", loaded: true},
		{name: "pending user registration with GUI available", domain: "user", loaded: true},
		{name: "running process is preserved", domain: "gui", loaded: true, running: true},
		{name: "resume explicitly starts", domain: "gui", resume: true},
		{name: "no start only writes plist", domain: "gui", noStart: true},
		{name: "bootstrap error", domain: "gui", fail: "bootstrap"},
		{name: "kickstart error", domain: "gui", fail: "kickstart"},
		{name: "enable error", domain: "gui", fail: "enable"},
		{name: "inspection error", domain: "gui", fail: "print"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, r := fixture(t)
			domain := fmt.Sprintf("%s/%d", tc.domain, r.UID)
			target := domain + "/" + launchLabel(r)
			loaded, running := tc.loaded, tc.running
			bootstraps, kicks, commands := 0, 0, 0
			failure := errors.New("host operation refused")
			e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
				commands++
				if args[0] != "/bin/launchctl" || slices.Contains(args, "-k") {
					t.Fatalf("unexpected or destructive command: %v", args)
				}
				if args[1] == tc.fail {
					return nil, failure
				}
				switch args[1] {
				case "print":
					if strings.Count(args[2], "/") == 1 {
						if tc.domain == "user" && !tc.loaded {
							return nil, errors.New("Could not find domain")
						}
						return nil, nil
					}
					if !loaded || args[2] != target {
						return nil, errors.New("Could not find service")
					}
					state := "\tstate = not running\n\truns = 0\n\tpended nondemand spawn = speculative\n"
					if running {
						state = "\tstate = running\n\tpid = 123\n\truns = 1\n"
					}
					return []byte("\tpath = " + r.Paths.Unit + "\n" + state), nil
				case "enable":
					if args[2] != target {
						t.Fatalf("enabled wrong domain: %v", args)
					}
				case "bootstrap":
					bootstraps++
					if loaded || !slices.Equal(args[2:], []string{domain, r.Paths.Unit}) {
						t.Fatalf("duplicate or wrong registration: %v", args)
					}
					loaded = true
					// An on-demand-only domain accepts registration without spawning.
				case "kickstart":
					kicks++
					if !loaded || !slices.Equal(args[2:], []string{target}) {
						t.Fatalf("invalid start request: %v", args)
					}
					running = true
				default:
					t.Fatalf("unexpected host operation: %v", args)
				}
				return nil, nil
			}
			var err error
			if tc.resume {
				err = e.resumeService(r)
			} else {
				err = e.startService(r, false, !tc.noStart)
			}
			if tc.fail != "" {
				if err == nil || !strings.Contains(err.Error(), failure.Error()) || running {
					t.Fatalf("host failure was not preserved: running=%v err=%v", running, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.noStart {
				if commands != 0 {
					t.Fatal("no-start invoked launchctl")
				}
				if _, err := os.Stat(r.Paths.Unit); err != nil {
					t.Fatal(err)
				}
				return
			}
			if !running || kicks != 1 || (tc.loaded && bootstraps != 0) || (!tc.loaded && bootstraps != 1) {
				t.Fatalf("running=%v bootstraps=%d kicks=%d", running, bootstraps, kicks)
			}
		})
	}
}

func TestLaunchAgentRejectsConflictingRegistrations(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%t", duplicate), func(t *testing.T) {
			e, r := fixture(t)
			e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
				if args[0] != "/bin/launchctl" || args[1] != "print" {
					t.Fatalf("conflicting service must not be changed: %v", args)
				}
				path := r.Paths.Unit + ".other"
				if duplicate {
					path = r.Paths.Unit
				}
				return []byte("\tpath = " + path + "\n\tstate = not running\n"), nil
			}
			if err := e.startService(r, false, true); err == nil {
				t.Fatal("conflicting service accepted")
			}
		})
	}
}

func TestLaunchAgentServiceStateIgnoresNestedFields(t *testing.T) {
	for _, tc := range []struct{ name, fields, wantState, wantPID string }{
		{"running", "state = running\n\tpid = 123", "running", "123"},
		{"crashed", "state = not running\n\tlast terminating signal = Killed: 9", "inactive", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, r := fixture(t)
			e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
				if args[1] != "print" {
					t.Fatalf("unexpected mutation: %v", args)
				}
				if strings.HasPrefix(args[2], "user/") {
					return nil, errors.New("Could not find service")
				}
				return []byte(fmt.Sprintf(`service = {
	path = %s
	%s
	resource coalition = {
		state = active
	}
	jetsam coalition = {
		state = active
	}
	environment = {
		path = unrelated
		pid = 999
	}
}
`, r.Paths.Unit, tc.fields)), nil
			}
			state, err := e.unitState(r)
			if err != nil {
				t.Fatal(err)
			}
			if state["ActiveState"] != tc.wantState || state["MainPID"] != tc.wantPID || state["FragmentPath"] != r.Paths.Unit {
				t.Fatalf("wrong service state: %v", state)
			}
		})
	}
}
