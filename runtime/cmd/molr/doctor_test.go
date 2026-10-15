package main

import (
	"molr.ai/runtime/internal/platform"
	"testing"
)

func TestDoctorUsesExplicitExecutionCapabilities(t *testing.T) {
	isolated := platform.Capabilities{Reason: "available", Shell: true, ProcessIsolation: true, FileIsolation: true, NetworkIsolation: true, ResourceLimits: true}
	for _, tc := range []struct {
		name  string
		allow bool
		caps  platform.Capabilities
		ready bool
	}{
		{"isolated", false, isolated, true},
		{"seatbelt", false, platform.Capabilities{OS: "darwin", NetworkBackend: "seatbelt-toggle", Reason: "available", Shell: true, FileIsolation: true, NetworkIsolation: true}, true},
		{"direct opt-in", true, platform.Capabilities{Reason: "sandbox_platform_unsupported", FullAccess: true}, true},
		{"direct without opt-in", false, platform.Capabilities{FullAccess: true}, false},
		{"configured but unavailable", true, platform.Capabilities{Reason: "shell_unavailable"}, false},
		{"partial isolation", false, platform.Capabilities{Reason: "available", Shell: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandEnvironmentReady(tc.allow, tc.caps); got != tc.ready {
				t.Fatalf("ready=%v, want %v", got, tc.ready)
			}
		})
	}
}
