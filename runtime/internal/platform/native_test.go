package platform_test

import (
	"runtime"
	"testing"

	"molr.ai/runtime/internal/platform"
)

func TestNativeSandboxSelectsHostPlatform(t *testing.T) {
	if got := platform.NativeSandbox(); got != (runtime.GOOS == "darwin") {
		t.Fatalf("NativeSandbox() = %v on %s", got, runtime.GOOS)
	}
}

func TestControlledReadyRequiresBackendIsolation(t *testing.T) {
	linux := platform.Capabilities{OS: "linux", Reason: "available", Shell: true, FileIsolation: true, NetworkIsolation: true, ProcessIsolation: true, ResourceLimits: true}
	mac := platform.Capabilities{OS: "darwin", Reason: "available", Shell: true, FileIsolation: true, NetworkIsolation: true, NetworkBackend: "seatbelt-toggle"}
	for _, backend := range []platform.Capabilities{linux, mac} {
		t.Run(backend.OS, func(t *testing.T) {
			if !backend.ControlledReady() {
				t.Fatal("ready backend rejected")
			}
			for _, change := range []struct {
				name  string
				apply func(*platform.Capabilities)
			}{
				{"unavailable", func(c *platform.Capabilities) { c.Reason = "sandbox_disabled" }},
				{"shell", func(c *platform.Capabilities) { c.Shell = false }},
				{"files", func(c *platform.Capabilities) { c.FileIsolation = false }},
				{"network", func(c *platform.Capabilities) { c.NetworkIsolation = false }},
			} {
				t.Run(change.name, func(t *testing.T) {
					c := backend
					change.apply(&c)
					if c.ControlledReady() {
						t.Fatal("incomplete backend accepted")
					}
				})
			}
		})
	}
	for _, change := range []func(*platform.Capabilities){
		func(c *platform.Capabilities) { c.ProcessIsolation = false },
		func(c *platform.Capabilities) { c.ResourceLimits = false },
	} {
		c := linux
		change(&c)
		if c.ControlledReady() {
			t.Fatal("Linux backend without hard isolation accepted")
		}
	}
	mac.NetworkBackend = ""
	if mac.ControlledReady() {
		t.Fatal("macOS without Seatbelt backend accepted")
	}
	mac.NetworkBackend = "seatbelt-toggle"
	mac.OS = "linux"
	if mac.ControlledReady() {
		t.Fatal("Seatbelt contract accepted for Linux")
	}
}
