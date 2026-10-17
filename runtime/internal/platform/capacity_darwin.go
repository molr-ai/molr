package platform

import (
	"context"
	"golang.org/x/sys/unix"
	"os/exec"
	"time"
)

func hostComputeCapacity() HostCapacity {
	var c HostCapacity
	if memory, err := unix.SysctlUint64("hw.memsize"); err == nil {
		c.MemoryBytes = int64(memory)
	}
	if processes, err := unix.SysctlUint32("kern.maxproc"); err == nil {
		c.Processes = int(processes)
	}
	// Two logging samples provide interval CPU usage; fixed arguments suppress
	// process rows and expensive library/map scans. No shell or background sampler.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/top", "-l", "2", "-s", "1", "-n", "0", "-F", "-R")
	command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	if data, err := command.Output(); err == nil {
		c.CPUUsagePercent, c.MemoryUsedBytes = parseDarwinUsage(string(data), c.MemoryBytes)
	}
	return c
}
