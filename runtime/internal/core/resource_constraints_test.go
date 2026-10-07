package core

import "testing"

func TestResourceConstraintsRejectAmbiguousOrUnboundedLimits(t *testing.T) {
	c := &ResourceConstraints{Compute: &ComputeConstraints{CPUPercent: 100, MemoryBytes: 128 << 20, Processes: 32, TempBytes: 8 << 20, MaxDurationSeconds: 60}}
	if err := c.Validate(); err != nil || c.Kind() != "compute" {
		t.Fatal(err, c.Kind())
	}
	c.Compute.MaxDurationSeconds = 0
	if c.Validate() == nil {
		t.Fatal("implicit unlimited duration accepted")
	}
	c.Compute.UnlimitedDuration = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Network = &NetworkConstraints{Bytes: 100, Connections: 1, Period: "day", Timezone: "UTC"}
	if c.Validate() == nil {
		t.Fatal("mixed resource constraints accepted")
	}
	c.Compute = nil
	if err := c.Validate(); err != nil || c.Kind() != "network" {
		t.Fatal(err)
	}
	c.Network.Timezone = "missing/timezone"
	if c.Validate() == nil {
		t.Fatal("invalid calendar accepted")
	}
	c = &ResourceConstraints{Storage: &StorageConstraints{CapacityBytes: 100, MaxFileBytes: 101, MaxFiles: 2}}
	if c.Validate() == nil {
		t.Fatal("file larger than workspace accepted")
	}
	c.Storage.MaxFileBytes = 50
	if err := c.Validate(); err != nil || c.Kind() != "storage" {
		t.Fatal(err)
	}
}
