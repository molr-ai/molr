package config

import (
	"bytes"
	"encoding/json"
	"molr.ai/runtime/internal/platform"
	"slices"
	"testing"
)

func TestResourcePresetsExpandAndRoundTrip(t *testing.T) {
	for _, name := range []string{"permissive", "least-privilege"} {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			var err error
			c.Sandbox, err = SandboxPreset(name)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("access_mode")) || bytes.Contains(raw, []byte("resource_preset")) {
				t.Fatal("preset persisted as runtime mode")
			}
			s := loaded.Sandbox
			if name == "least-privilege" {
				if s.Enabled || s.AllowFullAccess || !s.RestrictDirectories || len(s.Directories) != 0 || len(s.NetworkTargets(8787)) != 0 || s.GPU.Enabled {
					t.Fatal("least privilege exposes resources")
				}
			} else {
				if !s.Enabled || !s.AllowFullAccess || s.RestrictDirectories || len(s.NetworkTargets(8787)) == 0 {
					t.Fatal("permissive preset remains restricted")
				}
				expected := []string{"internet", "lan", "host"}
				if platform.NativeSandbox() {
					expected = []string{"network"}
				}
				if !slices.Equal(s.NetworkPresets, expected) {
					t.Fatalf("network presets = %v, want %v", s.NetworkPresets, expected)
				}
				if !platform.NativeSandbox() && (!s.GPU.AllDevices || !s.ComputeCeilings().AllGPUDevices || s.AuthorizationCeilings.CPUPercent != 0) {
					t.Fatal("permissive preset has deployment ceilings")
				}
			}
		})
	}
	if _, err := SandboxPreset("personal"); err == nil {
		t.Fatal("accepted unknown preset")
	}
}
