package config

import (
	"testing"

	"molr.ai/runtime/internal/platform"
)

func TestWindowsSandboxConfiguration(t *testing.T) {
	c := Defaults(t.TempDir())
	if err := c.Sandbox.validate(c.DataDir, 8080); err != nil {
		t.Fatal(err)
	}
	if c.Sandbox.ComputeCeilings().CPUPercent != 0 || len(c.Sandbox.ToolDirectories) != 0 || len(c.Sandbox.NetworkTargets(8080)) != 1 || c.Sandbox.NetworkTargets(8080)[0].Name != "preset:network" {
		t.Fatal(c.Sandbox)
	}
	for _, change := range []func(*Sandbox){
		func(s *Sandbox) { s.MemoryBytes = 1 << 30 },
		func(s *Sandbox) { s.NetworkPresets = []string{"internet"} },
		func(s *Sandbox) { s.Networks = []SandboxNetwork{{Name: "https"}} },
		func(s *Sandbox) { s.AuthorizationCeilings = platform.Limits{NetworkBytes: 10} },
		func(s *Sandbox) { s.WorkspaceProjectQuotas = true },
		func(s *Sandbox) { s.ToolDirectories = []string{`C:\`} },
	} {
		s := c.Sandbox
		change(&s)
		if s.validate(c.DataDir, 8080) == nil {
			t.Fatal("unsupported Windows configuration accepted")
		}
	}
}
