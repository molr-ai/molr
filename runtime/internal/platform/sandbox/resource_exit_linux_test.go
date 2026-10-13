//go:build linux && (amd64 || arm64)

package sandbox

import (
	"molr.ai/runtime/internal/platform"
	"testing"
)

func TestResourceExitReasonUsesKernelEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		code                       int
		reason, memory, pids, want string
	}{
		{"denied threads", 125, "", "oom_kill 0\n", "max 9\n", "process_limit"},
		{"successful command", 0, "", "oom_kill 0\n", "max 1\n", ""},
		{"unrelated failure", 125, "", "oom_kill 0\n", "max 0\n", ""},
		{"cancel precedence", 125, "canceled", "oom_kill 0\n", "max 3\n", "canceled"},
		{"memory precedence", 125, "", "oom_kill 1\n", "max 3\n", "memory_limit"},
		{"missing counters", 125, "", "", "", ""},
		{"invalid counters", 125, "", "oom_kill bad\n", "max -1\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resourceExitReason(platform.Exit{Code: tc.code, Reason: tc.reason}, []byte(tc.memory), []byte(tc.pids))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
