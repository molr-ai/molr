package installation

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestBwrapPreflightReusesLoadedProfileBeforeFallback(t *testing.T) {
	for _, mode := range []string{"chrome", "fallback", "unavailable", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			e, r := fixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			e.Ctx = ctx
			calls := 0
			e.Command = func(_ context.Context, command []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
				calls++
				joined := strings.Join(command, " ")
				if !strings.Contains(joined, "setpriv --no-new-privs ") || !slices.Contains(command, "/usr/bin/bwrap") || !slices.Contains(command, "--cap-drop") {
					t.Fatalf("unisolated probe: %v", command)
				}
				if calls == 1 {
					if !strings.Contains(joined, "/usr/bin/aa-exec -p chrome -- /usr/bin/bwrap") {
						t.Fatalf("existing chrome profile not preferred: %v", command)
					}
					if mode == "chrome" {
						return nil, nil
					}
					if mode == "cancelled" {
						cancel()
					}
					return nil, errors.New("profile unavailable")
				}
				if calls != 2 || slices.Contains(command, "/usr/bin/aa-exec") {
					t.Fatalf("unexpected fallback: %v", command)
				}
				if mode == "fallback" {
					return nil, nil
				}
				return nil, errors.New("bwrap unavailable")
			}
			err := e.probeBwrap(r)
			wantCalls := 2
			if mode == "chrome" || mode == "cancelled" {
				wantCalls = 1
			}
			wantFailure := mode == "unavailable" || mode == "cancelled"
			if (err != nil) != wantFailure || calls != wantCalls {
				t.Fatalf("preflight: err=%v calls=%d", err, calls)
			}
		})
	}
}
