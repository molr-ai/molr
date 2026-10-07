package core

import "time"

// Constraints are owner-approved resource limits, independent of access scope.
// Resource composition requires limits for compute, network and managed workspaces.
// Other resources (models, tools and external directory access) have no quota.
type ResourceConstraints struct {
	Compute *ComputeConstraints `json:"compute,omitempty"`
	Network *NetworkConstraints `json:"network,omitempty"`
	Storage *StorageConstraints `json:"storage,omitempty"`
}
type ComputeConstraints struct {
	CPUPercent         int   `json:"cpu_percent"`
	MemoryBytes        int64 `json:"memory_bytes"`
	Processes          int   `json:"processes"`
	MaxDurationSeconds int64 `json:"max_duration_seconds"`
	UnlimitedDuration  bool  `json:"unlimited_duration"`
	TempBytes          int64 `json:"temp_bytes"`
}
type NetworkConstraints struct {
	Bytes       int64  `json:"bytes"`
	Period      string `json:"period"`
	Timezone    string `json:"timezone"`
	Connections int    `json:"connections"`
}
type StorageConstraints struct {
	CapacityBytes int64 `json:"capacity_bytes"`
	MaxFileBytes  int64 `json:"max_file_bytes"`
	MaxFiles      int64 `json:"max_files"`
}

func (c *ResourceConstraints) Validate() error {
	if c == nil {
		return nil
	}
	count := 0
	if v := c.Compute; v != nil {
		count++
		if v.CPUPercent < 1 || v.MemoryBytes < 32<<20 || v.Processes < 8 || v.TempBytes < 1 || v.TempBytes > v.MemoryBytes || (v.UnlimitedDuration && v.MaxDurationSeconds != 0) || (!v.UnlimitedDuration && v.MaxDurationSeconds <= 0) || v.MaxDurationSeconds > int64((365*24*time.Hour)/time.Second) {
			return Invalid("constraints.compute", "positive CPU, memory, processes, temporary space and explicit execution duration required")
		}
	}
	if v := c.Network; v != nil {
		count++
		if v.Bytes < 1 || v.Connections < 1 {
			return Invalid("constraints.network", "positive traffic and connection limits required")
		}
		switch v.Period {
		case "lifetime", "day", "week", "month":
		default:
			return Invalid("constraints.network.period", "lifetime, day, week or month required")
		}
		if _, err := time.LoadLocation(v.Timezone); err != nil || v.Timezone == "" {
			return Invalid("constraints.network.timezone", "explicit IANA timezone required")
		}
	}
	if v := c.Storage; v != nil {
		count++
		if v.CapacityBytes < 1 || v.MaxFileBytes < 1 || v.MaxFileBytes > v.CapacityBytes || v.MaxFiles < 1 {
			return Invalid("constraints.storage", "positive capacity, single-file and file-count limits required")
		}
	}
	if count != 1 {
		return Invalid("constraints", "exactly one resource constraint kind required")
	}
	return nil
}
func (c *ResourceConstraints) Kind() string {
	if c == nil {
		return ""
	}
	if c.Compute != nil {
		return "compute"
	}
	if c.Network != nil {
		return "network"
	}
	if c.Storage != nil {
		return "storage"
	}
	return ""
}
