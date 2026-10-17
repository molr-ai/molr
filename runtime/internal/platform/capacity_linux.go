package platform

import (
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func hostComputeCapacity() HostCapacity {
	var c HostCapacity
	var info unix.Sysinfo_t
	if unix.Sysinfo(&info) == nil {
		c.MemoryBytes = int64(info.Totalram) * int64(info.Unit)
	}
	if data, err := os.ReadFile("/proc/sys/kernel/threads-max"); err == nil {
		c.Processes, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		c.MemoryUsedBytes = memoryUsedBytes(string(data), c.MemoryBytes)
	}
	// A short on-demand sample measures current activity instead of lifetime load.
	// There is no sampler or background polling when the page is not refreshed.
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		before, ok := parseCPUTicks(string(data))
		if ok {
			time.Sleep(100 * time.Millisecond)
			if data, err := os.ReadFile("/proc/stat"); err == nil {
				if after, ok := parseCPUTicks(string(data)); ok {
					c.CPUUsagePercent = cpuUsagePercent(before, after)
				}
			}
		}
	}
	return c
}
