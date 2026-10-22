//go:build !linux && !darwin && !windows

package platform

import "runtime"

func MachineCapacity() HostCapacity {
	return HostCapacity{CPUPercent: runtime.NumCPU() * 100}
}

func FilesystemCapacity(string, int64) StorageCapacity { return StorageCapacity{} }
