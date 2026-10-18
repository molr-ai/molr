//go:build linux

package installation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestStartServiceLinger(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		before, after                 string
		checkErr, enableErr           error
		system, noStart, restartFails bool
		wantChecks, wantEnables       int
		wantWarning                   bool
	}{
		{name: "already enabled", before: "yes\n", wantChecks: 1},
		{name: "automatically enabled", before: "no\n", after: "yes\n", wantChecks: 2, wantEnables: 1},
		{name: "authorization required", before: "no\n", enableErr: errors.New("Interactive authentication required"), wantChecks: 1, wantEnables: 1, wantWarning: true},
		{name: "command unavailable", checkErr: errors.New("loginctl not found"), enableErr: errors.New("loginctl not found"), wantChecks: 1, wantEnables: 1, wantWarning: true},
		{name: "enable timed out", before: "no\n", enableErr: context.DeadlineExceeded, wantChecks: 1, wantEnables: 1, wantWarning: true},
		{name: "enable did not persist", before: "no\n", after: "no\n", wantChecks: 2, wantEnables: 1, wantWarning: true},
		{name: "verification failed", checkErr: errors.New("user unavailable"), wantChecks: 2, wantEnables: 1, wantWarning: true},
		{name: "unknown initial state", before: "", after: "yes\n", wantChecks: 2, wantEnables: 1},
		{name: "system service", system: true},
		{name: "no start", noStart: true},
		{name: "restart failed", restartFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, r := fixture(t)
			r.System = tc.system
			e.Options.NoStart = tc.noStart
			e.Options.NonInteractive = true
			checks, enables, restarts := 0, 0, 0
			e.Command = func(ctx context.Context, args []string, input io.Reader, _ []string, out, diagnostics io.Writer) ([]byte, error) {
				switch args[0] {
				case "systemctl":
					if slices.Contains(args, "restart") {
						restarts++
						if tc.restartFails {
							return nil, errors.New("restart failed")
						}
					}
					return nil, nil
				case "loginctl":
					if restarts != 1 || input != nil || out != nil || diagnostics != nil || args[1] != "--no-ask-password" {
						t.Fatalf("unexpected interactive or premature linger command: %v", args)
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("linger command has no timeout")
					}
					switch args[2] {
					case "show-user":
						if !slices.Equal(args[3:], []string{r.User, "--property=Linger", "--value"}) {
							t.Fatalf("wrong user or property: %v", args)
						}
						checks++
						if checks == 1 {
							return []byte(tc.before), tc.checkErr
						}
						return []byte(tc.after), tc.checkErr
					case "enable-linger":
						if !slices.Equal(args[3:], []string{r.User}) {
							t.Fatalf("wrong account: %v", args)
						}
						enables++
						return nil, tc.enableErr
					}
				}
				t.Fatalf("unexpected host command: %v", args)
				return nil, nil
			}
			if err := e.startService(r, true, !tc.noStart); (err != nil) != tc.restartFails {
				t.Fatalf("startService: %v", err)
			}
			if checks != tc.wantChecks || enables != tc.wantEnables {
				t.Fatalf("checks=%d enables=%d; want %d and %d", checks, enables, tc.wantChecks, tc.wantEnables)
			}
			output := e.UI.Out.(*bytes.Buffer).String()
			if strings.Contains(output, "Warning:") != tc.wantWarning {
				t.Fatalf("unexpected warning state: %s", output)
			}
			if tc.wantWarning {
				for _, text := range []string{"last login session ends", "Administrator authorization", "sudo loginctl enable-linger " + shellJoin([]string{r.User}), "Linger=yes"} {
					if !strings.Contains(output, text) {
						t.Fatalf("missing guidance %q: %s", text, output)
					}
				}
			}
			if strings.Contains(output, "User linger is enabled") != (tc.wantChecks > 0 && !tc.wantWarning) {
				t.Fatalf("incorrect linger success claim: %s", output)
			}
		})
	}
}
