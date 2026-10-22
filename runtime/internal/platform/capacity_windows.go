package platform

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var capacityKernel32 = windows.NewLazySystemDLL("kernel32.dll")
var globalMemoryStatusEx = capacityKernel32.NewProc("GlobalMemoryStatusEx")
var getSystemTimes = capacityKernel32.NewProc("GetSystemTimes")

func MachineCapacity() HostCapacity {
	c := HostCapacity{CPUPercent: runtime.NumCPU() * 100}
	var memory struct {
		Length, MemoryLoad                                 uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile uint64
		TotalVirtual, AvailVirtual, AvailExtendedVirtual   uint64
	}
	memory.Length = uint32(unsafe.Sizeof(memory))
	if ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memory))); ok != 0 && memory.TotalPhys <= math.MaxInt64 && memory.AvailPhys <= memory.TotalPhys {
		c.MemoryBytes = int64(memory.TotalPhys)
		used := int64(memory.TotalPhys - memory.AvailPhys)
		c.MemoryUsedBytes = &used
	}
	// GetSystemTimes covers only one processor group above 64 logical CPUs.
	// Do not present a partial group as whole-machine utilization.
	if runtime.NumCPU() <= 64 {
		if before, ok := windowsCPUTicks(); ok {
			time.Sleep(100 * time.Millisecond)
			if after, ok := windowsCPUTicks(); ok {
				c.CPUUsagePercent = cpuUsagePercent(before, after)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.GPUs = windowsNVIDIASnapshot(ctx)
	return c
}

func windowsCPUTicks() (cpuTicks, bool) {
	var idle, kernel, user windows.Filetime
	ok, _, _ := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return cpuTicks{}, false
	}
	ticks := func(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	// Windows kernel time already includes idle time.
	return cpuTicks{total: ticks(kernel) + ticks(user), idle: ticks(idle)}, true
}

func FilesystemCapacity(storageRoot string, reserveBytes int64) StorageCapacity {
	if storageRoot == "" {
		return StorageCapacity{}
	}
	storageRoot, err := filepath.Abs(storageRoot)
	if err != nil {
		return StorageCapacity{}
	}
	for {
		st, err := os.Stat(storageRoot)
		if err == nil {
			if !st.IsDir() {
				return StorageCapacity{}
			}
			path, err := windows.UTF16PtrFromString(storageRoot)
			if err != nil {
				return StorageCapacity{}
			}
			var available, total, free uint64
			if windows.GetDiskFreeSpaceEx(path, &available, &total, &free) != nil || total > math.MaxInt64 || available > total {
				return StorageCapacity{}
			}
			// Total and available both honor the calling account's disk quota.
			reserveBytes = max(0, reserveBytes)
			return StorageCapacity{
				StorageTotalBytes:     int64(total),
				StorageUsedBytes:      int64(total - available),
				StorageBytes:          max(0, int64(total)-reserveBytes),
				StorageAvailableBytes: max(0, int64(available)-reserveBytes),
			}
		}
		if !os.IsNotExist(err) {
			return StorageCapacity{}
		}
		parent := filepath.Dir(storageRoot)
		if parent == storageRoot {
			return StorageCapacity{}
		}
		storageRoot = parent
	}
}
