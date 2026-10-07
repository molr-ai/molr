package config

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"path/filepath"
	"strings"
	"time"
)

// Sandbox is deployment-only. Resource APIs may reference these entries but
// cannot enlarge them or change the enforced execution limits.
type Sandbox struct {
	AllowFullAccess        bool            `json:"allow_full_access"`
	WorkspaceProjectQuotas bool            `json:"workspace_project_quotas"`
	AuthorizationCeilings  platform.Limits `json:"authorization_ceilings"`
	HostTools              bool            `json:"host_tools"`
	// Deprecated: accepted for existing deployments; never switches OS identity.
	HostToolsUser       string             `json:"host_tools_user,omitempty"`
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
	return Sandbox{AuthorizationCeilings: platform.Limits{CPUPercent: 6400, MemoryBytes: 64 << 30, Processes: 4096, TempBytes: 8 << 30, MaxFileBytes: 1 << 40, NetworkBytes: 1 << 40, NetworkConnections: 128}, Enabled: true, NetworkBytes: 64 << 20, NetworkConnections: 16, NetworkPresets: []string{"internet", "lan", "host"}, MinFreeDiskBytes: 256 << 20, MaxFileBytes: 1 << 30, Timeout: Duration(10 * time.Minute), MemoryBytes: 512 << 20, Processes: 64, CPUPercent: 100, TempBytes: 64 << 20, MaxOutputBytes: 64 << 10}
}
func (s Sandbox) validate(data string, ownPort uint16) error {
	if !s.Enabled {
		return nil
	}
	if s.AuthorizationCeilings.NetworkConnections < 1 || s.AuthorizationCeilings.NetworkConnections > 128 || s.AuthorizationCeilings.NetworkBytes < 1 || s.AuthorizationCeilings.NetworkBytes > 1<<50 || s.AuthorizationCeilings.CPUPercent < 1 || s.AuthorizationCeilings.CPUPercent > 1000000 || s.AuthorizationCeilings.MemoryBytes < 32<<20 || s.AuthorizationCeilings.Processes < 8 || s.AuthorizationCeilings.TempBytes < 1 || s.AuthorizationCeilings.TempBytes > s.AuthorizationCeilings.MemoryBytes {
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
	if s.MinFreeDiskBytes <= 0 || s.MaxFileBytes <= 0 || s.Timeout <= 0 || s.Timeout > Duration(time.Hour) || s.MemoryBytes < 32<<20 || s.TempBytes < 1 || s.TempBytes > s.MemoryBytes || s.Processes < 8 || s.Processes > 1024 || s.CPUPercent < 1 || s.CPUPercent > 1000 || s.MaxOutputBytes < 1 || s.MaxOutputBytes > 128<<10 || len(s.Directories) > 32 || len(s.Networks) > 32 || s.NetworkBytes < 1024 || s.NetworkBytes > 1<<30 || s.NetworkConnections < 1 || s.NetworkConnections > 128 {
		return core.Invalid("sandbox", "invalid limits")
	}
	overlap := func(a, b string) bool {
		if a == "" || b == "" {
			return false
		}
		return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
	}
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
		switch id {
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
