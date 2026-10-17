package platform

// HostCapacity reports machine capacity, not a reservation or an authorization.
// Zero capacity or nil usage means the platform could not determine that value.
type HostCapacity struct {
	CPUUsagePercent *float64
	MemoryUsedBytes *int64
	CPUPercent      int
	MemoryBytes     int64
	Processes       int
}

// StorageCapacity describes one filesystem; shared disks must not be summed.
type StorageCapacity struct {
	StorageTotalBytes     int64
	StorageUsedBytes      int64
	StorageBytes          int64
	StorageAvailableBytes int64
}
