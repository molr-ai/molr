package sandbox

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"testing"
	"time"
)

func testExecution() core.Execution {
	return core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant", OperationID: "operation"}
}
func testCommand() platform.Command {
	return platform.Command{Script: "echo ok", Directory: "/work", Timeout: time.Second, MaxOutputBytes: 4096, Limits: platform.Limits{MemoryBytes: 128 << 20, Processes: 32, CPUPercent: 100, TempBytes: 8 << 20, MaxFileBytes: 64 << 20}}
}
func TestSandboxFailsClosed(t *testing.T) {
	r := New(Options{QuotaStore: quotaTestStore(t)})
	c, err := r.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Shell || c.Reason == "" {
		t.Fatalf("unconfigured capability: %+v", c)
	}
	if _, err = r.Start(context.Background(), testExecution(), testCommand()); err == nil {
		t.Fatal("unconfigured runner executed shell")
	}
}
func TestPlanRejectsUnsafeMountsAndUnboundedExecution(t *testing.T) {
	for _, mutate := range []func(*platform.Command){
		func(c *platform.Command) { c.Timeout = 0 },
		func(c *platform.Command) { c.Limits.Processes = 0 },
		func(c *platform.Command) {
			c.Mounts = []platform.Mount{{Source: "/", Destination: "/resources/x", ReadOnly: true}}
		},
		func(c *platform.Command) {
			c.Mounts = []platform.Mount{{Source: "/data", Destination: "/proc", ReadOnly: true}}
		},
		func(c *platform.Command) { c.Limits.MaxFileBytes = 0 },
		func(c *platform.Command) {
			c.Mounts = []platform.Mount{{Source: "/data", Destination: "/resources/id/../../work"}}
		},
		func(c *platform.Command) {
			c.Mounts = []platform.Mount{{Source: "/data", Destination: "/resources/id"}, {Source: "/other", Destination: "/resources/id/child"}}
		},
		func(c *platform.Command) {
			c.Mounts = []platform.Mount{{Source: "/data", Destination: "/resources/id/child"}, {Source: "/other", Destination: "/resources/id"}}
		},
		func(c *platform.Command) { c.Directory = "/work/../etc" },
		func(c *platform.Command) { c.Environment = map[string]string{"LD_PRELOAD": "/work/library"} },
		func(c *platform.Command) {
			c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"*"}, Ports: []string{"443"}}}
		},
	} {
		c := testCommand()
		mutate(&c)
		if validate(c) == nil {
			t.Fatalf("accepted unsafe plan: %+v", c)
		}
	}
}
