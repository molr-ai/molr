package app

import (
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

// These are the actual deployment rules, not a description inferred from a
// resource's editable name. A missing/offline source must not imply broad access.
type networkPermissionScope struct {
	Known          bool
	Reason         string                  `json:",omitempty"`
	Target         *platform.NetworkTarget `json:",omitempty"`
	Restrictions   []string
	LoopbackAccess string
}

func describeNetwork(info sm.Info, targetID string) *networkPermissionScope {
	out := &networkPermissionScope{
		Reason: "network_target_unavailable",
		Restrictions: []string{
			"The sandbox network gateway operates at layer 4 (the transport layer) and supports only TCP/UDP. Protocols outside this scope, such as ICMP, are not forwarded.",
			"Unspecified, multicast, link-local and scoped IP destinations are prohibited. Metadata addresses 100.100.100.200, 168.63.129.16 and fd00:ec2::254 are prohibited.",
			"198.18.0.0/15 and fd00:6d6f:6c72:ffff::/64 are sandbox DNS virtual address ranges, not directly reachable destination networks. Keep using hostnames; do not reuse virtual IPs across executions.",
			"Target is an allow-set minus its exclusions. Separately granted network resources form a union. Runtime automatically selects active grants in the execution sandbox; requestable resources are not grants.",
			"Every resolved address is checked. Hostname exclusions apply to hostname connections; direct IP connections are checked against IP/CIDR exclusions. Allowed access does not guarantee the destination service is listening or reachable.",
		},
		LoopbackAccess: "localhost and literal loopback addresses belong to the execution sandbox. Use host.molr.internal (127.0.0.1) or host6.molr.internal (::1) to access authorized host loopback services. The host LAN address is evaluated as a LAN destination.",
	}
	if !info.Enabled {
		out.Reason = info.Capabilities.Reason
		if out.Reason == "" {
			out.Reason = "sandbox_unavailable"
		}
		return out
	}
	for _, n := range info.Networks {
		if n.ID == targetID {
			target := n.Target
			out.Known, out.Reason, out.Target = true, "", &target
			break
		}
	}
	return out
}

func localNetworkInfo(a adapters) sm.Info {
	info := sm.Info{Enabled: a.storageConfig.Enabled, Limits: platform.Limits{NetworkBytes: a.storageConfig.NetworkBytes, NetworkConnections: a.storageConfig.NetworkConnections}}
	for _, n := range a.storageConfig.NetworkTargets(a.ownPort) {
		info.Networks = append(info.Networks, sm.Network{ID: n.Name, Target: n.Target()})
	}
	return info
}
