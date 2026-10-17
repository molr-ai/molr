package platform

import "testing"

func TestCPUUsageSnapshot(t *testing.T) {
	before, ok := parseCPUTicks("cpu 100 10 20 200 30 5 5 10 80 5\ncpu0 1 2 3 4\n")
	if !ok || before.total != 380 || before.idle != 230 {
		t.Fatalf("guest time counted twice or idle incorrect: %+v", before)
	}
	after, ok := parseCPUTicks("cpu 140 10 30 230 50 5 5 10 100 5\n")
	if !ok {
		t.Fatal("invalid sample")
	}
	usage := cpuUsagePercent(before, after)
	if usage == nil || *usage != 50 {
		t.Fatalf("current interval usage: %v", usage)
	}
	idle := cpuUsagePercent(cpuTicks{total: 100, idle: 50}, cpuTicks{total: 200, idle: 150})
	if idle == nil || *idle != 0 {
		t.Fatal("zero usage must remain known")
	}
	for _, pair := range [][2]cpuTicks{{before, before}, {after, before}, {{total: 100, idle: 50}, {total: 101, idle: 100}}} {
		if cpuUsagePercent(pair[0], pair[1]) != nil {
			t.Fatal("invalid delta reported as usage")
		}
	}
	for _, data := range []string{"", "cpu0 1 2 3 4", "cpu 1 bad 3 4", "cpu 1 2 3"} {
		if _, ok := parseCPUTicks(data); ok {
			t.Fatalf("accepted invalid counters %q", data)
		}
	}
}

func TestMemoryUsageSnapshot(t *testing.T) {
	const total = 16 << 30
	used := memoryUsedBytes("MemTotal: 16777216 kB\nMemFree: 1048576 kB\nMemAvailable: 12582912 kB\n", total)
	if used == nil || *used != 4<<30 {
		t.Fatalf("reclaimable memory counted as used: %v", used)
	}
	for _, data := range []string{"", "MemAvailable: -1 kB", "MemAvailable: 99999999999 kB", "MemAvailable: invalid kB", "MemAvailable: 1 MB"} {
		if memoryUsedBytes(data, total) != nil {
			t.Fatalf("accepted invalid available memory %q", data)
		}
	}
}

func TestDarwinUsageSnapshot(t *testing.T) {
	cpu, memory := parseDarwinUsage("CPU usage: 0.0% user, 0.0% sys, 100.0% idle\nPhysMem: 2G used\nCPU usage: 20.0% user, 5.0% sys, 75.0% idle\nPhysMem: 5120M used (1024M wired, 512M compressor), 3G unused.\n", 8<<30)
	if cpu == nil || *cpu != 25 || memory == nil || *memory != 5<<30 {
		t.Fatalf("incorrect final sample: %v %v", cpu, memory)
	}
	cpu, memory = parseDarwinUsage("CPU usage: 0.0% user, 0.0% sys, 100.0% idle\nPhysMem: 999T used", 8<<30)
	if cpu != nil || memory != nil {
		t.Fatal("accepted first sample or impossible memory")
	}
	cpu, memory = parseDarwinUsage("unavailable", 8<<30)
	if cpu != nil || memory != nil {
		t.Fatal("invented usage")
	}
}
