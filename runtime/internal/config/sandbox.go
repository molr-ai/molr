package config

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
	"path/filepath"
	"strings"
	"time"
)

// Sandbox is deployment-only. Resource APIs may reference these entries but
// cannot enlarge them or change the enforced execution limits.
type Sandbox struct {
	ToolDirectories        []string           `json:"tool_directories,omitempty"`
	ProtectedRoots         []string           `json:"-"`
	GPU                    platform.GPUConfig `json:"gpu"`
	AllowFullAccess        bool               `json:"allow_full_access"`
	WorkspaceProjectQuotas bool               `json:"workspace_project_quotas"`
	AuthorizationCeilings  platform.Limits    `json:"authorization_ceilings"`
	HostTools              bool               `json:"host_tools"`
	// Deprecated: accepted for existing deployments; never switches OS identity.
	HostToolsUser string `json:"host_tools_user,omitempty"`
	// Enabled controls command execution. Workspace and file tools remain available.
	Enabled             bool               `json:"enabled"`
	Bwrap               string             `json:"bubblewrap"`
	RootFS              string             `json:"rootfs"`
	CgroupRoot          string             `json:"cgroup_root"`
	WorkRoot            string             `json:"work_root"`
	MinFreeDiskBytes    int64              `json:"min_free_disk_bytes"`
	Timeout             Duration           `json:"timeout"`
	MemoryBytes         int64              `json:"memory_bytes"`
	Processes           int                `json:"processes"`
	CPUPercent          int                `json:"cpu_percent"`
	TempBytes           int64              `json:"temp_bytes"`
	MaxOutputBytes      int64              `json:"max_output_bytes"`
	MaxFileBytes        int64              `json:"max_file_bytes"`
	RestrictDirectories bool               `json:"restrict_directories"`
	Directories         []SandboxDirectory `json:"directories"`
	NetworkBytes        int64              `json:"network_bytes"`
	NetworkConnections  int                `json:"network_connections"`
	NetworkPresets      []string           `json:"network_presets"`
	Networks            []SandboxNetwork   `json:"networks"`
}
type SandboxDirectory struct {
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}
type SandboxNetwork struct {
	Protocols        []string `json:"protocols"`
	Name             string   `json:"name"`
	Addresses        []string `json:"addresses"`
	Ports            []string `json:"ports"`
	ExcludeAddresses []string `json:"exclude_addresses,omitempty"`
	ExcludePorts     []string `json:"exclude_ports,omitempty"`
}

func (n SandboxNetwork) Target() platform.NetworkTarget {
	return platform.NetworkTarget{Protocols: n.Protocols, Addresses: n.Addresses, Ports: n.Ports, ExcludeAddresses: n.ExcludeAddresses, ExcludePorts: n.ExcludePorts}
}

func sandboxDefaults() Sandbox {
	if platform.NativeSandbox() {
		return Sandbox{Enabled: true, NetworkPresets: []string{"network"}, ToolDirectories: []string{"/opt/homebrew", "/usr/local", "/Library/Developer/CommandLineTools"}, Timeout: Duration(10 * time.Minute), MaxOutputBytes: 64 << 10}
	}
	return Sandbox{AuthorizationCeilings: platform.Limits{TempBytes: 8 << 30, MaxFileBytes: 1 << 40, NetworkBytes: 1 << 40, NetworkConnections: 128}, Enabled: true, NetworkBytes: 64 << 20, NetworkConnections: 16, NetworkPresets: []string{"internet", "lan", "host"}, MinFreeDiskBytes: 256 << 20, MaxFileBytes: 1 << 30, Timeout: Duration(10 * time.Minute), MemoryBytes: 512 << 20, Processes: 64, CPUPercent: 100, TempBytes: 64 << 20, MaxOutputBytes: 64 << 10}
}
func (s Sandbox) ComputeCeilings() platform.Limits {
	v := s.AuthorizationCeilings
	if !platform.NativeSandbox() {
		// Zero means no deployment ceiling; protocol/backend capacity bounds remain.
		if v.CPUPercent == 0 {
			v.CPUPercent = 1000000
		}
		if v.MemoryBytes == 0 {
			v.MemoryBytes = 1<<53 - 1
		}
		if v.Processes == 0 {
			v.Processes = 1<<31 - 1
		}
		if v.TempBytes == 0 {
			v.TempBytes = v.MemoryBytes
		}
		if v.MaxFileBytes == 0 {
			v.MaxFileBytes = 1<<53 - 1
		}
		if v.NetworkBytes == 0 {
			v.NetworkBytes = 1 << 50
		}
		if v.NetworkConnections == 0 {
			v.NetworkConnections = 128
		}
	}
	v.AllGPUDevices = s.GPU.Enabled && s.GPU.AllDevices
	v.GPUDeviceIDs = nil
	if s.GPU.Enabled {
		v.GPUDeviceIDs = append([]string(nil), s.GPU.Devices...)
	}
	return v
}
func (s Sandbox) validate(data string, ownPort uint16) error {
	if err := core.ValidateGPUDevices(s.GPU.Devices); err != nil {
		return err
	}
	if len(s.AuthorizationCeilings.GPUDeviceIDs) > 0 {
		return core.Invalid("sandbox.authorization_ceilings", "configure GPU devices through sandbox.gpu")
	}
	if s.GPU.AllDevices && len(s.GPU.Devices) > 0 {
		return core.Invalid("sandbox.gpu", "choose all_devices or an explicit device list")
	}
	if s.GPU.Enabled && !s.GPU.AllDevices && len(s.GPU.Devices) == 0 {
		return core.Invalid("sandbox.gpu.devices", "explicit GPU device list required")
	}
	maxLimits := s.ComputeCeilings()
	if !platform.NativeSandbox() && (maxLimits.NetworkConnections < 1 || maxLimits.NetworkConnections > 128 || maxLimits.NetworkBytes < 1 || maxLimits.NetworkBytes > 1<<50 || maxLimits.CPUPercent < 1 || maxLimits.CPUPercent > 1000000 || maxLimits.MemoryBytes < 32<<20 || maxLimits.Processes < 8 || maxLimits.TempBytes < 1 || maxLimits.TempBytes > maxLimits.MemoryBytes) {
		return core.Invalid("sandbox.authorization_ceilings", "invalid deployment ceilings")
	}
	if s.HostToolsUser != "" && s.HostToolsUser != "molr-sandbox" && s.HostToolsUser != "current" {
		return core.Invalid("sandbox.host_tools_user", "legacy host_tools_user must be current or molr-sandbox; use host_tools instead")
	}
	if s.UsesHostTools() && s.RootFS != "" {
		return core.Invalid("sandbox.host_tools", "host tools and custom rootfs are incompatible")
	}
	for _, p := range []string{s.Bwrap, s.RootFS, s.CgroupRoot, s.WorkRoot} {
		// Missing deployment paths leave Shell unavailable, without preventing
		// the default-enabled Runtime from serving unrelated features.
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == string(filepath.Separator) {
			return core.Invalid("sandbox", "dedicated absolute deployment paths required")
		}
	}
	if !platform.NativeSandbox() && (s.MinFreeDiskBytes <= 0 || s.MaxFileBytes <= 0 || s.Timeout <= 0 || s.Timeout > Duration(time.Hour) || s.MemoryBytes < 32<<20 || s.TempBytes < 1 || s.TempBytes > s.MemoryBytes || s.Processes < 8 || s.Processes > 1024 || s.CPUPercent < 1 || s.CPUPercent > 1000 || s.MaxOutputBytes < 1 || s.MaxOutputBytes > 128<<10 || len(s.Directories) > 32 || len(s.Networks) > 32 || s.NetworkBytes < 1024 || s.NetworkBytes > 1<<30 || s.NetworkConnections < 1 || s.NetworkConnections > 128) {
		return core.Invalid("sandbox", "invalid limits")
	}
	if platform.NativeSandbox() {
		if s.MemoryBytes != 0 || s.Processes != 0 || s.CPUPercent != 0 || s.TempBytes != 0 || s.MaxFileBytes != 0 || s.NetworkBytes != 0 || s.NetworkConnections != 0 || s.MinFreeDiskBytes != 0 || s.WorkspaceProjectQuotas || s.GPU.Enabled || s.GPU.AllDevices || len(s.GPU.Devices) > 0 || s.Bwrap != "" || s.RootFS != "" || s.CgroupRoot != "" || len(s.Networks) > 0 {
			return core.Invalid("sandbox", "macOS supports file scopes, execution duration and a network toggle; hard quotas and Linux backends are unsupported")
		}
		max := s.AuthorizationCeilings
		if max.CPUPercent != 0 || max.MemoryBytes != 0 || max.Processes != 0 || max.TempBytes != 0 || max.MaxFileBytes != 0 || max.NetworkBytes != 0 || max.NetworkConnections != 0 {
			return core.Invalid("sandbox.authorization_ceilings", "hard quotas are unsupported on macOS")
		}
		if s.Timeout <= 0 || s.Timeout > Duration(time.Hour) || s.MaxOutputBytes < 1 || s.MaxOutputBytes > 128<<10 || len(s.Directories) > 32 {
			return core.Invalid("sandbox", "invalid limits")
		}
	} else if len(s.ToolDirectories) > 0 {
		return core.Invalid("sandbox.tool_directories", "native macOS tools only")
	}
	for _, dir := range s.ToolDirectories {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || textfiles.Overlaps(dir, data) || textfiles.Overlaps(dir, s.WorkRoot) {
			return core.Invalid("sandbox.tool_directories", "dedicated tool directories must not overlap private data or workspaces")
		}
	}
	overlap := textfiles.Overlaps
	if overlap(s.WorkRoot, data) || overlap(s.RootFS, data) || overlap(s.WorkRoot, s.RootFS) {
		return core.Invalid("sandbox", "execution data must be separate from private runtime data and rootfs")
	}
	seen := map[string]bool{}
	for _, d := range s.Directories {
		if !filepath.IsAbs(d.Path) || filepath.Clean(d.Path) != d.Path || d.Path == string(filepath.Separator) || seen[d.Path] {
			return core.Invalid("sandbox.directories", "invalid resource directory")
		}
		for _, p := range []string{data, s.RootFS, s.WorkRoot, s.Bwrap, s.CgroupRoot} {
			if overlap(d.Path, p) {
				return core.Invalid("sandbox.directories", "protected path")
			}
		}
		seen[d.Path] = true
	}
	seen = map[string]bool{}
	for _, network := range s.Networks {
		if strings.HasPrefix(network.Name, "preset:") {
			return core.Invalid("sandbox.networks", "preset: names are reserved for built-in networks")
		}
	}
	networks, err := s.availableNetworks(ownPort)
	if err != nil {
		return err
	}
	for _, n := range networks {
		if n.Name == "" || len(n.Name) > 64 || strings.ContainsAny(n.Name, "/\\\x00") || seen[n.Name] {
			return core.Invalid("sandbox.networks", "named network targets required")
		}
		seen[n.Name] = true
		if _, err := platform.CompileNetworkTarget(n.Target()); err != nil {
			return err
		}
	}
	return nil
}

// Presets expose candidates only. Connecting and granting resources remain explicit.
func (s Sandbox) availableNetworks(ownPort uint16) ([]SandboxNetwork, error) {
	networks := append([]SandboxNetwork{}, s.Networks...)
	seen := map[string]bool{}
	for _, id := range s.NetworkPresets {
		if seen[id] {
			return nil, core.Invalid("network_presets", "duplicate preset")
		}
		seen[id] = true
		n := SandboxNetwork{Name: "preset:" + id, Protocols: []string{"tcp", "udp"}, Ports: []string{"1-65535"}}
		if platform.NativeSandbox() && id != "network" {
			return nil, core.Invalid("network_presets", "macOS supports only the network toggle preset")
		}
		switch id {
		case "network":
			if !platform.NativeSandbox() {
				return nil, core.Invalid("network_presets", "network toggle requires macOS")
			}
			n.Addresses = []string{"0.0.0.0/0", "::/0"}
		case "internet":
			n.Addresses = []string{"0.0.0.0/0", "::/0"}
			n.ExcludeAddresses = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", "127.0.0.0/8", "::1/128", "198.18.0.0/15"}
		case "lan":
			n.Addresses = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}
		case "host":
			n.Addresses = []string{"127.0.0.0/8", "::1/128"}
		default:
			return nil, core.Invalid("network_presets", "unknown network preset")
		}
		networks = append(networks, n)
	}
	return networks, nil
}
func (s Sandbox) NetworkTargets(ownPort uint16) []SandboxNetwork {
	n, _ := s.availableNetworks(ownPort)
	return n
}

// UsesHostTools keeps old deployment files readable; the service manager owns identity.
func (s Sandbox) UsesHostTools() bool {
	return s.HostTools || s.HostToolsUser != ""
}
