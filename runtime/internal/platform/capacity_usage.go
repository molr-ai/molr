package platform

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

type cpuTicks struct{ total, idle uint64 }

func parseCPUTicks(data string) (cpuTicks, bool) {
	var ticks cpuTicks
	line, _, _ := strings.Cut(data, "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return ticks, false
	}
	// guest and guest_nice are already included in user and nice.
	for i := 1; i < len(fields) && i <= 8; i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuTicks{}, false
		}
		ticks.total += v
		if i == 4 || i == 5 {
			ticks.idle += v
		}
	}
	return ticks, true
}

func cpuUsagePercent(before, after cpuTicks) *float64 {
	if after.total <= before.total || after.idle < before.idle {
		return nil
	}
	total, idle := after.total-before.total, after.idle-before.idle
	if idle > total {
		return nil
	}
	percent := 100 * float64(total-idle) / float64(total)
	return &percent
}

func memoryUsedBytes(data string, total int64) *int64 {
	if total <= 0 {
		return nil
	}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "MemAvailable:" || fields[2] != "kB" {
			continue
		}
		available, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || available < 0 || available > total/1024 {
			return nil
		}
		used := total - available*1024
		return &used
	}
	return nil
}

var darwinCPUIdle = regexp.MustCompile(`CPU usage:.*?([0-9.]+)% idle`)
var darwinMemoryUsed = regexp.MustCompile(`PhysMem: ([0-9.]+)([BKMGT]) used`)

func parseDarwinUsage(data string, totalMemory int64) (*float64, *int64) {
	var cpu *float64
	var memory *int64
	// top's first CPU sample is not an interval snapshot. Require both samples
	// and use the last one. See Apple's top(1) logging-mode documentation.
	if samples := darwinCPUIdle.FindAllStringSubmatch(data, -1); len(samples) >= 2 {
		if idle, err := strconv.ParseFloat(samples[len(samples)-1][1], 64); err == nil && idle >= 0 && idle <= 100 {
			used := 100 - idle
			cpu = &used
		}
	}
	if samples := darwinMemoryUsed.FindAllStringSubmatch(data, -1); len(samples) > 0 {
		sample := samples[len(samples)-1]
		if value, err := strconv.ParseFloat(sample[1], 64); err == nil {
			value *= math.Pow(1024, float64(strings.Index("BKMGT", sample[2])))
			if value >= 0 && value <= float64(totalMemory) {
				used := int64(value)
				memory = &used
			}
		}
	}
	return cpu, memory
}
