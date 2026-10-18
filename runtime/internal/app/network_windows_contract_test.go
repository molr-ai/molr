package app

import (
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

// Exercises remote Windows capability presentation from every host platform.
func TestAppContainerNetworkScopePreservesWindowsRestrictions(t *testing.T) {
	info := sm.Info{
		Enabled:      true,
		Capabilities: platform.Capabilities{OS: "windows", NetworkBackend: "appcontainer-toggle"},
		Networks:     []sm.Network{{ID: "preset:network", Target: platform.NetworkTarget{Protocols: []string{"tcp", "udp"}, Addresses: []string{"0.0.0.0/0", "::/0"}, Ports: []string{"1-65535"}}}},
	}
	scope := describeNetwork(info, "preset:network")
	if !scope.Known || scope.Mode != "all_ip" || scope.Target == nil {
		t.Fatalf("Windows network grant missing: %+v", scope)
	}
	if !strings.Contains(scope.LoopbackAccess, "does not allow") || !strings.Contains(strings.Join(scope.Restrictions, " "), "firewall") {
		t.Fatalf("Windows restrictions missing: %+v", scope)
	}
	info.Enabled = false
	info.Capabilities.Reason = "sandbox_disabled"
	scope = describeNetwork(info, "preset:network")
	if scope.Known || scope.Target != nil || scope.Reason != "sandbox_disabled" {
		t.Fatalf("disabled backend advertised network access: %+v", scope)
	}
}

func TestNativeComputeGrantRejectsUnsupportedHardLimits(t *testing.T) {
	command := platform.Command{}
	limits := policy.ComputeConstraints{MaxDurationSeconds: 90}
	if err := applyComputeConstraints(&command, &limits, platform.Limits{}); err != nil {
		t.Fatal(err)
	}
	if command.Timeout != 90*time.Second || command.ComputeQuota {
		t.Fatalf("native grant lost duration or invented quota support: %+v", command)
	}
	for _, change := range []func(*policy.ComputeConstraints){
		func(c *policy.ComputeConstraints) { c.CPUPercent = 100 },
		func(c *policy.ComputeConstraints) { c.MemoryBytes = 32 << 20 },
		func(c *policy.ComputeConstraints) { c.Processes = 8 },
		func(c *policy.ComputeConstraints) { c.TempBytes = 1 << 20 },
	} {
		requested := limits
		change(&requested)
		if applyComputeConstraints(&platform.Command{}, &requested, platform.Limits{}) == nil {
			t.Fatal("native backend silently accepted a hard limit", requested)
		}
	}
}
