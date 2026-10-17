//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bytes"
	"context"
	"encoding/csv"
	"math"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

type gpuUsage struct {
	utilization *float64
	memory      *int64
}

func sampleGPUUsage(ctx context.Context, devices []platform.GPUDevice) {
	if len(devices) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := nvidiaSMI(ctx, "--query-gpu=uuid,utilization.gpu,memory.used", "--format=csv,noheader,nounits")
	if err != nil {
		return
	}
	usage, err := parseNVIDIAUsage(data)
	if err != nil {
		return
	}
	applyGPUUsage(devices, usage)
}

func applyGPUUsage(devices []platform.GPUDevice, usage map[string]gpuUsage) {
	for i := range devices {
		device := &devices[i]
		sample := usage[device.ID]
		device.UtilizationPercent = sample.utilization
		device.MemoryUsedBytes = nil
		if sample.memory != nil && *sample.memory <= device.MemoryBytes {
			device.MemoryUsedBytes = sample.memory
		}
	}
}

func parseNVIDIAUsage(data []byte) (map[string]gpuUsage, error) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) > 32 {
		return nil, unavailable("gpu_inventory_invalid")
	}
	out := map[string]gpuUsage{}
	for _, row := range rows {
		if len(row) != 3 {
			return nil, unavailable("gpu_inventory_invalid")
		}
		id := strings.TrimSpace(row[0])
		if core.ValidateGPUDevices([]string{id}) != nil {
			return nil, unavailable("gpu_inventory_invalid")
		}
		if _, exists := out[id]; exists {
			return nil, unavailable("gpu_inventory_invalid")
		}
		var sample gpuUsage
		// Unsupported counters (for example N/A) remain unknown, independently.
		if v, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64); err == nil && !math.IsNaN(v) && v >= 0 && v <= 100 {
			sample.utilization = &v
		}
		if v, err := strconv.ParseInt(strings.TrimSpace(row[2]), 10, 64); err == nil && v >= 0 && v <= 1<<30 {
			value := v << 20
			sample.memory = &value
		}
		out[id] = sample
	}
	return out, nil
}
