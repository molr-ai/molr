package platform

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"molr.ai/runtime/internal/core"
)

func windowsNVIDIASnapshot(ctx context.Context) []GPUDevice {
	// Search only driver installation locations, never the working directory or PATH.
	var candidates []string
	if system, err := windows.GetSystemDirectory(); err == nil {
		candidates = append(candidates, filepath.Join(system, "nvidia-smi.exe"))
	}
	if programs, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0); err == nil {
		candidates = append(candidates, filepath.Join(programs, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
	}
	for _, path := range candidates {
		if ctx.Err() != nil {
			return nil
		}
		cmd := exec.CommandContext(ctx, path, "--query-gpu=uuid,name,memory.total,utilization.gpu,memory.used", "--format=csv,noheader,nounits")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		cmd.WaitDelay = 200 * time.Millisecond
		out, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(out, (64<<10)+1))
		if readErr != nil || len(data) > 64<<10 {
			_ = cmd.Process.Kill()
		}
		err = cmd.Wait()
		if err == nil && readErr == nil && len(data) <= 64<<10 {
			return parseWindowsNVIDIA(data)
		}
	}
	return nil
}

func parseWindowsNVIDIA(data []byte) []GPUDevice {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) > 32 {
		return nil
	}
	devices := make([]GPUDevice, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if len(row) != 5 {
			return nil
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		if core.ValidateGPUDevices([]string{row[0]}) != nil || seen[row[0]] || row[1] == "" || len(row[1]) > 256 {
			return nil
		}
		seen[row[0]] = true
		device := GPUDevice{ID: row[0], Name: row[1], Vendor: "nvidia", Reason: "gpu_platform_unsupported"}
		if total, ok := nvidiaMemoryBytes(row[2]); ok {
			device.MemoryBytes = total
		}
		if usage, err := strconv.ParseFloat(row[3], 64); err == nil && !math.IsNaN(usage) && usage >= 0 && usage <= 100 {
			device.UtilizationPercent = &usage
		}
		if used, ok := nvidiaMemoryBytes(row[4]); ok && (device.MemoryBytes == 0 || used <= device.MemoryBytes) {
			device.MemoryUsedBytes = &used
		}
		devices = append(devices, device)
	}
	return devices
}

func nvidiaMemoryBytes(value string) (int64, bool) {
	mib, err := strconv.ParseUint(value, 10, 63)
	if err != nil || mib > math.MaxInt64>>20 {
		return 0, false
	}
	return int64(mib) << 20, true
}
